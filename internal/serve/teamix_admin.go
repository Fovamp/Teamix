package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/teamixconfig"
)

// 用户管理（仅架构师）：白名单的增删改查，保护最后一个架构师。
// 项目管理（仅架构师）：projects.yaml 的增删改，添加时校验 git 链接可访问。

func (ts *TeamixServer) usersConfig() *teamixconfig.UsersConfig {
	if ts.globalCfg != nil && ts.globalCfg.Users != nil {
		return ts.globalCfg.Users
	}
	return teamixconfig.DefaultUsersConfig()
}

func (ts *TeamixServer) projectsConfig() *teamixconfig.ProjectsConfig {
	if ts.globalCfg != nil && ts.globalCfg.Projects != nil {
		return ts.globalCfg.Projects
	}
	return teamixconfig.DefaultProjectsConfig()
}

// reloadConfigs 重新加载全局配置（users/projects 变更后调用）。
func (ts *TeamixServer) reloadConfigs() {
	if cfg, err := teamixconfig.LoadAll(ts.workspaceRoot); err == nil {
		ts.globalCfg = cfg
	}
}

// ── 用户管理 ──

// GET /teamix/users
func (ts *TeamixServer) handleUsersList(w http.ResponseWriter, r *http.Request, u *userSession) {
	if !ts.isArchitect(u) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	type userJSON struct {
		Name      string `json:"name"`
		Role      string `json:"role"`
		IsCurrent bool   `json:"isCurrent"`
	}
	users := ts.usersConfig().Users
	out := make([]userJSON, 0, len(users))
	for _, e := range users {
		out = append(out, userJSON{Name: e.Name, Role: e.RoleOr(), IsCurrent: e.Name == u.name})
	}
	writeJSON(w, map[string]any{"users": out})
}

// POST /teamix/users/add {name, role, httpsUsername?, httpsPassword?}
func (ts *TeamixServer) handleUserAdd(w http.ResponseWriter, r *http.Request, u *userSession) {
	if !ts.isArchitect(u) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var body struct {
		Name          string `json:"name"`
		Role          string `json:"role"`
		HTTPSUsername string `json:"httpsUsername"` // 可选：该用户的 git 凭证
		HTTPSPassword string `json:"httpsPassword"` // 密码或访问令牌
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !safeTokenName(body.Name) {
		http.Error(w, `{"error":"无效用户名（仅限字母数字 _ - .）"}`, http.StatusBadRequest)
		return
	}
	role := teamixconfig.NormalizeRole(body.Role)
	uc := ts.usersConfig()
	if uc.FindUser(body.Name) != nil {
		http.Error(w, `{"error":"用户已存在"}`, http.StatusConflict)
		return
	}
	uc.Users = append(uc.Users, teamixconfig.UserEntry{Name: body.Name, Role: role})
	if err := uc.SaveUsers(ts.workspaceRoot); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// 可选：写入该用户的 git HTTPS 凭证（账号 或 令牌），登录后即可直接克隆
	if body.HTTPSUsername != "" {
		// 用户可能尚未登录过：先初始化工作区（创建目录与默认配置）
		if _, err := ts.InitUserWorkspace(body.Name); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		userRoot := ts.UserRoot(body.Name)
		puc, err := teamixconfig.LoadUserConfig(userRoot)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		puc.Git.HTTPSUsername = body.HTTPSUsername
		puc.Git.HTTPSPassword = body.HTTPSPassword
		puc.Git.SSHKeyPath = ""
		if err := puc.SaveUserConfig(userRoot); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	ts.reloadConfigs()
	writeJSON(w, map[string]bool{"ok": true})
}

// POST /teamix/users/remove {name}
func (ts *TeamixServer) handleUserRemove(w http.ResponseWriter, r *http.Request, u *userSession) {
	if !ts.isArchitect(u) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	uc := ts.usersConfig()
	target := uc.FindUser(body.Name)
	if target == nil {
		http.Error(w, `{"error":"用户不存在"}`, http.StatusNotFound)
		return
	}
	// 保护最后一个架构师：删除架构师前必须还有别的架构师
	if target.IsArchitect() && len(uc.ArchitectNames()) <= 1 {
		http.Error(w, `{"error":"不能删除最后一个架构师"}`, http.StatusConflict)
		return
	}
	for i := range uc.Users {
		if uc.Users[i].Name == body.Name {
			uc.Users = append(uc.Users[:i], uc.Users[i+1:]...)
			break
		}
	}
	if err := uc.SaveUsers(ts.workspaceRoot); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ts.reloadConfigs()
	writeJSON(w, map[string]bool{"ok": true})
}

// POST /teamix/users/role {name, role}
func (ts *TeamixServer) handleUserRoleUpdate(w http.ResponseWriter, r *http.Request, u *userSession) {
	if !ts.isArchitect(u) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var body struct {
		Name string `json:"name"`
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	role := teamixconfig.NormalizeRole(body.Role)
	uc := ts.usersConfig()
	target := uc.FindUser(body.Name)
	if target == nil {
		http.Error(w, `{"error":"用户不存在"}`, http.StatusNotFound)
		return
	}
	// 保护最后一个架构师：不能把唯一的架构师降级
	if target.IsArchitect() && role != "architect" && len(uc.ArchitectNames()) <= 1 {
		http.Error(w, `{"error":"不能降级最后一个架构师"}`, http.StatusConflict)
		return
	}
	target.Role = role
	if err := uc.SaveUsers(ts.workspaceRoot); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ts.reloadConfigs()
	writeJSON(w, map[string]bool{"ok": true})
}

// POST /teamix/users/credentials {name, httpsUsername, httpsPassword} — 为已存在用户补/改 git 凭证
func (ts *TeamixServer) handleUserCredentials(w http.ResponseWriter, r *http.Request, u *userSession) {
	if !ts.isArchitect(u) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var body struct {
		Name          string `json:"name"`
		HTTPSUsername string `json:"httpsUsername"`
		HTTPSPassword string `json:"httpsPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if ts.usersConfig().FindUser(body.Name) == nil {
		http.Error(w, `{"error":"用户不存在"}`, http.StatusNotFound)
		return
	}
	// 用户可能尚未登录过：先初始化工作区
	if _, err := ts.InitUserWorkspace(body.Name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	userRoot := ts.UserRoot(body.Name)
	puc, err := teamixconfig.LoadUserConfig(userRoot)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	puc.Git.HTTPSUsername = body.HTTPSUsername
	puc.Git.HTTPSPassword = body.HTTPSPassword
	puc.Git.SSHKeyPath = ""
	if err := puc.SaveUserConfig(userRoot); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// ── 项目管理 ──

// validateGitRemote 用 git ls-remote 校验仓库链接可访问。
func validateGitRemote(url string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "ls-remote", url, "HEAD")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("git 仓库不可访问: %s", msg)
	}
	return nil
}

// POST /teamix/projects/add {name, git, description}
// 添加流程：校验 git 链接 → clone 到公共区 projects/<name>/ → 扫描模块 → 写 projects.yaml。
func (ts *TeamixServer) handleProjectAdd(w http.ResponseWriter, r *http.Request, u *userSession) {
	if !ts.isArchitect(u) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var body struct {
		Name        string `json:"name"`
		Git         string `json:"git"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" || body.Git == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !safeTokenName(body.Name) {
		http.Error(w, `{"error":"无效项目名（仅限字母数字 _ - .）"}`, http.StatusBadRequest)
		return
	}
	pc := ts.projectsConfig()
	if pc.FindProject(body.Name) != nil {
		http.Error(w, `{"error":"项目已存在"}`, http.StatusConflict)
		return
	}
	gitURL := strings.TrimSpace(body.Git)
	// 添加时校验 git 链接可访问
	if err := validateGitRemote(gitURL); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// 克隆到公共构建区 projects/<name>/（与 .teamix 同级，为资源池/构建预留）
	projDir := filepath.Join(ts.workspaceRoot, "projects", body.Name)
	uc, _ := teamixconfig.LoadUserConfig(u.userRoot)
	if err := ts.cloneProject(gitURL, projDir, uc); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": "克隆到公共区失败: " + err.Error()})
		return
	}
	// 自动分析器：扫描模块写入 services
	services := analyzeProject(projDir)

	pc.Projects = append(pc.Projects, teamixconfig.ProjectEntry{
		Name: body.Name, Git: gitURL, Description: body.Description, Services: services,
	})
	if err := pc.SaveProjects(ts.workspaceRoot); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ts.reloadConfigs()
	writeJSON(w, map[string]any{"ok": true, "services": len(services)})
}

// POST /teamix/projects/{name}/scan — 架构师手动重新扫描模块（先 git pull 更新公共区）。
func (ts *TeamixServer) handleProjectScan(w http.ResponseWriter, r *http.Request, u *userSession) {
	if !ts.isArchitect(u) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	project := r.PathValue("name")
	pc := ts.projectsConfig()
	target := pc.FindProject(project)
	if target == nil {
		http.Error(w, `{"error":"项目不存在"}`, http.StatusNotFound)
		return
	}
	projDir := filepath.Join(ts.workspaceRoot, "projects", project)
	// 公共区目录不存在（改造前添加的项目）→ 先 clone
	if _, err := os.Stat(projDir); os.IsNotExist(err) {
		uc, _ := teamixconfig.LoadUserConfig(u.userRoot)
		if err := ts.cloneProject(target.Git, projDir, uc); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": "克隆到公共区失败: " + err.Error()})
			return
		}
	}
	// 尝试 git pull 更新公共区代码（失败不阻断，用现有目录重新扫描）
	if _, err := os.Stat(filepath.Join(projDir, ".git")); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		cmd := exec.CommandContext(ctx, "git", "-C", projDir, "pull", "--ff-only")
		_ = cmd.Run()
		cancel()
	}
	services := analyzeProject(projDir)
	target.Services = services
	if err := pc.SaveProjects(ts.workspaceRoot); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ts.reloadConfigs()
	writeJSON(w, map[string]any{"ok": true, "services": services})
}

// POST /teamix/projects/remove {name}
func (ts *TeamixServer) handleProjectRemove(w http.ResponseWriter, r *http.Request, u *userSession) {
	if !ts.isArchitect(u) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	pc := ts.projectsConfig()
	if pc.FindProject(body.Name) == nil {
		http.Error(w, `{"error":"项目不存在"}`, http.StatusNotFound)
		return
	}
	for i := range pc.Projects {
		if pc.Projects[i].Name == body.Name {
			pc.Projects = append(pc.Projects[:i], pc.Projects[i+1:]...)
			break
		}
	}
	if err := pc.SaveProjects(ts.workspaceRoot); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// 删除公共区克隆目录 projects/<name>/（配置与代码一起移除）
	if err := os.RemoveAll(filepath.Join(ts.workspaceRoot, "projects", body.Name)); err != nil {
		slog.Warn("teamix: failed to remove project dir", "project", body.Name, "err", err)
	}
	ts.reloadConfigs()
	writeJSON(w, map[string]bool{"ok": true})
}
func (ts *TeamixServer) handleProjectUpdate(w http.ResponseWriter, r *http.Request, u *userSession) {
	if !ts.isArchitect(u) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var body struct {
		Name        string `json:"name"`
		Git         string `json:"git"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	pc := ts.projectsConfig()
	target := pc.FindProject(body.Name)
	if target == nil {
		http.Error(w, `{"error":"项目不存在"}`, http.StatusNotFound)
		return
	}
	if body.Git != "" && strings.TrimSpace(body.Git) != target.Git {
		newGit := strings.TrimSpace(body.Git)
		// 编辑时只做格式校验（网络可达性由拉取时验证——此时凭证可能尚未配置）
		if !isSSHURL(newGit) && !isHTTPSURL(newGit) && !isLocalURL(newGit) {
			writeJSON(w, map[string]any{"ok": false, "error": "git 链接格式无法识别（支持 SSH / HTTPS / 本地路径）"})
			return
		}
		target.Git = newGit
		// 同步公共区 remote：已 clone 则把 origin 切到新链接
		projDir := filepath.Join(ts.workspaceRoot, "projects", body.Name)
		if _, err := os.Stat(filepath.Join(projDir, ".git")); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			_ = exec.CommandContext(ctx, "git", "-C", projDir, "remote", "set-url", "origin", newGit).Run()
			cancel()
		}
	}
	if body.Description != "" {
		target.Description = body.Description
	}
	if err := pc.SaveProjects(ts.workspaceRoot); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ts.reloadConfigs()
	writeJSON(w, map[string]bool{"ok": true})
}
