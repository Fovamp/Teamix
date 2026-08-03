package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
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

// POST /teamix/users/add {name, role}
func (ts *TeamixServer) handleUserAdd(w http.ResponseWriter, r *http.Request, u *userSession) {
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
	// 添加时校验 git 链接可访问
	if err := validateGitRemote(strings.TrimSpace(body.Git)); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	pc.Projects = append(pc.Projects, teamixconfig.ProjectEntry{
		Name: body.Name, Git: strings.TrimSpace(body.Git), Description: body.Description,
	})
	if err := pc.SaveProjects(ts.workspaceRoot); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ts.reloadConfigs()
	writeJSON(w, map[string]bool{"ok": true})
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
	ts.reloadConfigs()
	writeJSON(w, map[string]bool{"ok": true})
}

// POST /teamix/projects/update {name, git?, description?}
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
		if err := validateGitRemote(strings.TrimSpace(body.Git)); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		target.Git = strings.TrimSpace(body.Git)
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
