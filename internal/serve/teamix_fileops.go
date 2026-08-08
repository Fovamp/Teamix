package serve

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"reasonix/internal/diff"
	"reasonix/internal/event"
)

// fileOp 一次 AI 对项目文件的写操作（文件树高亮 + 确认/取消，与 git 隔离）。
type fileOp struct {
	ID      string `json:"id"`
	Project string `json:"project"`
	Path    string `json:"path"` // 相对项目根
	Kind    string `json:"kind"`
	Time    string `json:"time"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	Status  string `json:"status"` // pending | acked | undone
	HasUndo bool   `json:"hasUndo"`
	abs     string // 绝对路径（撤销用，不出 JSON）
}

func fileOpRandID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "op-" + time.Now().Format("150405.000")
	}
	return "op-" + hex.EncodeToString(b)
}

// fileOpsManager per-user 操作记录：内存表 + ops.json 持久化 + undo/<id> 原文件快照。
// 确认=删快照；取消=从快照恢复原内容。重启 teamix.exe 后未确认操作仍可查看/处理。
type fileOpsManager struct {
	mu      sync.Mutex
	root    string // userRoot/.teamix/fileops
	ops     map[string]*fileOp
	ordered []string // 时间序
}

func newFileOpsManager(userRoot string) *fileOpsManager {
	m := &fileOpsManager{
		root: filepath.Join(userRoot, ".teamix", "fileops"),
		ops:  make(map[string]*fileOp),
	}
	_ = os.MkdirAll(filepath.Join(m.root, "undo"), 0o755)
	m.load()
	return m
}

func (m *fileOpsManager) load() {
	data, err := os.ReadFile(filepath.Join(m.root, "ops.json"))
	if err != nil {
		return
	}
	var list []*fileOp
	if json.Unmarshal(data, &list) != nil {
		return
	}
	for _, op := range list {
		if op.Status == "pending" { // 已确认/已撤销的闭环后不再展示
			op.abs = filepath.Join(filepath.Dir(m.root), op.Project, op.Path)
			m.ops[op.ID] = op
			m.ordered = append(m.ordered, op.ID)
		}
	}
}

func (m *fileOpsManager) save() {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := make([]*fileOp, 0, len(m.ordered))
	for _, id := range m.ordered {
		if op, ok := m.ops[id]; ok {
			op.abs = "" // 不落盘绝对路径
			list = append(list, op)
		}
	}
	data, _ := json.Marshal(list)
	_ = os.WriteFile(filepath.Join(m.root, "ops.json"), data, 0o644)
}

// add 记录一次写操作并保存原文件快照（供取消时恢复）。返回新操作。
func (m *fileOpsManager) add(absPath, project, sub string, ch diff.Change) *fileOp {
	id := fileOpRandID()
	op := &fileOp{
		ID: id, Project: project, Path: sub,
		Kind: string(ch.Kind), Time: time.Now().Format(time.RFC3339),
		Added: ch.Added, Removed: ch.Removed, Status: "pending", HasUndo: true,
		abs: absPath,
	}
	_ = os.WriteFile(filepath.Join(m.root, "undo", id), []byte(ch.OldText), 0o644)
	m.mu.Lock()
	m.ops[id] = op
	m.ordered = append(m.ordered, id)
	m.mu.Unlock()
	m.save()
	return op
}

// list 按项目返回 pending 操作（新→旧）。
func (m *fileOpsManager) list(project string) []fileOp {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []fileOp
	for i := len(m.ordered) - 1; i >= 0; i-- {
		op := m.ops[m.ordered[i]]
		if project == "" || op.Project == project {
			cp := *op
			cp.abs = ""
			out = append(out, cp)
		}
	}
	return out
}

// ack 确认操作：删除 undo 快照，标记 acked。
func (m *fileOpsManager) ack(id string) bool {
	m.mu.Lock()
	op, ok := m.ops[id]
	if !ok || op.Status != "pending" {
		m.mu.Unlock()
		return false
	}
	op.Status = "acked"
	op.HasUndo = false
	m.mu.Unlock()
	_ = os.Remove(filepath.Join(m.root, "undo", id))
	m.save()
	return true
}

// undo 取消操作：从快照恢复原文件（OldText 非空写回；空=新建文件则删除）。
func (m *fileOpsManager) undo(id string) bool {
	m.mu.Lock()
	op, ok := m.ops[id]
	if !ok || op.Status != "pending" {
		m.mu.Unlock()
		return false
	}
	m.mu.Unlock()
	data, err := os.ReadFile(filepath.Join(m.root, "undo", id))
	if err != nil {
		return false
	}
	if len(data) == 0 {
		// 新建文件：取消 = 删除
		_ = os.Remove(op.abs)
	} else {
		if err := os.WriteFile(op.abs, data, 0o644); err != nil {
			return false
		}
	}
	m.mu.Lock()
	op.Status = "undone"
	op.HasUndo = false
	m.mu.Unlock()
	_ = os.Remove(filepath.Join(m.root, "undo", id))
	m.save()
	return true
}

// onFileOp 是 boot.OnFileChange 回调：记录 + 快照 + SSE 广播。
// 只记录用户项目克隆下的文件（跳过 .teamix/.reasonix 自身）。
func onFileOp(userRoot string, ops *fileOpsManager, bc *Broadcaster, ch diff.Change) {
	abs := ch.Path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(userRoot, abs)
	}
	rel, err := filepath.Rel(userRoot, abs)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return
	}
	parts := strings.SplitN(filepath.ToSlash(rel), "/", 2)
	if len(parts) < 2 {
		return
	}
	project := parts[0]
	if project == ".teamix" || project == ".reasonix" {
		return
	}
	op := ops.add(abs, project, parts[1], ch)
	if bc != nil {
		if data, err := json.Marshal(op); err == nil {
			bc.Emit(event.Event{Kind: event.FileOp, Level: event.LevelInfo, Text: string(data)})
		}
	}
}

// GET /teamix/fileops?project=X 未确认的 AI 文件操作（当前用户，按项目过滤）。
func (ts *TeamixServer) handleFileOpsList(w http.ResponseWriter, r *http.Request, u *userSession) {
	ops := u.ops.list(r.URL.Query().Get("project"))
	writeJSON(w, ops)
}

// POST /teamix/fileops/ack {id} 确认操作（保留改动，删除快照）。
func (ts *TeamixServer) handleFileOpsAck(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		http.Error(w, `{"error":"fileop id required"}`, http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"ok": u.ops.ack(body.ID)})
}

// POST /teamix/fileops/undo {id} 取消操作（从快照恢复原文件）。
func (ts *TeamixServer) handleFileOpsUndo(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		http.Error(w, `{"error":"fileop id required"}`, http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"ok": u.ops.undo(body.ID)})
}
