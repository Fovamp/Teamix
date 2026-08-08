package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// POST /teamix/filetree/ops 文件树文件操作（新建文件/新建目录/重命名/删除）。
// body: {action: mkdir|write|rename|delete, project, path, name?, content?}
// path/name 均为相对项目根路径；严格限制在项目克隆内（防穿越）。
func (ts *TeamixServer) handleFileTreeOps(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Action  string `json:"action"`
		Project string `json:"project"`
		Path    string `json:"path"`
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	if body.Project == "" || !safeModuleName(body.Project) {
		http.Error(w, `{"error":"project required"}`, http.StatusBadRequest)
		return
	}
	projPath := filepath.Join(u.userRoot, body.Project)
	if _, err := os.Stat(projPath); err != nil {
		http.Error(w, `{"error":"project not found"}`, http.StatusNotFound)
		return
	}
	resolve := func(p string) (string, bool) {
		if p == "" || strings.Contains(p, "\x00") {
			return "", false
		}
		abs := filepath.Join(projPath, filepath.FromSlash(p))
		rel, err := filepath.Rel(projPath, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", false
		}
		return abs, true
	}
	switch body.Action {
	case "mkdir":
		abs, ok := resolve(body.Path)
		if !ok {
			http.Error(w, `{"error":"invalid path"}`, http.StatusBadRequest)
			return
		}
		if err := os.MkdirAll(abs, 0o755); err != nil {
			http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusInternalServerError)
			return
		}
	case "write":
		abs, ok := resolve(body.Path)
		if !ok {
			http.Error(w, `{"error":"invalid path"}`, http.StatusBadRequest)
			return
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err == nil {
			if err := os.WriteFile(abs, []byte(body.Content), 0o644); err != nil {
				http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusInternalServerError)
				return
			}
		} else {
			http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusInternalServerError)
			return
		}
	case "rename":
		abs, ok := resolve(body.Path)
		if !ok || body.Name == "" {
			http.Error(w, `{"error":"invalid path"}`, http.StatusBadRequest)
			return
		}
		dst, ok := resolve(body.Name)
		if !ok {
			http.Error(w, `{"error":"invalid name"}`, http.StatusBadRequest)
			return
		}
		if err := os.Rename(abs, dst); err != nil {
			http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusInternalServerError)
			return
		}
	case "delete":
		abs, ok := resolve(body.Path)
		if !ok {
			http.Error(w, `{"error":"invalid path"}`, http.StatusBadRequest)
			return
		}
		if err := os.RemoveAll(abs); err != nil {
			http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusInternalServerError)
			return
		}
	default:
		http.Error(w, `{"error":"unknown action"}`, http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
