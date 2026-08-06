package serve

import (
	"encoding/json"
	"net/http"
	"path/filepath"

	"reasonix/internal/memory"
)

// 私有记忆：每个用户的 controller memory store（users/<name>/.teamix/<project>/…，
// 切项目重建 controller 后按项目隔离）。
// 全局记忆：workspaceRoot/.teamix/memory/<project>/（架构师按项目维护，
// 全员在对应项目只读继承）。

// globalMemoryStore 返回某项目下的全局记忆存储（架构师维护，全员只读）。
// project 为空时归入根目录（历史数据）；未选项目的读写由调用方拒绝。
func (ts *TeamixServer) globalMemoryStore(project string) memory.Store {
	base := filepath.Join(ts.workspaceRoot, ".teamix", "memory")
	if project != "" {
		base = filepath.Join(base, project)
	}
	return memory.Store{Dir: base}
}

func (ts *TeamixServer) handleMemoryList(w http.ResponseWriter, r *http.Request, u *userSession) {
	// 记忆按项目隔离：未选择项目时一律返回空（前端提示先选项目，
	// 与总结一致——点进对应项目后才加载该项目的记忆）。
	if u.selectedProject == "" {
		writeJSON(w, map[string]any{"memories": []any{}, "dir": "", "scope": r.URL.Query().Get("scope")})
		return
	}
	scope := r.URL.Query().Get("scope") // "global" → 全局记忆（全员可读）
	if scope == "global" {
		all := ts.globalMemoryStore(u.selectedProject).List()
		writeJSON(w, map[string]any{"memories": memJSON(all), "dir": ts.globalMemoryStore(u.selectedProject).Dir, "scope": "global"})
		return
	}
	memSet := u.ctrl.Memory()
	if memSet == nil || memSet.Store.Dir == "" {
		writeJSON(w, map[string]any{"memories": []any{}, "dir": ""})
		return
	}
	writeJSON(w, map[string]any{"memories": memJSON(memSet.Store.List()), "dir": memSet.Store.Dir, "scope": "private"})
}

type memEntry struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Sensitivity string `json:"sensitivity"` // 数据源敏感级声明（空 = 未声明）
	Body        string `json:"body"`
}

func memJSON(all []memory.Memory) []memEntry {
	out := make([]memEntry, len(all))
	for i, m := range all {
		out[i] = memEntry{
			Name: m.Name, Title: m.Title,
			Description: m.Description, Type: string(m.Type),
			Sensitivity: m.Sensitivity,
			Body:        m.Body,
		}
	}
	return out
}

func (ts *TeamixServer) handleMemorySave(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Name        string `json:"name"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Type        string `json:"type"`
		Sensitivity string `json:"sensitivity"` // public/internal/redact/confidential
		Body        string `json:"body"`
		Scope       string `json:"scope"` // "global" (architect) | "private" (default)
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if body.Scope != "" && body.Scope != "global" && body.Scope != "private" {
		http.Error(w, `{"error":"invalid scope (global|private)"}`, http.StatusBadRequest)
		return
	}
	m := memory.Memory{
		Name: body.Name, Title: body.Title,
		Description: body.Description,
		Type:        memory.NormalizeType(body.Type),
		Sensitivity: body.Sensitivity,
		Body:        body.Body,
	}
	// 全局记忆仅架构师可写（按项目隔离：未选项目拒绝）
	if body.Scope == "global" {
		if !ts.isArchitect(u) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if u.selectedProject == "" {
			http.Error(w, "请先选择项目再维护全局记忆", http.StatusBadRequest)
			return
		}
		path, err := ts.globalMemoryStore(u.selectedProject).Save(m)
		if err != nil {
			http.Error(w, "save failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "name": body.Name, "path": path, "scope": "global"})
		return
	}
	memSet := u.ctrl.Memory()
	if memSet == nil || memSet.Store.Dir == "" {
		http.Error(w, "memory store not available", http.StatusInternalServerError)
		return
	}
	path, err := memSet.Store.Save(m)
	if err != nil {
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "name": body.Name, "path": path, "scope": "private"})
}

func (ts *TeamixServer) handleMemoryDelete(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Name  string `json:"name"`
		Scope string `json:"scope"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if body.Scope == "global" {
		if !ts.isArchitect(u) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if u.selectedProject == "" {
			http.Error(w, "请先选择项目", http.StatusBadRequest)
			return
		}
		if err := ts.globalMemoryStore(u.selectedProject).Delete(body.Name); err != nil {
			http.Error(w, "delete failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
		return
	}
	memSet := u.ctrl.Memory()
	if memSet == nil || memSet.Store.Dir == "" {
		http.Error(w, "memory store not available", http.StatusInternalServerError)
		return
	}
	if err := memSet.Store.Delete(body.Name); err != nil {
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
