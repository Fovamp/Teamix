package serve

import (
	"encoding/json"
	"net/http"

	"reasonix/internal/memory"
)

func (ts *TeamixServer) handleMemoryList(w http.ResponseWriter, r *http.Request, u *userSession) {
	memSet := u.ctrl.Memory()
	if memSet == nil || memSet.Store.Dir == "" {
		writeJSON(w, map[string]any{"memories": []any{}, "dir": ""})
		return
	}
	all := memSet.Store.List()
	type memEntry struct {
		Name        string `json:"name`
		Title       string `json:"title`
		Description string `json:"description`
		Type        string `json:"type`
		Body        string `json:"body`
	}
	out := make([]memEntry, len(all))
	for i, m := range all {
		out[i] = memEntry{
			Name: m.Name, Title: m.Title,
			Description: m.Description, Type: string(m.Type),
			Body: m.Body,
		}
	}
	writeJSON(w, map[string]any{"memories": out, "dir": memSet.Store.Dir})
}

func (ts *TeamixServer) handleMemorySave(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Name        string `json:"name`
		Title       string `json:"title`
		Description string `json:"description`
		Type        string `json:"type`
		Body        string `json:"body`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	memSet := u.ctrl.Memory()
	if memSet == nil || memSet.Store.Dir == "" {
		http.Error(w, "memory store not available", http.StatusInternalServerError)
		return
	}
	m := memory.Memory{
		Name: body.Name, Title: body.Title,
		Description: body.Description,
		Type:        memory.NormalizeType(body.Type),
		Body:        body.Body,
	}
	path, err := memSet.Store.Save(m)
	if err != nil {
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "name": body.Name, "path": path})
}
