package serve

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"reasonix/internal/event"
)

// AI 文件操作日志（回合窗口方案）：
//   用户提交消息（handleSubmit）→ beginTurn：上一轮 new→old + 快照项目（内容库）
//   AI 回合结束（controller OnTurnDone）→ endTurn：diff 回合起点 vs 当前 → 记录
// 覆盖任意写文件方式（工具 / bash / python 脚本内部写），与 git 隔离，全部可撤销。
//
// 存储布局（users/<user>/.teamix/fileops/）：
//   base/<sha256>   内容寻址内容库（去重，跨会话累积）
//   index.json      最近一次快照（project → relPath → hash/size）
//   ops.json        操作记录（new | old | acked | undone）
//   undo/<opID>     撤销快照（旧内容全文；created 操作为空文件标记）

type fileOp struct {
	ID      string `json:"id"`
	Project string `json:"project"`
	Path    string `json:"path"` // 相对项目根
	Kind    string `json:"kind"` // modified | created | deleted
	Time    string `json:"time"`
	Status  string `json:"status"` // new | old | acked | undone
	Session string `json:"session"`
	Turn    int    `json:"turn"`
	Issue   string `json:"issue"` // 备注：该轮用户输入摘要（解决什么问题）
	HasUndo bool   `json:"hasUndo"`
	abs     string // 绝对路径（撤销用，不出 JSON）
}

type fileMeta struct {
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

// fileOpsManager per-user：内容库 + 回合快照 + 操作记录（跨会话/跨重启持久化）。
type fileOpsManager struct {
	mu      sync.Mutex
	root    string // userRoot/.teamix/fileops
	baseDir string // root/base
	ops     map[string]*fileOp
	ordered []string
	index   map[string]map[string]fileMeta // project -> relPath -> meta
	turn    int
	session string
	issue   string
	bc      *Broadcaster // SSE 广播（可为 nil）
}

// 快照排除目录（构建产物/版本库/IDE 噪音）。
var fileOpsSkipDirs = map[string]bool{
	".git": true, ".idea": true, ".vscode": true, ".teamix": true, ".reasonix": true,
	"node_modules": true, "target": true, "dist": true, "build": true, "out": true,
	"__pycache__": true, ".venv": true, "venv": true, ".next": true, ".m2": true, "logs": true,
}

func newFileOpsManager(userRoot string, bc *Broadcaster) *fileOpsManager {
	m := &fileOpsManager{
		root:    filepath.Join(userRoot, ".teamix", "fileops"),
		baseDir: "",
		ops:     make(map[string]*fileOp),
		index:   map[string]map[string]fileMeta{},
		bc:      bc,
	}
	m.baseDir = filepath.Join(m.root, "base")
	_ = os.MkdirAll(m.baseDir, 0o755)
	_ = os.MkdirAll(filepath.Join(m.root, "undo"), 0o755)
	m.load()
	return m
}

func fileOpRandID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "op-" + time.Now().Format("150405.000")
	}
	return "op-" + hex.EncodeToString(b)
}

func hashContent(data []byte) string {
	s := sha256.Sum256(data)
	return hex.EncodeToString(s[:])
}

// storeContent 内容寻址入库，返回 hash。
func (m *fileOpsManager) storeContent(data []byte) string {
	h := hashContent(data)
	if _, err := os.Stat(filepath.Join(m.baseDir, h)); err == nil {
		return h // 已存在（去重）
	}
	_ = os.WriteFile(filepath.Join(m.baseDir, h), data, 0o644)
	return h
}

func (m *fileOpsManager) load() {
	if data, err := os.ReadFile(filepath.Join(m.root, "ops.json")); err == nil {
		var list []*fileOp
		if json.Unmarshal(data, &list) == nil {
			for _, op := range list {
				if op.Status == "new" || op.Status == "old" {
					op.abs = filepath.Join(filepath.Dir(m.root), op.Project, op.Path)
					m.ops[op.ID] = op
					m.ordered = append(m.ordered, op.ID)
				}
			}
		}
	}
	if data, err := os.ReadFile(filepath.Join(m.root, "index.json")); err == nil {
		_ = json.Unmarshal(data, &m.index)
	}
}

func (m *fileOpsManager) save() {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := make([]*fileOp, 0, len(m.ordered))
	for _, id := range m.ordered {
		if op, ok := m.ops[id]; ok {
			cp := *op
			cp.abs = ""
			list = append(list, &cp)
		}
	}
	if data, err := json.Marshal(list); err == nil {
		_ = os.WriteFile(filepath.Join(m.root, "ops.json"), data, 0o644)
	}
	if data, err := json.Marshal(m.index); err == nil {
		_ = os.WriteFile(filepath.Join(m.root, "index.json"), data, 0o644)
	}
}

// snapshot 全项目快照：遍历源码（跳过构建产物/版本库），内容入库并更新 index。
// 全量 hash 对比（不能只比 size：AI 等长替换 size 不变会漏检）。
func (m *fileOpsManager) snapshot() {
	for _, projDir := range m.projectDirs() {
		project := filepath.Base(projDir)
		prev := m.index[project]
		next := map[string]fileMeta{}
		_ = filepath.WalkDir(projDir, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if fileOpsSkipDirs[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			rel, rerr := filepath.Rel(projDir, p)
			if rerr != nil {
				return nil
			}
			relSlash := filepath.ToSlash(rel)
			data, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			next[relSlash] = fileMeta{Hash: m.storeContent(data), Size: int64(len(data))}
			_ = prev // prev 仅作参考，全量重算保证起点准确
			return nil
		})
		m.index[project] = next
	}
}

// projectDirs 返回用户项目克隆目录（userRoot/<Project>）。
func (m *fileOpsManager) projectDirs() []string {
	userRoot := filepath.Dir(filepath.Dir(m.root)) // fileops -> .teamix -> userRoot
	var out []string
	ents, err := os.ReadDir(userRoot)
	if err != nil {
		return nil
	}
	for _, e := range ents {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := filepath.Join(userRoot, e.Name())
		if _, err := os.Stat(filepath.Join(p, ".git")); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// beginTurn 用户提交消息时调用：上一轮 new → old（默认同意），快照当前项目作为
// 本轮起点。session/issue 记录备注（会话名 / 轮次 / 用户输入摘要）。
func (m *fileOpsManager) beginTurn(session, issue string) {
	m.mu.Lock()
	m.turn++
	m.session = session
	m.issue = issue
	for _, id := range m.ordered {
		if op, ok := m.ops[id]; ok && op.Status == "new" {
			op.Status = "old"
		}
	}
	m.mu.Unlock()
	m.snapshot()
	m.save()
}

// endTurn AI 回合结束时调用：diff 回合起点快照 vs 当前，把本轮 AI 改动记录为
// new 操作（含 undo 快照 = 回合起点内容），SSE 广播。覆盖任何写文件方式。
func (m *fileOpsManager) endTurn() {
	type change struct {
		abs     string
		project string
		rel     string
		kind    string // modified | created | deleted
		oldHash string
	}
	var changes []change
	for _, projDir := range m.projectDirs() {
		project := filepath.Base(projDir)
		prev := m.index[project]
		now := map[string]fileMeta{}
		_ = filepath.WalkDir(projDir, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if fileOpsSkipDirs[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			rel, rerr := filepath.Rel(projDir, p)
			if rerr != nil {
				return nil
			}
			relSlash := filepath.ToSlash(rel)
			data, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			h := m.storeContent(data)
			now[relSlash] = fileMeta{Hash: h, Size: int64(len(data))}
			old, had := prev[relSlash]
			if !had {
				changes = append(changes, change{p, project, relSlash, "created", ""})
				return nil
			}
			if old.Hash != h {
				changes = append(changes, change{p, project, relSlash, "modified", old.Hash})
			}
			return nil
		})
		// 被删除的文件
		for rel, old := range prev {
			if _, ok := now[rel]; !ok {
				changes = append(changes, change{"", project, rel, "deleted", old.Hash})
			}
		}
	}
	// 更新 index 到当前（供下一轮 beginTurn 作起点）；skipDirs 复用 + size 复用，增量快
	m.snapshot()
	// 记录操作
	for _, c := range changes {
		m.record(c.abs, c.project, c.rel, c.kind, c.oldHash)
	}
	m.save()
}

// record 记录一条 new 操作并写 undo 快照。
func (m *fileOpsManager) record(abs, project, rel, kind, oldHash string) {
	id := fileOpRandID()
	op := &fileOp{
		ID: id, Project: project, Path: rel, Kind: kind,
		Time: time.Now().Format(time.RFC3339), Status: "new",
		Session: m.session, Turn: m.turn, Issue: m.issue, HasUndo: true,
		abs: abs,
	}
	// undo 快照 = 回合起点内容（旧 hash 内容或空=新建）
	var oldData []byte
	if oldHash != "" {
		oldData, _ = os.ReadFile(filepath.Join(m.baseDir, oldHash))
	}
	_ = os.WriteFile(filepath.Join(m.root, "undo", id), oldData, 0o644)
	if kind == "created" && len(oldData) == 0 {
		// 空 undo 内容 + created = 撤销时删除文件
	}
	m.mu.Lock()
	m.ops[id] = op
	m.ordered = append(m.ordered, id)
	m.mu.Unlock()
	m.broadcast(op)
}

func (m *fileOpsManager) broadcast(op *fileOp) {
	if m.bc == nil {
		return
	}
	if data, err := json.Marshal(op); err == nil {
		m.bc.Emit(event.Event{Kind: event.FileOp, Level: event.LevelInfo, Text: string(data)})
	}
}

// list 返回未闭环操作（new + old，新→旧）。
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

// ack 确认：删除 undo 快照，从列表移除（彻底闭环）。
func (m *fileOpsManager) ack(id string) bool {
	m.mu.Lock()
	op, ok := m.ops[id]
	if !ok || (op.Status != "new" && op.Status != "old") {
		m.mu.Unlock()
		return false
	}
	delete(m.ops, id)
	m.removeOrdered(id)
	m.mu.Unlock()
	_ = os.Remove(filepath.Join(m.root, "undo", id))
	m.save()
	return true
}

// ackAll 确认某项目（或全部）未闭环操作。
func (m *fileOpsManager) ackAll(project string) int {
	m.mu.Lock()
	var ids []string
	for id, op := range m.ops {
		if project != "" && op.Project != project {
			continue
		}
		if op.Status == "new" || op.Status == "old" {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		delete(m.ops, id)
		m.removeOrdered(id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = os.Remove(filepath.Join(m.root, "undo", id))
	}
	m.save()
	return len(ids)
}

// undo 撤销：从 undo 快照恢复（created → 删除文件；其余写回旧内容）。撤销即闭环移除。
func (m *fileOpsManager) undo(id string) bool {
	m.mu.Lock()
	op, ok := m.ops[id]
	if !ok || (op.Status != "new" && op.Status != "old") {
		m.mu.Unlock()
		return false
	}
	m.mu.Unlock()
	data, err := os.ReadFile(filepath.Join(m.root, "undo", id))
	if err != nil {
		return false
	}
	if op.Kind == "created" && len(data) == 0 {
		_ = os.Remove(op.abs) // 新建文件：撤销 = 删除
	} else if op.Kind == "deleted" {
		if err := os.MkdirAll(filepath.Dir(op.abs), 0o755); err == nil {
			_ = os.WriteFile(op.abs, data, 0o644) // 删除的文件：恢复
		}
	} else {
		if len(data) == 0 {
			_ = os.Remove(op.abs) // 旧内容为空 → 删除（等价回到空文件/不存在）
		} else if err := os.WriteFile(op.abs, data, 0o644); err != nil {
			return false
		}
	}
	m.mu.Lock()
	delete(m.ops, id)
	m.removeOrdered(id)
	m.mu.Unlock()
	_ = os.Remove(filepath.Join(m.root, "undo", id))
	m.save()
	return true
}

func (m *fileOpsManager) removeOrdered(id string) {
	for i, v := range m.ordered {
		if v == id {
			m.ordered = append(m.ordered[:i], m.ordered[i+1:]...)
			return
		}
	}
}

// ── HTTP handlers ──

// GET /teamix/fileops?project=X 未闭环的 AI 文件操作（用户全局、跨会话）。
func (ts *TeamixServer) handleFileOpsList(w http.ResponseWriter, r *http.Request, u *userSession) {
	writeJSON(w, u.ops.list(r.URL.Query().Get("project")))
}

// POST /teamix/fileops/ack {id} 确认（保留改动，删除快照）。
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

// POST /teamix/fileops/ack_all {project?} 全部确认。
func (ts *TeamixServer) handleFileOpsAckAll(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Project string `json:"project"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	n := u.ops.ackAll(body.Project)
	writeJSON(w, map[string]any{"ok": true, "acked": n})
}

// POST /teamix/fileops/undo {id} 撤销（从快照恢复，撤销即闭环移除）。
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
