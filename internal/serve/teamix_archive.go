package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	reasonixAgent "reasonix/internal/agent"
	"reasonix/internal/provider"
	reasonixStore "reasonix/internal/store"
)

// 会话归档（假删除）：红叉/右键删除 = 把会话整组（jsonl + 全部 sidecar + ckpt/jobs）
// 移入 <userRoot>/.teamix/archive/sessions/<项目>/，可在前端「会话」归档列表查看/恢复，
// 归档列表里的删除才是真删除。

// archiveSessionsDir 归档根目录（按项目隔离；未选项目 → tmp）。
func (ts *TeamixServer) archiveSessionsDir(u *userSession) string {
	proj := u.selectedProject
	if proj == "" {
		proj = "tmp"
	}
	return filepath.Join(u.userRoot, ".teamix", "archive", "sessions", proj)
}

// activeSessionsDir 当前活跃会话目录（与 boot.Build 的 SessionDir 一致）。
func (ts *TeamixServer) activeSessionsDir(u *userSession) string {
	if u.selectedProject == "" {
		return filepath.Join(u.userRoot, ".teamix", "tmp", "sessions")
	}
	return filepath.Join(u.userRoot, ".teamix", "sessions", u.selectedProject)
}

// archiveEntry 归档列表项。
type archiveEntry struct {
	Name    string `json:"name"`    // 会话 stem（不含 .jsonl）
	Title   string `json:"title"`   // 首条用户消息预览
	Turns   int    `json:"turns"`   // 用户轮数
	Time    string `json:"time"`    // 文件修改时间（消息本身无时间戳）
	Project string `json:"project"` // 所属项目（tmp = 未选项目）
}

// handleArchive 归档 API（按 method 分发）：
//
//	GET  → 列表；POST → 归档（body: {name} 单个；{mode: current|others|all} 批量）。
func (ts *TeamixServer) handleArchive(w http.ResponseWriter, r *http.Request, u *userSession) {
	switch r.Method {
	case http.MethodGet:
		ts.archiveList(w, u)
	case http.MethodPost:
		ts.archivePost(w, r, u)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// archiveList GET /teamix/archive：列出当前项目的归档会话（最新在前）。
func (ts *TeamixServer) archiveList(w http.ResponseWriter, u *userSession) {
	dir := ts.archiveSessionsDir(u)
	var out []archiveEntry
	entries, err := os.ReadDir(dir)
	if err != nil {
		writeJSON(w, map[string]any{"archives": []archiveEntry{}})
		return
	}
	for _, e := range entries {
		if e.IsDir() || !reasonixStore.IsSessionTranscriptName(e.Name()) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		first, turns := reasonixAgent.SessionPreview(path)
		fi, _ := e.Info()
		out = append(out, archiveEntry{
			Name:    strings.TrimSuffix(e.Name(), ".jsonl"),
			Title:   first,
			Turns:   turns,
			Time:    fi.ModTime().Format(time.RFC3339),
			Project: ts.archiveProjectOf(u),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time > out[j].Time })
	if out == nil {
		out = []archiveEntry{}
	}
	writeJSON(w, map[string]any{"archives": out})
}

func (ts *TeamixServer) archiveProjectOf(u *userSession) string {
	if u.selectedProject == "" {
		return "tmp"
	}
	return u.selectedProject
}

// archivePost POST /teamix/archive：归档指定会话（{name}）或批量（{mode}）。
// 红叉=单个归档；右键「归档其他/归档全部」= 批量。整组移动（含 sidecar），不真删。
func (ts *TeamixServer) archivePost(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Name string `json:"name"`
		Mode string `json:"mode"` // current | others | all（name 为空时生效）
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	srcDir := ts.activeSessionsDir(u)
	dstDir := ts.archiveSessionsDir(u)
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	failures := ts.archiveBatch(u, srcDir, dstDir, body.Name, body.Mode)
	if len(failures) > 0 {
		http.Error(w, "归档失败（文件被占用）: "+strings.Join(failures, ", "), http.StatusConflict)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// archiveBatch 执行归档：name 非空归档单个；否则按 mode（current/others/all）批量。
// 返回失败的文件名列表（整组移动中任一文件失败即记入）。
func (ts *TeamixServer) archiveBatch(u *userSession, srcDir, dstDir, name, mode string) []string {
	var targets []string
	if name != "" {
		if !validSessionStem(name) {
			return []string{name}
		}
		targets = []string{name}
	} else {
		current := ""
		if u.ctrl != nil {
			current = strings.TrimSuffix(filepath.Base(u.ctrl.SessionPath()), ".jsonl")
		}
		entries, err := os.ReadDir(srcDir)
		if err != nil {
			return nil
		}
		for _, e := range entries {
			if e.IsDir() || !reasonixStore.IsSessionTranscriptName(e.Name()) {
				continue
			}
			stem := strings.TrimSuffix(e.Name(), ".jsonl")
			if mode == "others" && stem == current {
				continue
			}
			if mode == "all" || mode == "others" || mode == "current" {
				targets = append(targets, stem)
			}
		}
		if mode == "current" && current != "" {
			targets = []string{current}
		}
	}
	var failed []string
	for _, stem := range targets {
		if err := ts.moveSessionGroup(srcDir, dstDir, stem); err != nil {
			failed = append(failed, stem)
		}
	}
	return failed
}

// moveSessionGroup 把一个会话整组从 srcDir 移到 dstDir：jsonl + 全部 sidecar
// + ckpt/jobs 目录。任一移动失败即中止该会话（返回错误，避免半移状态）。
func (ts *TeamixServer) moveSessionGroup(srcDir, dstDir, stem string) error {
	if !validSessionStem(stem) {
		return os.ErrInvalid
	}
	src := filepath.Join(srcDir, stem+".jsonl")
	if _, err := os.Stat(src); err != nil {
		return err // 会话不存在
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}
	// 常规文件组：jsonl + sidecar（store.SessionSidecarFiles 列出的权威组）
	paths := []string{src}
	paths = append(paths, reasonixStore.SessionSidecarFiles(src)...)
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			continue // sidecar 可能不存在（早期会话）
		}
		if err := os.Rename(p, filepath.Join(dstDir, filepath.Base(p))); err != nil {
			return err
		}
	}
	// 目录组：ckpt / jobs（存在才移）
	for _, suffix := range []string{".ckpt", ".jobs"} {
		dir := filepath.Join(srcDir, stem+suffix)
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			if err := os.Rename(dir, filepath.Join(dstDir, stem+suffix)); err != nil {
				return err
			}
		}
	}
	return nil
}

// handleArchiveRead GET /teamix/archive/read?name=：返回归档会话的轮次内容。
// 轮次边界 = 用户消息（排除系统注入的合成消息）；每轮含用户消息与后续回复。
func (ts *TeamixServer) handleArchiveRead(w http.ResponseWriter, r *http.Request, u *userSession) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if !validSessionStem(name) {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	path := filepath.Join(ts.archiveSessionsDir(u), name+".jsonl")
	sess, err := reasonixAgent.LoadSession(path)
	if err != nil {
		http.Error(w, "会话不存在或无法读取", http.StatusNotFound)
		return
	}
	// 按用户消息切轮
	var turns [][]archiveMsg
	var cur []archiveMsg
	for _, m := range sess.Messages {
		if m.Role == provider.RoleUser {
			if reasonixAgent.IsUserAuthoredTurn(m.Content) {
				if len(cur) > 0 {
					turns = append(turns, cur)
				}
				cur = []archiveMsg{}
			}
		}
		cur = append(cur, archiveMsg{Role: string(m.Role), Content: m.Content})
	}
	if len(cur) > 0 {
		turns = append(turns, cur)
	}
	if turns == nil {
		turns = [][]archiveMsg{}
	}
	writeJSON(w, map[string]any{"name": name, "turns": turns})
}

type archiveMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// handleArchiveRestore POST /teamix/archive/restore {name}：把归档会话移回活跃目录。
func (ts *TeamixServer) handleArchiveRestore(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !validSessionStem(body.Name) {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	srcDir := ts.archiveSessionsDir(u)
	dstDir := ts.activeSessionsDir(u)
	if err := ts.moveSessionGroup(srcDir, dstDir, body.Name); err != nil {
		http.Error(w, "恢复失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleArchiveDelete POST /teamix/archive/delete {name}：真删除归档会话（整组含 sidecar）。
func (ts *TeamixServer) handleArchiveDelete(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !validSessionStem(body.Name) {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	dir := ts.archiveSessionsDir(u)
	path := filepath.Join(dir, body.Name+".jsonl")
	if _, err := os.Stat(path); err != nil {
		http.Error(w, "归档会话不存在", http.StatusNotFound)
		return
	}
	failed := false
	paths := []string{path}
	paths = append(paths, reasonixStore.SessionSidecarFiles(path)...)
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if !removeFileWithRetry(p, 5) {
			failed = true
		}
	}
	for _, suffix := range []string{".ckpt", ".jobs"} {
		_ = removeAllWithRetry(filepath.Join(dir, body.Name+suffix), 3)
	}
	if failed {
		http.Error(w, "删除失败（文件被占用），请稍后重试", http.StatusConflict)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// validSessionStem 校验会话名（防止路径穿越）。
func validSessionStem(name string) bool {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return false
	}
	return true
}
