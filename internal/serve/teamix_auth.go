package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reasonix/internal/control"
	"strings"
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
	// 清理遗留的 checkpoint 目录（<name>.ckpt），避免删除会话后留下垃圾文件。
	_ = os.RemoveAll(strings.TrimSuffix(target, ".jsonl") + ".ckpt")
	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteSessions 批量删除会话：mode="others" 删除除当前会话外的全部；
// mode="all" 删除全部（含当前）。同步清理各会话的 checkpoint 目录。
func (ts *TeamixServer) handleDeleteSessions(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Mode string `json:"mode"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	mode := body.Mode
	if mode != "others" && mode != "all" {
		http.Error(w, "mode must be 'others' or 'all'", http.StatusBadRequest)
		return
	}
	dir := u.ctrl.SessionDir()
	if dir == "" {
		http.Error(w, "sessions disabled", http.StatusBadRequest)
		return
	}
	current := filepath.Base(u.ctrl.SessionPath()) // 形如 0805-xxxx.jsonl
	ents, err := os.ReadDir(dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, e := range ents {
		if e.IsDir() || filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}
		if mode == "others" && e.Name() == current {
			continue
		}
		full := filepath.Join(dir, e.Name())
		_ = os.Remove(full)
		_ = os.RemoveAll(strings.TrimSuffix(full, ".jsonl") + ".ckpt")
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
