package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"reasonix/internal/ragflow"
	"reasonix/internal/tool"
)

func init() { tool.RegisterBuiltin(docKBSearch{}) }

// docKBSearch 从团队文档知识库（RAGFlow）做语义检索。
// 配置：RAGFLOW_API_KEY / RAGFLOW_BASE_URL（serve 启动时从项目 .env 注入进程环境）。
type docKBSearch struct{}

func (docKBSearch) Name() string { return "doc_kb_search" }

func (docKBSearch) Description() string {
	return "Search the team document knowledge base (RAGFlow) by semantic similarity and return the top matching chunks. Use for questions about team docs, policies, specs, or any material uploaded to the knowledge base. Requires RAGFLOW_API_KEY configured on the server."
}

func (docKBSearch) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "question":{"type":"string","description":"Natural-language search question"},
  "top_k":{"type":"integer","description":"Max chunks to return (default 5, max 20)"},
  "dataset":{"type":"string","description":"Optional dataset name to restrict the search to (default: all datasets)"}
},
"required":["question"]
}`)
}

func (docKBSearch) ReadOnly() bool { return true }

func (docKBSearch) SnipHint() tool.SnipHint {
	return tool.SnipHint{Head: 100, Tail: 10, HeadChars: 20000, TailChars: 3000}
}

func (docKBSearch) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Question string `json:"question"`
		TopK     int    `json:"top_k"`
		Dataset  string `json:"dataset"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("doc_kb_search: bad args: %w", err)
	}
	p.Question = strings.TrimSpace(p.Question)
	if p.Question == "" {
		return "", fmt.Errorf("doc_kb_search: question is required")
	}
	key := os.Getenv("RAGFLOW_API_KEY")
	if key == "" {
		return "", fmt.Errorf("doc_kb_search: RAGFLOW_API_KEY 未配置（在项目 .env 中添加后重启）")
	}
	c := ragflow.NewClient(os.Getenv("RAGFLOW_BASE_URL"), key)
	if p.TopK <= 0 {
		p.TopK = 5
	}
	if p.TopK > 20 {
		p.TopK = 20
	}
	ds, err := c.ListDatasets(ctx)
	if err != nil {
		return "", fmt.Errorf("doc_kb_search: 知识库连接失败: %w", err)
	}
	var ids []string
	if p.Dataset != "" {
		for _, d := range ds {
			if d.Name == p.Dataset {
				ids = append(ids, d.ID)
				break
			}
		}
		if len(ids) == 0 {
			return "", fmt.Errorf("doc_kb_search: dataset %q 不存在（现有: %v）", p.Dataset, datasetNames(ds))
		}
	} else {
		for _, d := range ds {
			ids = append(ids, d.ID)
		}
	}
	chunks, err := c.Retrieve(ctx, p.Question, ids, p.TopK)
	if err != nil {
		return "", fmt.Errorf("doc_kb_search: 检索失败: %w", err)
	}
	if len(chunks) == 0 {
		return "（知识库未检索到相关片段）", nil
	}
	var b strings.Builder
	for i, ch := range chunks {
		src := ch.Document
		if src == "" {
			src = "未知文档"
		}
		fmt.Fprintf(&b, "[%d] 来源: %s\n%s", i+1, src, strings.TrimSpace(ch.Content))
		if i < len(chunks)-1 {
			b.WriteString("\n\n")
		}
	}
	return b.String(), nil
}

func datasetNames(ds []ragflow.Dataset) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.Name)
	}
	return out
}
