package teamixconfig

import (
	"log/slog"
)

type GlobalConfig struct {
	Config   *Config
	Users    *UsersConfig
	Projects *ProjectsConfig
}

func LoadAll(globalRoot string) (*GlobalConfig, error) {
	cfg, err := Load(globalRoot)
	if err != nil {
		cfg = DefaultConfig()
	}
	users, err := LoadUsers(globalRoot)
	if err != nil {
		users = DefaultUsersConfig()
	}
	// 首次启动：用户列表为空时自动创建 admin 架构师账号
	if len(users.Users) == 0 {
		admin := UserEntry{Name: "admin", Role: "architect"}
		users.Users = append(users.Users, admin)
		if err := users.SaveUsers(globalRoot); err != nil {
			slog.Warn("teamix: failed to save bootstrap admin", "err", err)
		} else {
			slog.Info("teamix: auto-created default admin user (users.yaml was empty)")
		}
	}
	projects, err := LoadProjects(globalRoot)
	if err != nil {
		projects = DefaultProjectsConfig()
	}
	return &GlobalConfig{
		Config:   cfg,
		Users:    users,
		Projects: projects,
	}, nil
}

func (g *GlobalConfig) ArchitectNames() []string {
	if names := g.Users.ArchitectNames(); len(names) > 0 {
		return names
	}
	return g.Config.Workflow.Architects
}

func (g *GlobalConfig) IsArchitect(name string) bool {
	if u := g.Users.FindUser(name); u != nil {
		return u.IsArchitect()
	}
	for _, a := range g.Config.Workflow.Architects {
		if a == name {
			return true
		}
	}
	return false
}

// UserExists 严格白名单：未配置任何用户（users.yaml 为空）时拒绝所有登录，
// 必须由架构师先在 users.yaml 配置白名单后才能登录。
func (g *GlobalConfig) UserExists(name string) bool {
	if len(g.Users.Users) == 0 {
		return false
	}
	return g.Users.FindUser(name) != nil
}
