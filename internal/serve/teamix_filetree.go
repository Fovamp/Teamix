package serve

import (
	"bytes"
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

// GET /teamix/filetree/search?project=X&q=KW 项目文件内容搜索
// （仅文本文件、跳过构建产物/大文件，最多 200 个命中，防呆大小写不敏感）。
func (ts *TeamixServer) handleFileTreeSearch(w http.ResponseWriter, r *http.Request, u *userSession) {
	project := r.URL.Query().Get("project")
	q := r.URL.Query().Get("q")
	if project == "" || q == "" || !safeModuleName(project) {
		http.Error(w, `{"error":"project and q required"}`, http.StatusBadRequest)
		return
	}
	projPath := filepath.Join(u.userRoot, project)
	var hits []string
	const limit = 200
	lq := strings.ToLower(q)
	_ = filepath.WalkDir(projPath, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if fileOpsSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if fi, e := d.Info(); e != nil || fi.Size() > 512*1024 {
			return nil // 跳过大文件
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil || bytes.IndexByte(data, 0) >= 0 {
			return nil // 读取失败或疑似二进制
		}
		if bytes.Contains(data, []byte(q)) || bytes.Contains(bytes.ToLower(data), []byte(lq)) {
			if rel, rerr := filepath.Rel(projPath, p); rerr == nil {
				hits = append(hits, filepath.ToSlash(rel))
				if len(hits) >= limit {
					return filepath.SkipAll
				}
			}
		}
		return nil
	})
	if hits == nil {
		hits = []string{}
	}
	writeJSON(w, map[string]any{"hits": hits})
}
