package serve

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"reasonix/internal/boot"
	"reasonix/internal/teamixconfig"
	reasonixAgent "reasonix/internal/agent"
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
		// 密码留空 = 保持已有密码（前端回填用户名后点保存不应把已存密码清空，
		// 否则每次登录都要重新填写）。只有 SSH 切换或显式填密码才改动。
		if body.HTTPSPassword != "" {
			uc.Git.HTTPSPassword = body.HTTPSPassword
		}
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

	// 目录不存在，或存在但不是有效 git 仓库（残留空壳）→ 清理后克隆
	if _, err := os.Stat(filepath.Join(projPath, ".git")); os.IsNotExist(err) {
		if _, statErr := os.Stat(projPath); statErr == nil {
			// 残留目录（无 .git）：清掉再克隆
			if rmErr := os.RemoveAll(projPath); rmErr != nil {
				writeJSON(w, map[string]any{"ok": false, "error": "清理残留项目目录失败: " + rmErr.Error()})
				return
			}
		}
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
		if err := ts.cloneProject(proj.Git, projPath, uc, u.name+"/"+body.Project); err != nil {
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

	// 项目内 Teamix 配置不入 git：.teamix/ 与 .reasonix/ 全部忽略（幂等追加）。
	ensureProjectGitignore(projPath)

	// 记忆/工作区按项目隔离：切换项目 = 重建 controller（复用 switchModel
	// 的 Snapshot+AdoptHistory 模式）。MemoryUserDir 带项目段 → agent 的
	// memory store（私有+全局 recall）全部按项目隔离；会话目录切到项目；
	// 历史消息迁移保留，继续对话不中断。
	if u.ctrl.Running() {
		writeJSON(w, map[string]any{"ok": false, "error": "当前有轮次在运行，请等待完成后再切换项目"})
		return
	}
	cur := u.ctrl
	if err := cur.Snapshot(); err != nil {
		slog.Warn("teamix: snapshot before project switch", "err", err)
	}
	prevPath := cur.SessionPath()
	carried := cur.History()

	bc := NewBroadcaster()
	projSessionDir := filepath.Join(projPath, ".teamix", "sessions")
	if err := os.MkdirAll(projSessionDir, 0o755); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": "create session dir: " + err.Error()})
		return
	}
	newCtrl, err := boot.Build(context.Background(), boot.Options{
		Model:               u.model,
		RequireKey:          true,
		Sink:                bc,
		Stderr:              os.Stderr,
		TokenMode:           ts.profile,
		WorkspaceRoot:       u.userRoot,
		SessionDir:          projSessionDir,
		SharedHost:          ts.sharedHost,
		ExcludedPluginNames: ts.excludedMachineMCPNames(),
		MemoryUserDir:       filepath.Join(u.userRoot, ".teamix", body.Project), // 记忆按项目隔离
		ExcludeHomeSkills:   true,
		WrapProvider:        ts.headroomHook,
		Router:              ts.routerCfg(u.name),
		RagIndex:            ts.ragIndexFor(u.name),
	})
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": "切换项目失败（重建控制器）: " + err.Error()})
		return
	}
	newPath := reasonixAgent.ContinueSessionPath(prevPath, newCtrl.SessionDir(), newCtrl.Label())
	newCtrl.AdoptHistory(carried, newPath)
	u.ctrl = newCtrl
	u.bc = bc
	u.selectedProject = body.Project
	cur.Close()

	writeJSON(w, map[string]any{
		"ok":      true,
		"project": body.Project,
		"path":    projPath,
		"cloned":  cloned,
	})
}

var (
	// 接收对象进度（本地拉取）
	receivingRe = regexp.MustCompile(`Receiving objects:\s+(\d+)% \((\d+)/(\d+)\)`)
	// 文件检出进度（checkout 阶段，如 50/12350）
	updatingRe = regexp.MustCompile(`Updating files:\s+(\d+)% \((\d+)/(\d+)\)`)
)

// cloneProject 完整克隆（保留全部提交历史，工作流关联性分析依赖 git log/blame）。
// progKey 非空时记录传输进度供前端轮询（个人选择项目）；公共区构建克隆传 "" 不记录。
func (ts *TeamixServer) cloneProject(gitURL, targetPath string, uc *teamixconfig.UserConfig, progKey string) error {
	cmd := exec.Command("git", "-c", "credential.helper=", "-c", "core.longpaths=true", "clone", "--progress", gitURL, targetPath)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	// 按链接类型匹配凭证：SSH 链接用 SSH Key，HTTPS 链接用账号密码。
	switch {
	case isSSHURL(gitURL) && uc.Git.SSHKeyPath != "":
		cmd.Env = append(os.Environ(),
			fmt.Sprintf("GIT_SSH_COMMAND=ssh -i %q -o StrictHostKeyChecking=accept-new", uc.Git.SSHKeyPath),
		)
	case isHTTPSURL(gitURL) && uc.Git.HTTPSUsername != "":
		if u, err := url.Parse(gitURL); err == nil && u.Scheme == "https" {
			u.User = url.UserPassword(uc.Git.HTTPSUsername, uc.Git.HTTPSPassword)
			cmd = exec.Command("git", "-c", "credential.helper=", "-c", "core.longpaths=true", "clone", "--progress", u.String(), targetPath)
			cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		}
	}

	var stderrBuf bytes.Buffer
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return &gitError{msg: "无法读取克隆输出", err: err}
	}
	if err := cmd.Start(); err != nil {
		return &gitError{msg: "启动克隆失败: " + err.Error(), err: err}
	}
	// 解析 stderr 的进度帧更新进度：先 "Receiving objects: xx% (a/b)"，
	// 检出阶段 "Updating files: xx% (a/b)"。进度帧以 \r 结尾不换行（CR≈帧数、LF 极少），
	// 必须按 \r 逐帧读取（实测完整 clone 171+85 帧）。
	go func() {
		br := bufio.NewReader(io.TeeReader(stderrPipe, &stderrBuf))
		frames := 0
		for {
			line, err := br.ReadString('\r')
			if progKey != "" {
				if m := receivingRe.FindStringSubmatch(line); m != nil {
					frames++
					ts.setCloneProgress(progKey, "接收 "+m[1]+"% ("+m[2]+"/"+m[3]+")")
				} else if m := updatingRe.FindStringSubmatch(line); m != nil {
					frames++
					ts.setCloneProgress(progKey, "文件 "+m[1]+"% ("+m[2]+"/"+m[3]+")")
				}
			}
			if err != nil {
				if progKey != "" {
					slog.Info("clone progress done", "progKey", progKey, "frames", frames)
				}
				break
			}
		}
	}()
	werr := cmd.Wait()
	if progKey != "" {
		ts.clearCloneProgress(progKey)
	}
	if werr != nil {
		// 从 git stderr 收集错误（StderrPipe 已消费，用错误本身兜底）
		errOutput := strings.TrimSpace(stderrBuf.String())
		msg := "克隆失败：" + strings.TrimSpace(werr.Error())
		if errOutput != "" {
			if len(errOutput) > 500 {
				errOutput = "..." + errOutput[len(errOutput)-500:]
			}
			msg += "\\n" + errOutput
		}
		if isSSHURL(gitURL) {
			msg += "。请确认已配置正确的 SSH Key"
		}
		return &gitError{msg: msg, err: werr}
	}
	return nil
}

// setCloneProgress / getCloneProgress / clearCloneProgress：clone 进度存取。
func (ts *TeamixServer) setCloneProgress(key, progress string) {
	ts.cloneMu.Lock()
	defer ts.cloneMu.Unlock()
	if ts.cloneProg == nil {
		ts.cloneProg = make(map[string]string)
	}
	ts.cloneProg[key] = progress
}

func (ts *TeamixServer) getCloneProgress(key string) string {
	ts.cloneMu.Lock()
	defer ts.cloneMu.Unlock()
	return ts.cloneProg[key]
}

func (ts *TeamixServer) clearCloneProgress(key string) {
	ts.cloneMu.Lock()
	defer ts.cloneMu.Unlock()
	delete(ts.cloneProg, key)
}

func (ts *TeamixServer) snapshotCloneProgress() map[string]string {
	ts.cloneMu.Lock()
	defer ts.cloneMu.Unlock()
	out := make(map[string]string, len(ts.cloneProg))
	for k, v := range ts.cloneProg {
		out[k] = v
	}
	return out
}

// GET /teamix/clone/progress?project=xxx — 轮询 clone 进度（前端进度条用）。
func (ts *TeamixServer) handleCloneProgress(w http.ResponseWriter, r *http.Request, u *userSession) {
	project := r.URL.Query().Get("project")
	if project != "" {
		writeJSON(w, map[string]any{
			"running":  ts.getCloneProgress(u.name+"/"+project) != "",
			"project":  project,
			"progress": ts.getCloneProgress(u.name + "/" + project),
		})
		return
	}
	// 不带 project：返回当前用户第一个进行中的 clone（刷新页面后恢复用）
	for key, prog := range ts.snapshotCloneProgress() {
		if len(key) > len(u.name)+1 && key[:len(u.name)+1] == u.name+"/" {
			writeJSON(w, map[string]any{"running": true, "project": key[len(u.name)+1:], "progress": prog})
			return
		}
	}
	writeJSON(w, map[string]any{"running": false})
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
