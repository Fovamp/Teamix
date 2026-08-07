package serve

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"reasonix/internal/ragflow"
)

// Teamix 知识库：文档知识库（RAGFlow）+ 代码知识库（codebase-memory-mcp）。
// 配置来源：项目 .env 的 RAGFLOW_API_KEY / RAGFLOW_BASE_URL（serve 启动时
// loadTeamixEnv 注入进程环境）。

func (ts *TeamixServer) ragflowClient() *ragflow.Client {
	return ragflow.NewClient(os.Getenv("RAGFLOW_BASE_URL"), os.Getenv("RAGFLOW_API_KEY"))
}

// kbDatasetView 是 overview 里单个 dataset 的视图。
type kbDatasetView struct {
	Name   string `json:"name"`
	Docs   int    `json:"docs"`
	Chunks int    `json:"chunks"`
	Tokens int    `json:"tokens"`
}

// kbProjectView 是 overview 里单个项目的代码索引状态。
type kbProjectView struct {
	Name    string `json:"name"`
	Indexed bool   `json:"indexed"`
	IndexAt string `json:"indexAt,omitempty"` // 找到的索引目录
}

// handleKBOverview 架构师知识库总览：RAGFlow 连接状态 + dataset 列表；
// 各项目代码索引状态（.codebase-memory 产物或 .teamix/codebase-index 目录）；
// codebase-memory-mcp 服务器是否已配置。
func (ts *TeamixServer) handleKBOverview(w http.ResponseWriter, r *http.Request, u *userSession) {
	if !ts.isArchitect(u) {
		http.Error(w, "仅架构师可查看知识库总览", http.StatusForbidden)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	key := os.Getenv("RAGFLOW_API_KEY")
	rag := map[string]any{"configured": key != ""}
	datasets := []kbDatasetView{}
	if key != "" {
		client := ts.ragflowClient()
		list, err := client.ListDatasets(ctx)
		if err != nil {
			rag["reachable"] = false
			rag["error"] = err.Error()
		} else {
			rag["reachable"] = true
			for _, d := range list {
				datasets = append(datasets, kbDatasetView{Name: d.Name, Docs: d.DocCount, Chunks: d.ChunkCount, Tokens: d.TokenNum})
			}
			sort.Slice(datasets, func(i, j int) bool { return datasets[i].Name < datasets[j].Name })
		}
	}
	rag["datasets"] = datasets

	// codebase-memory-mcp 配置状态（全局 mcp.json）
	mcp := map[string]any{"configured": false, "name": ""}
	for name := range ts.loadGlobalMCPServers() {
		if strings.Contains(strings.ToLower(name), "codebase-memory") {
			mcp["configured"] = true
			mcp["name"] = name
			break
		}
	}

	// 各项目代码索引状态：公共区 projects/<项目>/.codebase-memory 或
	// 任一用户 .teamix/codebase-index/<项目>。
	projects := []kbProjectView{}
	if g := ts.GlobalCfg(); g != nil && g.Projects != nil {
		for _, p := range g.Projects.Projects {
			pv := kbProjectView{Name: p.Name}
			// 公共克隆：仓库内 .codebase-memory（codebase-memory-mcp --persistence 默认产物）
			pub := filepath.Join(ts.workspaceRoot, "projects", p.Name, ".codebase-memory")
			if fi, err := os.Stat(pub); err == nil && fi.IsDir() {
				pv.Indexed = true
				pv.IndexAt = pub
			} else {
				// 用户级索引目录
				users, _ := os.ReadDir(filepath.Join(ts.workspaceRoot, "users"))
				for _, ue := range users {
					if !ue.IsDir() {
						continue
					}
					idx := filepath.Join(ts.workspaceRoot, "users", ue.Name(), ".teamix", "codebase-index", p.Name)
					if fi, err := os.Stat(idx); err == nil && fi.IsDir() {
						pv.Indexed = true
						pv.IndexAt = idx
						break
					}
				}
			}
			projects = append(projects, pv)
		}
	}

	writeJSON(w, map[string]any{
		"ragflow":  rag,
		"mcp":      mcp,
		"projects": projects,
		"help": map[string]string{
			"index":     "在 MCP 设置中添加 codebase-memory-mcp 服务器后，对项目调用 index_repository 建索引；检索模型 doc_kb_search 已内置。",
			"embedding": "RAGFlow 检索需要服务端配置 embedding 模型；未配置时 doc_kb_search 会返回 RAGFlow 的原始错误。",
		},
	})
}

// handleRAGIndex 架构师：确保 RAGFlow dataset 存在并上传一个文档。
// body: {"dataset":"teamix-docs"(默认),"filename":"notes.md","content":"..."}
func (ts *TeamixServer) handleRAGIndex(w http.ResponseWriter, r *http.Request, u *userSession) {
	if !ts.isArchitect(u) {
		http.Error(w, "仅架构师可上传知识库文档", http.StatusForbidden)
		return
	}
	var body struct {
		Dataset  string `json:"dataset"`
		Filename string `json:"filename"`
		Content  string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	body.Content = strings.TrimSpace(body.Content)
	if body.Content == "" {
		http.Error(w, `{"error":"content 不能为空"}`, http.StatusBadRequest)
		return
	}
	if body.Dataset == "" {
		body.Dataset = "teamix-docs"
	}
	if body.Filename == "" {
		body.Filename = "doc-" + time.Now().Format("20060102-150405") + ".md"
	}
	if os.Getenv("RAGFLOW_API_KEY") == "" {
		http.Error(w, `{"error":"RAGFLOW_API_KEY 未配置（项目 .env 中添加后重启）"}`, http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	client := ts.ragflowClient()
	ds, err := client.EnsureDataset(ctx, body.Dataset)
	if err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusInternalServerError)
		return
	}
	if err := client.UploadDocument(ctx, ds.ID, body.Filename, []byte(body.Content)); err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusInternalServerError)
		return
	}
	slog.Info("teamix: rag doc indexed", "by", u.name, "dataset", body.Dataset, "file", body.Filename)
	writeJSON(w, map[string]any{"ok": true, "dataset": body.Dataset, "filename": body.Filename})
}

// jsonEscape 生成可安全嵌入 JSON 字符串字面量的转义。
func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}
