package teamixconfig

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// UserConfig 对应用户私有配置 users/<name>/.teamix/config.yaml
type UserConfig struct {
	Git         GitConfig    `yaml:"git"`
	MCP         []PluginRef  `yaml:"mcp,omitempty"`    // 兼容遗留：新代码写 .reasonix/mcp-private.json
	Skills      []SkillRef   `yaml:"skills,omitempty"` // 兼容遗留：新代码写 .reasonix/skills/
	Preferences Preferences  `yaml:"preferences"`
}

type GitConfig struct {
	SSHKeyPath     string `yaml:"ssh_key_path"`
	HTTPSUsername  string `yaml:"https_username"`
	HTTPSPassword  string `yaml:"https_password"`
}

type PluginRef struct {
	Name    string   `yaml:"name"`
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
	Type    string   `yaml:"type,omitempty"`
}

type SkillRef struct {
	Name    string `yaml:"name"`
	Enabled bool   `yaml:"enabled"`
}

type Preferences struct {
	Language string `yaml:"language"`
}

// DefaultUserConfig returns a minimal user config template.
func DefaultUserConfig() *UserConfig {
	return &UserConfig{
		Git: GitConfig{},
		MCP: []PluginRef{},
		Skills: []SkillRef{},
		Preferences: Preferences{Language: "zh"},
	}
}

// LoadUserConfig reads users/<name>/.teamix/config.yaml
func LoadUserConfig(userRoot string) (*UserConfig, error) {
	path := filepath.Join(userRoot, ".teamix", "config.yaml")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultUserConfig(), nil
		}
		return nil, fmt.Errorf("open user config: %w", err)
	}
	defer f.Close()

	var uc UserConfig
	if err := yaml.NewDecoder(f).Decode(&uc); err != nil {
		return nil, fmt.Errorf("parse user config: %w", err)
	}
	return &uc, nil
}

// SaveUserConfig writes users/<name>/.teamix/config.yaml
func (uc *UserConfig) SaveUserConfig(userRoot string) error {
	path := filepath.Join(userRoot, ".teamix", "config.yaml")
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create user config: %w", err)
	}
	defer f.Close()

	enc := yaml.NewEncoder(f)
	enc.SetIndent(2)
	if err := enc.Encode(uc); err != nil {
		return fmt.Errorf("encode user config: %w", err)
	}
	return nil
}

// HasGitCredentials returns true if any git credential is configured.
func (uc *UserConfig) HasGitCredentials() bool {
	return uc.Git.SSHKeyPath != "" || uc.Git.HTTPSUsername != ""
}

// ValidateGitCredentials checks if the configured credentials are valid.
func (uc *UserConfig) ValidateGitCredentials() error {
	if uc.Git.SSHKeyPath != "" {
		if _, err := os.Stat(uc.Git.SSHKeyPath); err != nil {
			return fmt.Errorf("SSH key not found at %s（注意：SSH key 路径是 Teamix 服务器上的路径，不是浏览器所在电脑的路径）: %w", uc.Git.SSHKeyPath, err)
		}
		return nil
	}
	if uc.Git.HTTPSUsername != "" {
		if uc.Git.HTTPSPassword == "" {
			return fmt.Errorf("HTTPS password is required when username is set")
		}
		return nil
	}
	if uc.Git.HTTPSPassword != "" {
		return fmt.Errorf("已填写令牌但缺少用户名：HTTPS 凭证需要「用户名 + 令牌（密码栏）」两个字段。用户名可填 oauth2 或任意非空值")
	}
	return fmt.Errorf("no git credentials configured (set ssh_key_path or https_username)")
}
