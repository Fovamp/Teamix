package serve

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"reasonix/internal/teamixconfig"
)

func (ts *TeamixServer) handleGitCredentials(w http.ResponseWriter, r *http.Request, u *userSession) {
	uc, err := teamixconfig.LoadUserConfig(u.userRoot)
	if err != nil {
		http.Error(w, `{"error":"failed to load user config"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"sshKeyPath":    uc.Git.SSHKeyPath,
		"httpsUsername": uc.Git.HTTPSUsername,
		"configured":    uc.HasGitCredentials(),
	})
}

func (ts *TeamixServer) handleGitCredentialsSave(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		SSHKeyPath    string `json:"sshKeyPath"`
		HTTPSUsername string `json:"httpsUsername"`
		HTTPSPassword string `json:"httpsPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}

	uc, err := teamixconfig.LoadUserConfig(u.userRoot)
	if err != nil {
		http.Error(w, `{"error":"failed to load user config"}`, http.StatusInternalServerError)
		return
	}

	// Update git config
	if body.SSHKeyPath != "" {
		uc.Git.SSHKeyPath = body.SSHKeyPath
		// Clear HTTPS if switching to SSH
		uc.Git.HTTPSUsername = ""
		uc.Git.HTTPSPassword = ""
	} else if body.HTTPSUsername != "" {
		uc.Git.HTTPSUsername = body.HTTPSUsername
		uc.Git.HTTPSPassword = body.HTTPSPassword
		uc.Git.SSHKeyPath = ""
	}

	// Validate
	if err := uc.ValidateGitCredentials(); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	if err := uc.SaveUserConfig(u.userRoot); err != nil {
		http.Error(w, `{"error":"failed to save user config"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (ts *TeamixServer) handleGitValidate(w http.ResponseWriter, r *http.Request, u *userSession) {
	uc, err := teamixconfig.LoadUserConfig(u.userRoot)
	if err != nil {
		writeJSON(w, map[string]any{"valid": false, "error": "failed to load config"})
		return
	}
	if err := uc.ValidateGitCredentials(); err != nil {
		writeJSON(w, map[string]any{"valid": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"valid": true})
}

func (ts *TeamixServer) handleProjectSelect(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Project string `json:"project"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Project == "" {
		http.Error(w, `{"error":"project name required"}`, http.StatusBadRequest)
		return
	}
	// 项目名只允许普通名称，防止 projects.yaml 配置的 name 含 .. 逃逸 userRoot。
	if filepath.Base(body.Project) != body.Project {
		http.Error(w, `{"error":"invalid project name"}`, http.StatusBadRequest)
		return
	}

	// Validate project exists in global config
	proj := ts.globalCfg.Projects.FindProject(body.Project)
	if proj == nil {
		http.Error(w, `{"error":"project not found"}`, http.StatusNotFound)
		return
	}

	// Check git credentials
	uc, err := teamixconfig.LoadUserConfig(u.userRoot)
	if err != nil {
		http.Error(w, `{"error":"failed to load user config"}`, http.StatusInternalServerError)
		return
	}

	projPath := filepath.Join(u.userRoot, body.Project)
	cloned := false

	// If project directory does not exist, clone it
	if _, err := os.Stat(projPath); os.IsNotExist(err) {
		// 按链接类型校验凭证：SSH 链接需 SSH Key，HTTPS 需账号，本地路径无需凭证
		var needCred string
		switch {
		case isSSHURL(proj.Git) && uc.Git.SSHKeyPath == "":
			needCred = "该项目的 git 链接是 SSH 格式，请先配置 SSH Key"
		case isHTTPSURL(proj.Git) && uc.Git.HTTPSUsername == "":
			needCred = "该项目的 git 链接是 HTTPS 格式，请先配置账号密码"
		case !uc.HasGitCredentials() && !isLocalURL(proj.Git):
			needCred = "git credentials not configured"
		}
		if needCred != "" {
			writeJSON(w, map[string]any{"ok": false, "needCredentials": true, "error": needCred})
			return
		}
		if err := ts.cloneProject(proj.Git, projPath, uc); err != nil {
			// 认证类错误（凭证错误/无权限）→ 弹凭证表单引导用户重新配置
			if isAuthError(err.Error()) {
				writeJSON(w, map[string]any{"ok": false, "needCredentials": true, "error": err.Error()})
				return
			}
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		cloned = true
	}

	// Switch user session to this project
	u.selectedProject = body.Project

	// 项目内 Teamix 配置不入 git：.teamix/ 与 .reasonix/ 全部忽略（幂等追加）。
	ensureProjectGitignore(projPath)

	// Switch controller session directory to the project's .teamix/sessions,
	// so new sessions and session listings are project-scoped.
	sessionDir := filepath.Join(projPath, ".teamix", "sessions")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": "create session dir: " + err.Error()})
		return
	}
	u.ctrl.SetSessionDir(sessionDir)

	writeJSON(w, map[string]any{
		"ok":      true,
		"project": body.Project,
		"path":    projPath,
		"cloned":  cloned,
	})
}

func (ts *TeamixServer) cloneProject(gitURL, targetPath string, uc *teamixconfig.UserConfig) error {
	cmd := exec.Command("git", "clone", gitURL, targetPath)

	// 按链接类型匹配凭证：SSH 链接用 SSH Key，HTTPS 链接用账号密码。
	switch {
	case isSSHURL(gitURL) && uc.Git.SSHKeyPath != "":
		cmd.Env = append(os.Environ(),
			fmt.Sprintf("GIT_SSH_COMMAND=ssh -i %q -o StrictHostKeyChecking=accept-new", uc.Git.SSHKeyPath),
		)
	case isHTTPSURL(gitURL) && uc.Git.HTTPSUsername != "":
		if u, err := url.Parse(gitURL); err == nil && u.Scheme == "https" {
			u.User = url.UserPassword(uc.Git.HTTPSUsername, uc.Git.HTTPSPassword)
			cmd = exec.Command("git", "clone", u.String(), targetPath)
		}
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg == "" {
			msg = err.Error()
		}
		// 凭证缺失/错误时给出中文提示（提取 git 真正错误行，跳过 Cloning into 进度行）
		if isSSHURL(gitURL) {
			return &gitError{msg: "克隆失败（SSH 链接）：" + gitErrorSummary(msg) + "。请确认已配置正确的 SSH Key", err: err}
		}
		return &gitError{msg: "克隆失败：" + gitErrorSummary(msg), err: err}
	}
	return nil
}

// gitErrorSummary 从 git 输出中提取真正错误行（fatal:/error:/认证失败等），
// 跳过 "Cloning into" 等进度行。
func gitErrorSummary(output string) string {
	lines := strings.Split(output, "\n")
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.Contains(t, "fatal:") || strings.Contains(t, "error:") ||
			strings.Contains(t, "denied") || strings.Contains(t, "Authentication") ||
			strings.Contains(t, "Could not") || strings.Contains(t, "unable to") ||
			strings.Contains(t, "找不到") {
			return t
		}
	}
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t != "" && !strings.HasPrefix(t, "Cloning into") {
			return t
		}
	}
	return strings.TrimSpace(output)
}

// isAuthError 判断克隆错误是否为认证/权限类（应引导用户重新配置凭证）。
func isAuthError(msg string) bool {
	lower := strings.ToLower(msg)
	for _, kw := range []string{"access denied", "authentication", "permission denied", "401", "403", "not found: repository", "could not read username"} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// isSSHURL 判断 git 链接是否为 SSH 格式（git@host:path 或 ssh://）。
func isSSHURL(u string) bool {
	return strings.HasPrefix(u, "git@") || strings.HasPrefix(u, "ssh://")
}

// isHTTPSURL 判断 git 链接是否为 HTTPS 格式。
func isHTTPSURL(u string) bool {
	return strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://")
}

// isLocalURL 判断 git 链接是否为本地路径（无需凭证）。
func isLocalURL(u string) bool {
	if isSSHURL(u) || isHTTPSURL(u) || strings.HasPrefix(u, "file://") || strings.HasPrefix(u, "git://") {
		return false
	}
	return true
}

type gitError struct {
	msg string
	err error
}

func (e *gitError) Error() string { return e.msg }

// ensureProjectGitignore 确保项目根 .gitignore 忽略 Teamix 运行时目录
// （.teamix/ 会话/配置、.reasonix/ 本地配置），幂等追加，避免会话与本地配置被提交。
func ensureProjectGitignore(projPath string) {
	path := filepath.Join(projPath, ".gitignore")
	data, _ := os.ReadFile(path)
	content := string(data)
	need := []string{".teamix/", ".reasonix/"}
	added := []string{}
	for _, line := range need {
		if !strings.Contains(content, line) {
			added = append(added, line)
		}
	}
	if len(added) == 0 {
		return
	}
	var sb strings.Builder
	if content != "" && !strings.HasSuffix(content, "\n") {
		sb.WriteString("\n")
	}
	sb.WriteString("# Teamix workspace data (auto-generated)\n")
	for _, line := range added {
		sb.WriteString(line + "\n")
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		slog.Warn("teamix: failed to write project .gitignore", "path", path, "err", err)
	}
}
