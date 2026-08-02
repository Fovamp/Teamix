package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"reasonix/internal/control"
)

func (ts *TeamixServer) handleUserRole(w http.ResponseWriter, r *http.Request, u *userSession) {
	role := "developer"
	if ts.globalCfg != nil && ts.globalCfg.IsArchitect(u.name) {
		role = "architect"
	}
	writeJSON(w, map[string]any{"role": role, "user": u.name})
}

func (ts *TeamixServer) handleDeleteSession(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "//\\") {
		http.Error(w, "invalid session name", http.StatusBadRequest)
		return
	}
	dir := u.ctrl.SessionDir()
	if dir == "" {
		http.Error(w, "sessions disabled", http.StatusBadRequest)
		return
	}
	target := filepath.Join(dir, name+".jsonl")
	absDir, _ := filepath.Abs(dir)
	absTarget, _ := filepath.Abs(target)
	if !strings.HasPrefix(absTarget, absDir+string(os.PathSeparator)) && absTarget != absDir {
		http.Error(w, "invalid session path", http.StatusForbidden)
		return
	}
	if err := os.Remove(target); err != nil {
		if os.IsNotExist(err) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func sessionTitle(ctrl control.SessionAPI, name, firstMsg string) string {
	if firstMsg != "" {
		if len(firstMsg) > 60 {
			return firstMsg[:60] + "\u2026"
		}
		return firstMsg
	}
	return strings.TrimSuffix(name, ".jsonl")
}

func teamixCurrentModelRef(c control.SessionAPI) string {
	ref := strings.TrimSpace(c.ModelRef())
	if ref != "" {
		return ref
	}
	return strings.TrimSpace(c.Label())
}

func (ts *TeamixServer) isArchitect(u *userSession) bool {
	if ts.globalCfg != nil {
		return ts.globalCfg.IsArchitect(u.name)
	}
	return false
}

func (ts *TeamixServer) getArchitects() []string {
	if ts.globalCfg != nil {
		return ts.globalCfg.ArchitectNames()
	}
	return nil
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
