package teamixconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type TeamixConfig struct {
	Name         string `yaml:"name"`
	DefaultModel string `yaml:"default_model"`
	ProjectsFile string `yaml:"projects_file,omitempty"`
	UsersFile    string `yaml:"users_file,omitempty"`
	MCPFile      string `yaml:"mcp_file,omitempty"`
}

type Config struct {
	Teamix    TeamixConfig    `yaml:"teamix"`
	Project   ProjectConfig   `yaml:"project,omitempty"`
	Modules   []ModuleConfig  `yaml:"modules,omitempty"`
	Workflow  WorkflowConfig  `yaml:"workflow,omitempty"`
	Headroom  HeadroomConfig  `yaml:"headroom,omitempty"`
	Models    ModelsConfig    `yaml:"models,omitempty"`
	Sensitive SensitiveConfig `yaml:"sensitive,omitempty"`
	Audit     AuditConfig     `yaml:"audit,omitempty"`
	Quota     QuotaConfig     `yaml:"quota,omitempty"`
	Alert     AlertConfig     `yaml:"alert,omitempty"`
	Nacos     NacosConfig     `yaml:"nacos,omitempty"`
}

// NacosConfig 是 nacos 配置中心连接模板（.teamix/config.yaml 的 nacos 段；
// 用户级 users/<name>/.teamix/config.yaml 同名段优先，回落团队级）。
// 个人启动时以环境变量注入（Spring relaxed binding 优先级最高，覆盖项目里
// 写乱的 nacos 配置，不改项目文件）：
//   config（拉配置）:  namespace=Teamix + group=ConfigGroup（默认 Global，共享一份）
//   discovery（注册）: namespace=Teamix + group=<用户名>（隔离）
// 仅当项目模块目录存在 spring.cloud.nacos 配置时才注入（无 nacos 项目保持原生）。
type NacosConfig struct {
	ServerAddr  string `yaml:"server_addr,omitempty"`  // nacos 服务地址（如 192.168.29.42:30107）
	Namespace   string `yaml:"namespace,omitempty"`    // 统一命名空间（默认 Teamix）
	ConfigGroup string `yaml:"config_group,omitempty"` // config 拉配置组（默认 Global）
	Username    string `yaml:"username,omitempty"`
	Password    string `yaml:"password,omitempty"`
}

// AlertConfig 致命告警渠道（P3）：企微机器人 webhook URL。空 = 仅日志/审计。
type AlertConfig struct {
	WebhookURL string `yaml:"webhook_url,omitempty"` // 企业微信群机器人 webhook
}

// QuotaConfig 三层配额（出网请求次数）：个人日额 + 全局月额（architect 豁免）。
// 超限柔性降级：外部请求自动切内部池（不报错，审计记 quota_exceeded）。
type QuotaConfig struct {
	PerUserPerDay  int `yaml:"per_user_per_day,omitempty"` // <=0 不限
	GlobalPerMonth int `yaml:"global_per_month,omitempty"` // <=0 不限
}

// ModelsConfig 是双模型协作的模型池配置（.teamix/config.yaml 的 models 段）。
// 内部池（本地 Qwen，数据不出内网）与外部池（云端 DeepSeek）按用途角色分工。
type ModelsConfig struct {
	Internal []ModelPool `yaml:"internal,omitempty"`
	External []ModelPool `yaml:"external,omitempty"`
}

// ModelPool 声明一个模型池条目：ref 引用现有 providers 体系（provider/model），
// roles 列出该池服务的用途角色，max_input_tokens 用于窗口预检，max_parallel 限并发。
type ModelPool struct {
	Ref                 string   `yaml:"ref"`
	Roles               []string `yaml:"roles,omitempty"`
	MaxInputTokens      int      `yaml:"max_input_tokens,omitempty"`
	MaxParallel         int      `yaml:"max_parallel,omitempty"`
	BudgetPerUserPerDay float64  `yaml:"budget_per_user_per_day,omitempty"`
}

// SensitiveConfig 是机密黑名单（.teamix/config.yaml 的 sensitive 段）。
// 命中 dirs/files 的内容强制走内部模型（fail-closed），配置层无外部路径。
// Tools 是内置工具的敏感级声明（工具名 → public/internal/redact）：显式声明
// 优先于默认兜底（如 doc_kb_search 默认 internal）。未声明工具走各自默认。
type SensitiveConfig struct {
	Dirs  []string          `yaml:"dirs,omitempty"`
	Files []string          `yaml:"files,omitempty"`
	Tools map[string]string `yaml:"tools,omitempty"`
}

// AuditConfig 是 AI 调用审计日志配置（.teamix/config.yaml 的 audit 段）。
// 日志按 用户×天 落盘，仅 architect 角色可见（泄露审计）。
type AuditConfig struct {
	Dir           string   `yaml:"dir,omitempty"`
	RetentionDays int      `yaml:"retention_days,omitempty"`
	RolesVisible  []string `yaml:"roles_visible,omitempty"`
}

// HeadroomConfig 是 headroom 上下文压缩层配置（.teamix/config.yaml 的 headroom 段）。
// 压缩只作用于发给 LLM 的 tool 结果消息，system/用户输入保持原样（前缀缓存稳定）。
type HeadroomConfig struct {
	Enabled   bool   `yaml:"enabled"`
	URL       string `yaml:"url"`
	Model     string `yaml:"model,omitempty"`
	MinChars  int    `yaml:"min_content_chars,omitempty"`
	TimeoutMs int    `yaml:"timeout_ms,omitempty"`
}

type ProjectConfig struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

type ModuleConfig struct {
	Name        string `yaml:"name"`
	Path        string `yaml:"path"`
	Description string `yaml:"description"`
}

type WorkflowConfig struct {
	AutoAdvance      bool     `yaml:"auto_advance"`
	ApprovalRequired []string `yaml:"approval_required"`
	Architects       []string `yaml:"architects"`
}

func DefaultConfig() *Config {
	return &Config{
		Teamix:   TeamixConfig{},
		Project:  ProjectConfig{},
		Modules:  []ModuleConfig{},
		Workflow: WorkflowConfig{AutoAdvance: true},
		Audit: AuditConfig{
			Dir:           ".teamix/logs/ai-audit",
			RetentionDays: 30,
			RolesVisible:  []string{"architect"},
		},
	}
}

func Load(workspaceRoot string) (*Config, error) {
	path := filepath.Join(workspaceRoot, ".teamix", "config.yaml")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := DefaultConfig()
			cfg.Project.Name = filepath.Base(workspaceRoot)
			cfg.discoverModules(workspaceRoot)
			return cfg, nil
		}
		return nil, fmt.Errorf("open .teamix/config.yaml: %w", err)
	}
	defer f.Close()

	var cfg Config
	if err := yaml.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse .teamix/config.yaml: %w", err)
	}
	for i := range cfg.Modules {
		if cfg.Modules[i].Name == "" {
			cfg.Modules[i].Name = filepath.Base(cfg.Modules[i].Path)
		}
	}
	if len(cfg.Modules) == 0 {
		cfg.discoverModules(workspaceRoot)
	}
	// 独立敏感清单（前端可视化编辑）优先覆盖 config.yaml 的 sensitive 段。
	if overlay, err := LoadSensitiveOverlay(workspaceRoot); err == nil && overlay != nil {
		cfg.Sensitive = *overlay
	}
	return &cfg, nil
}

// sensitiveOverlayFile 独立机密清单文件名（前端「安全」设置页写入，避免手改 config.yaml）。
const sensitiveOverlayFile = ".teamix/sensitive.yaml"

// SensitiveOverlayPath 返回独立敏感清单文件路径。
func SensitiveOverlayPath(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, sensitiveOverlayFile)
}

// LoadSensitiveOverlay 读取独立敏感清单（前端写入）；文件不存在返回 nil。
func LoadSensitiveOverlay(workspaceRoot string) (*SensitiveConfig, error) {
	b, err := os.ReadFile(SensitiveOverlayPath(workspaceRoot))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var sc SensitiveConfig
	if err := yaml.Unmarshal(b, &sc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", sensitiveOverlayFile, err)
	}
	return &sc, nil
}

// SaveSensitiveOverlay 写独立敏感清单（前端「安全」设置页保存，architect 可写）。
func SaveSensitiveOverlay(workspaceRoot string, sc SensitiveConfig) error {
	data, err := yaml.Marshal(sc)
	if err != nil {
		return err
	}
	path := SensitiveOverlayPath(workspaceRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func resolveFile(globalRoot, fileRef, defaultName string) string {
	if fileRef != "" {
		return filepath.Join(globalRoot, ".teamix", fileRef)
	}
	return filepath.Join(globalRoot, ".teamix", defaultName)
}

func (c *Config) ProjectsFilePath(globalRoot string) string {
	return resolveFile(globalRoot, c.Teamix.ProjectsFile, "projects.yaml")
}

func (c *Config) UsersFilePath(globalRoot string) string {
	return resolveFile(globalRoot, c.Teamix.UsersFile, "users.yaml")
}

func (cfg *Config) discoverModules(workspaceRoot string) {
	skipDirs := map[string]bool{
		".git": true, ".teamix": true, ".reasonix": true,
		"node_modules": true, "vendor": true, ".venv": true,
		"__pycache__": true, "bin": true, "obj": true,
	}
	codeExts := map[string]bool{
		".go": true, ".py": true, ".js": true, ".ts": true,
		".tsx": true, ".jsx": true, ".rs": true, ".java": true,
		".c": true, ".h": true, ".cpp": true, ".hpp": true,
		".css": true, ".html": true, ".vue": true, ".svelte": true,
	}

	entries, err := os.ReadDir(workspaceRoot)
	if err != nil {
		return
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if skipDirs[name] || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		hasCode := false
		filepath.WalkDir(filepath.Join(workspaceRoot, name), func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if codeExts[strings.ToLower(filepath.Ext(p))] {
				hasCode = true
				return filepath.SkipAll
			}
			return nil
		})
		if !hasCode {
			continue
		}
		cfg.Modules = append(cfg.Modules, ModuleConfig{Name: name, Path: name + "/"})
	}
	sort.Slice(cfg.Modules, func(i, j int) bool { return cfg.Modules[i].Name < cfg.Modules[j].Name })
}
