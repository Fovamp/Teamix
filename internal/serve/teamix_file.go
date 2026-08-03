package serve

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sort"
)

type treeEntry struct {
	Name     string       `json:"name"`
	Path     string       `json:"path"`
	IsDir    bool         `json:"isDir"`
	Children []*treeEntry `json:"children,omitempty"`
}

func (ts *TeamixServer) handleUpload(w http.ResponseWriter, r *http.Request, u *userSession) {
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "upload too large or bad form", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	relPath := r.FormValue("path")
	if relPath == "" {
		relPath = header.Filename
	}
	relPath = filepath.Clean(relPath)
	if strings.Contains(relPath, "..") || filepath.IsAbs(relPath) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	fullPath := filepath.Join(u.userRoot, filepath.FromSlash(relPath))
	absRoot, _ := filepath.Abs(u.userRoot)
	absPath, _ := filepath.Abs(fullPath)
	if !strings.HasPrefix(absPath, absRoot) {
		http.Error(w, "path outside workspace", http.StatusForbidden)
		return
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		http.Error(w, "cannot create directory", http.StatusInternalServerError)
		return
	}
	dst, err := os.Create(fullPath)
	if err != nil {
		http.Error(w, "cannot create file", http.StatusInternalServerError)
		return
	}
	defer dst.Close()
	if _, err := io.Copy(dst, file); err != nil {
		http.Error(w, "write error", http.StatusInternalServerError)
		return
	}
	slog.Info("teamix: file uploaded", "path", relPath, "user", u.name)
	writeJSON(w, map[string]any{"ok": true, "path": filepath.ToSlash(relPath)})
}

func (ts *TeamixServer) handleFile(w http.ResponseWriter, r *http.Request, u *userSession) {
	path := r.URL.Query().Get("path")
	if path == "" || strings.Contains(path, "..") {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	fullPath := filepath.Join(u.userRoot, filepath.FromSlash(path))
	absRoot, _ := filepath.Abs(u.userRoot)
	absPath, _ := filepath.Abs(fullPath)
	if !strings.HasPrefix(absPath, absRoot) {
		http.Error(w, "path outside workspace", http.StatusForbidden)
		return
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	truncated := len(data) > 100000
	if truncated {
		data = data[:100000]
	}
	writeJSON(w, map[string]any{
		"path":      path,
		"body":      string(data),
		"size":      len(data),
		"truncated": truncated,
	})
}

func (ts *TeamixServer) handleTree(w http.ResponseWriter, r *http.Request, u *userSession) {
	// 未选项目：不展示原始工作区内容，前端显示"请先选择项目"
	if u.selectedProject == "" {
		writeJSON(w, map[string]any{"empty": true, "reason": "noProject"})
		return
	}
	root := filepath.Join(u.userRoot, u.selectedProject)
	tree := buildTree(root, "", 0, 15)
	if len(tree) == 0 {
		writeJSON(w, map[string]any{"empty": true, "reason": "emptyProject"})
		return
	}
	writeJSON(w, tree)
}

func buildTree(base, relPath string, depth, maxDepth int) []*treeEntry {
	if depth > maxDepth {
		return nil
	}
	fullPath := filepath.Join(base, relPath)
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return nil
	}
	skipDirs := map[string]bool{
		".git": true, ".teamix": true, ".reasonix": true,
		"node_modules": true, "vendor": true, ".venv": true,
		"__pycache__": true, "bin": true, "obj": true,
		".idea": true, ".vscode": true, ".mvn": true, "target": true,
		"dist": true, ".gradle": true, "build": true, "out": true,
	}
	// 配置文件（非点开头）也不在树中展示：树只用于浏览代码。
	skipFiles := map[string]bool{
		"reasonix.toml": true, ".gitignore": true, ".gitattributes": true,
		"config.toml": true, "README.md": true, "LICENSE": true,
		"package-lock.json": true, "pnpm-lock.yaml": true, "yarn.lock": true,
		"go.sum": true,
	}
	var out []*treeEntry
	for _, e := range entries {
		name := e.Name()
		if skipDirs[name] || skipFiles[name] || strings.HasPrefix(name, ".") {
			continue
		}
		childRel := filepath.Join(relPath, name)
		entry := &treeEntry{Name: name, Path: filepath.ToSlash(childRel), IsDir: e.IsDir()}
		if e.IsDir() {
			entry.Children = buildTree(base, childRel, depth+1, maxDepth)
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func (ts *TeamixServer) handleModules(w http.ResponseWriter, r *http.Request) {
	type modJSON struct {
		Name        string `json:"name"`
		Path        string `json:"path"`
		Description string `json:"description,omitempty"`
	}
	if ts.globalCfg == nil {
		writeJSON(w, []modJSON{})
		return
	}
	cfg := ts.globalCfg.Config
	if cfg == nil {
		writeJSON(w, []modJSON{})
		return
	}
	out := make([]modJSON, 0, len(cfg.Modules))
	for _, m := range cfg.Modules {
		out = append(out, modJSON{Name: m.Name, Path: m.Path, Description: m.Description})
	}
	writeJSON(w, out)
}
