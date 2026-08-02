package teamixconfig

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type UsersConfig struct {
	Users []UserEntry `yaml:"users"`
}

type UserEntry struct {
	Name string `yaml:"name"`
	Role string `yaml:"role"`
}

func (u UserEntry) RoleOr() string {
	switch u.Role {
	case "architect":
		return "architect"
	default:
		return "developer"
	}
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
