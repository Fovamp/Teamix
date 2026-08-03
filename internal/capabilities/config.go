// Package capabilities manages per-project capability overrides for
// MCP servers, AI personality (Soul), Skills, and model Gateway routing.
// Each capability is stored as a YAML file under .reasonix/capabilities/
// (Reasonix-side Agent infrastructure config, per the layout doc).
// A zero-value (file missing or empty) means "use global default".
package capabilities

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// MCPConfig defines project-level MCP server overrides.
type MCPConfig struct {
	Servers []MCPServer `yaml:"servers" json:"servers"`
}

type MCPServer struct {
	Name      string            `yaml:"name"      json:"name"`
	Transport string            `yaml:"transport"  json:"transport"`
	Command   string            `yaml:"command"   json:"command"`
	Args      []string          `yaml:"args"      json:"args"`
	Env       map[string]string `yaml:"env"       json:"env"`
	Enabled   bool              `yaml:"enabled"   json:"enabled"`
}

// SoulConfig defines project-level AI personality overrides.
type SoulConfig struct {
	SystemPrompt string  `yaml:"system_prompt" json:"systemPrompt"`
	Personality  string  `yaml:"personality"   json:"personality"`
	Temperature  float64 `yaml:"temperature"   json:"temperature"`
	MaxTokens    int     `yaml:"max_tokens"    json:"maxTokens"`
}

// SkillsConfig defines project-level skill management.
type SkillsConfig struct {
	CustomSkills []CustomSkill `yaml:"custom_skills" json:"customSkills"`
	Disabled     []string      `yaml:"disabled"      json:"disabled"`
}

type CustomSkill struct {
	Name    string `yaml:"name"    json:"name"`
	Source  string `yaml:"source"  json:"source"`
	Enabled bool   `yaml:"enabled" json:"enabled"`
}

// GatewayConfig defines project-level model routing.
type GatewayConfig struct {
	Models          []ModelRoute     `yaml:"models"           json:"models"`
	DeveloperModel  string           `yaml:"developer_model"  json:"developerModel"`
	ArchitectModel  string           `yaml:"architect_model"  json:"architectModel"`
}

type ModelRoute struct {
	Task     string `yaml:"task"     json:"task"`
	Provider string `yaml:"provider" json:"provider"`
	Model    string `yaml:"model"    json:"model"`
}

// AllConfigs groups all capability configurations.
type AllConfigs struct {
	MCP     *MCPConfig     `json:"mcp"`
	Soul    *SoulConfig    `json:"soul"`
	Skills  *SkillsConfig  `json:"skills"`
	Gateway *GatewayConfig `json:"gateway"`
}

// ── Load ──

func LoadAll(root string) *AllConfigs {
	return &AllConfigs{
		MCP:     loadSingle[MCPConfig](root, "mcp.yaml"),
		Soul:    loadSingle[SoulConfig](root, "soul.yaml"),
		Skills:  loadSingle[SkillsConfig](root, "skills.yaml"),
		Gateway: loadSingle[GatewayConfig](root, "gateway.yaml"),
	}
}

func loadSingle[T any](root, name string) *T {
	path := filepath.Join(root, ".reasonix", "capabilities", name)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return nil
	}
	var v T
	if err := yaml.Unmarshal(data, &v); err != nil {
		return nil
	}
	return &v
}

// ── Save ──

func SaveMCP(root string, cfg *MCPConfig) error {
	return saveSingle(root, "mcp.yaml", cfg)
}

func SaveSoul(root string, cfg *SoulConfig) error {
	return saveSingle(root, "soul.yaml", cfg)
}

func SaveSkills(root string, cfg *SkillsConfig) error {
	return saveSingle(root, "skills.yaml", cfg)
}

func SaveGateway(root string, cfg *GatewayConfig) error {
	return saveSingle(root, "gateway.yaml", cfg)
}

func saveSingle(root, name string, v any) error {
	dir := filepath.Join(root, ".reasonix", "capabilities")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), data, 0644)
}
