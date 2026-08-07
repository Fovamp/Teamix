package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/control"
)

func (ts *TeamixServer) handleUserRole(w http.ResponseWriter, r *http.Request, u *userSession) {
	role := "developer"
	if ts.GlobalCfg() != nil && ts.GlobalCfg().IsArchitect(u.name) {
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
	if !removeFileWithRetry(target, 5) {
		http.Error(w, "删除失败（文件被占用），请稍后重试", http.StatusConflict)
		return
	}
	// 清理遗留的 checkpoint 目录（<name>.ckpt），避免删除会话后留下垃圾文件。
	_ = removeAllWithRetry(strings.TrimSuffix(target, ".jsonl")+".ckpt", 3)
	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteSessions 批量删除会话：mode="others" 删除除当前会话外的全部；
// mode="all" 删除全部（含当前）。同步清理各会话的 checkpoint 目录。
// 删除失败（Windows 上文件可能被短暂占用）会重试，仍失败则返回具体文件名，
// 不再静默吞掉——否则会出现"其他都删了，当前会话没删掉"。
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
	var failed []string
	for _, e := range ents {
		if e.IsDir() || filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}
		if mode == "others" && e.Name() == current {
			continue
		}
		full := filepath.Join(dir, e.Name())
		if !removeFileWithRetry(full, 5) {
			failed = append(failed, e.Name())
			continue
		}
		_ = removeAllWithRetry(strings.TrimSuffix(full, ".jsonl")+".ckpt", 3)
	}
	if len(failed) > 0 {
		http.Error(w, "删除失败（文件被占用）: "+strings.Join(failed, ", "), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// removeFileWithRetry 删除文件并短暂重试，绕过 Windows 上杀毒/索引的瞬态占用。
func removeFileWithRetry(path string, attempts int) bool {
	for i := 0; i < attempts; i++ {
		err := os.Remove(path)
		if err == nil {
			return true
		}
		if !os.IsNotExist(err) {
			time.Sleep(80 * time.Millisecond)
			continue
		}
		return true // 已不存在，视为删除成功
	}
	return false
}

func removeAllWithRetry(path string, attempts int) bool {
	for i := 0; i < attempts; i++ {
		err := os.RemoveAll(path)
		if err == nil {
			return true
		}
		time.Sleep(80 * time.Millisecond)
	}
	return false
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
	if ts.GlobalCfg() != nil {
		return ts.GlobalCfg().IsArchitect(u.name)
	}
	return false
}

func (ts *TeamixServer) getArchitects() []string {
	if ts.GlobalCfg() != nil {
		return ts.GlobalCfg().ArchitectNames()
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
