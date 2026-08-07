package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/config"
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

// GET /teamix/skills/content?name=xxx 返回 skill 的正文（编辑回填用）。
// 内置 skill（编译 embed）也返回正文——前端"复制编辑"自动带出内容，
// 保存到私有目录即成为可编辑的副本（同名覆盖内置）。
func (ts *TeamixServer) handleSkillContent(w http.ResponseWriter, r *http.Request, u *userSession) {
	name := r.URL.Query().Get("name")
	if name == "" || !skill.IsValidName(name) {
		http.Error(w, `{"error":"bad skill name"}`, http.StatusBadRequest)
		return
	}
	for _, s := range u.ctrl.AllSkills() {
		if s.Name != name {
			continue
		}
		scope := "user"
		if s.Path == "" || s.Path == "(builtin)" {
			scope = "builtin"
		} else if s.Scope == skill.ScopeGlobal {
			scope = "global"
		}
		writeJSON(w, map[string]any{
			"name":        s.Name,
			"scope":       scope,
			"path":        s.Path,
			"body":        s.Body, // 正文（不含 frontmatter；保存时前端补 name/desc/sensitivity）
			"description": s.Description,
			"sensitivity": s.Sensitivity,
		})
		return
	}
	http.Error(w, `{"error":"skill not found"}`, http.StatusNotFound)
}

// POST /teamix/mcp/toggle  {name, enabled, scope}
// 启用/禁用 MCP server：false → 断开当前会话 + 配置记 Enabled=false（下次会话不加载）；
// true → 重新挂载 + 配置 Enabled=true。
func (ts *TeamixServer) handleMCPToggle(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
		Scope   string `json:"scope"` // "global" | "private" (default)
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// 定位配置（global 优先，否则 private）
	if _, isGlobal := ts.loadGlobalMCPServers()[body.Name]; isGlobal {
		if !ts.isArchitect(u) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if err := ts.setGlobalMCPEnabled(body.Name, body.Enabled); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		if err := ts.setUserMCPEnabled(u, body.Name, body.Enabled); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if body.Enabled {
		// 重新挂载（从配置读 command/args）
		specs := loadUserMCPServers(u.userRoot)
		spec, ok := specs[body.Name]
		if !ok {
			spec, ok = ts.loadGlobalMCPServers()[body.Name]
		}
		if ok {
			_, _ = u.ctrl.AddMCPServer(config.PluginEntry{Name: body.Name, Type: spec.Type, Command: spec.Command, Args: spec.Args})
		}
	} else {
		u.ctrl.DisconnectMCPServer(body.Name)
	}
	writeJSON(w, map[string]any{"ok": true, "name": body.Name, "enabled": body.Enabled})
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
