package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/skill"
)

// Skills CRUD：全局（workspaceRoot/.reasonix/skills，架构师维护，全员继承）
// 与私有（users/<name>/.reasonix/skills，本人维护）。
// AllSkills() 是 live 扫描，写文件后无需重启即可在前端看到。

const teamixSkillFile = "SKILL.md"

func (ts *TeamixServer) globalSkillsDir() string {
	return filepath.Join(ts.workspaceRoot, ".reasonix", "skills")
}

func (ts *TeamixServer) userSkillsDir(u *userSession) string {
	return filepath.Join(u.userRoot, ".reasonix", "skills")
}

// skillDirFor 解析 scope → 目标目录，并做权限校验（全局仅架构师）。
func (ts *TeamixServer) skillDirFor(u *userSession, scope string) (string, error) {
	switch scope {
	case "global":
		if !ts.isArchitect(u) {
			return "", errForbidden
		}
		return ts.globalSkillsDir(), nil
	default:
		return ts.userSkillsDir(u), nil
	}
}

var errForbidden = &statusError{code: http.StatusForbidden, msg: "forbidden"}

type statusError struct {
	code int
	msg  string
}

func (e *statusError) Error() string { return e.msg }

// renderSkillFile 组装 <name>/SKILL.md：frontmatter description + sensitivity + body。
func renderSkillFile(name, description, sensitivity, body string) string {
	desc := strings.TrimSpace(description)
	desc = strings.ReplaceAll(desc, "\n", " ")
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString("description: " + desc + "\n")
	if s := strings.TrimSpace(sensitivity); s != "" {
		sb.WriteString("sensitivity: " + s + "\n")
	}
	sb.WriteString("---\n\n")
	sb.WriteString(strings.TrimLeft(body, "\n"))
	return sb.String()
}

// POST /teamix/skills/save  {name, scope, description, body}
func (ts *TeamixServer) handleSkillSave(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Name        string `json:"name"`
		Scope       string `json:"scope"` // "global" | "private" (default)
		Description string `json:"description"`
		Sensitivity string `json:"sensitivity"` // public/internal/redact/confidential
		Body        string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if body.Scope != "" && body.Scope != "global" && body.Scope != "private" {
		http.Error(w, `{"error":"invalid scope (global|private)"}`, http.StatusBadRequest)
		return
	}
	scope := body.Scope
	if scope == "" {
		scope = "private"
	}
	if !skill.IsValidName(body.Name) {
		http.Error(w, `{"error":"invalid skill name — letters, digits, '_', '-', '.'"}`, http.StatusBadRequest)
		return
	}
	dir, err := ts.skillDirFor(u, scope)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	folder := filepath.Join(dir, body.Name, teamixSkillFile)
	if err := os.MkdirAll(filepath.Dir(folder), 0o755); err != nil {
		http.Error(w, "create skill dir failed", http.StatusInternalServerError)
		return
	}
	content := renderSkillFile(body.Name, body.Description, body.Sensitivity, body.Body)
	if err := os.WriteFile(folder, []byte(content), 0o644); err != nil {
		http.Error(w, "write skill failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "name": body.Name, "scope": scope, "path": folder})
}

// POST /teamix/skills/delete  {name, scope}
func (ts *TeamixServer) handleSkillDelete(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Name  string `json:"name"`
		Scope string `json:"scope"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	scope := body.Scope
	if scope == "" {
		scope = "private"
	}
	if !skill.IsValidName(body.Name) {
		http.Error(w, `{"error":"invalid skill name"}`, http.StatusBadRequest)
		return
	}
	dir, err := ts.skillDirFor(u, scope)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	// 兼容目录式 <name>/SKILL.md 与扁平 <name>.md
	removed := false
	if err := os.Remove(filepath.Join(dir, body.Name, teamixSkillFile)); err == nil {
		removed = true
		_ = os.Remove(filepath.Join(dir, body.Name)) // 清空目录
	}
	if err := os.Remove(filepath.Join(dir, body.Name+".md")); err == nil {
		removed = true
	}
	if !removed {
		http.Error(w, `{"error":"skill not found"}`, http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
