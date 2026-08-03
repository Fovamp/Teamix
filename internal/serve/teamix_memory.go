package serve

import (
	"encoding/json"
	"net/http"
	"path/filepath"

	"reasonix/internal/memory"
)

// 私有记忆：每个用户的 controller memory store（users/<name>/...）。
// 全局记忆：workspaceRoot/.teamix/memory/（架构师维护，全员只读继承）。

func (ts *TeamixServer) globalMemoryStore() memory.Store {
	return memory.Store{Dir: filepath.Join(ts.workspaceRoot, ".teamix", "memory")}
}

func (ts *TeamixServer) handleMemoryList(w http.ResponseWriter, r *http.Request, u *userSession) {
	scope := r.URL.Query().Get("scope") // "global" → 全局记忆（全员可读）
	if scope == "global" {
		all := ts.globalMemoryStore().List()
		writeJSON(w, map[string]any{"memories": memJSON(all), "dir": ts.globalMemoryStore().Dir, "scope": "global"})
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
	Body        string `json:"body"`
}

func memJSON(all []memory.Memory) []memEntry {
	out := make([]memEntry, len(all))
	for i, m := range all {
		out[i] = memEntry{
			Name: m.Name, Title: m.Title,
			Description: m.Description, Type: string(m.Type),
			Body: m.Body,
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
		Body:        body.Body,
	}
	// 全局记忆仅架构师可写
	if body.Scope == "global" {
		if !ts.isArchitect(u) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		path, err := ts.globalMemoryStore().Save(m)
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
		if err := ts.globalMemoryStore().Delete(body.Name); err != nil {
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
