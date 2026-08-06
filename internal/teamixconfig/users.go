package teamixconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type UsersConfig struct {
	Users []UserEntry `yaml:"users"`
}

type UserEntry struct {
	Name string `yaml:"name"`
	Role string `yaml:"role"`
	// AllowExternal 是否允许该用户的外部模型调用（出网）。默认 true；
	// false = 该用户全部请求走内部 Qwen（管理员禁用外网，权限细粒度 P2）。
	AllowExternal *bool `yaml:"allow_external,omitempty"`
}

// CanUseExternal 报告该用户是否允许出网（未配置 = 允许）。
func (u UserEntry) CanUseExternal() bool {
	return u.AllowExternal == nil || *u.AllowExternal
}

func (u UserEntry) RoleOr() string {
	switch u.Role {
	case "architect":
		return "architect"
	default:
		return "developer"
	}
}

// NormalizeRole 把任意角色字符串规范化为 architect|developer。
func NormalizeRole(role string) string {
	if strings.ToLower(strings.TrimSpace(role)) == "architect" {
		return "architect"
	}
	return "developer"
}

func (u UserEntry) IsArchitect() bool { return u.RoleOr() == "architect" }

func (uc *UsersConfig) FindUser(name string) *UserEntry {
	for i := range uc.Users {
		if uc.Users[i].Name == name {
			return &uc.Users[i]
		}
	}
	return nil
}

func (uc *UsersConfig) ArchitectNames() []string {
	var out []string
	for _, u := range uc.Users {
		if u.IsArchitect() {
			out = append(out, u.Name)
		}
	}
	return out
}

func DefaultUsersConfig() *UsersConfig {
	return &UsersConfig{}
}

func LoadUsers(globalRoot string) (*UsersConfig, error) {
	path := filepath.Join(globalRoot, ".teamix", "users.yaml")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultUsersConfig(), nil
		}
		return nil, fmt.Errorf("open users.yaml: %w", err)
	}
	defer f.Close()

	var uc UsersConfig
	if err := yaml.NewDecoder(f).Decode(&uc); err != nil {
		return nil, fmt.Errorf("parse users.yaml: %w", err)
	}
	return &uc, nil
}

// SaveUsers 把白名单写回 .teamix/users.yaml。
func (uc *UsersConfig) SaveUsers(globalRoot string) error {
	path := filepath.Join(globalRoot, ".teamix", "users.yaml")
	data, err := yaml.Marshal(uc)
	if err != nil {
		return fmt.Errorf("marshal users.yaml: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write users.yaml: %w", err)
	}
	return nil
}
