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
	Teamix   TeamixConfig   `yaml:"teamix"`
	Project  ProjectConfig  `yaml:"project,omitempty"`
	Modules  []ModuleConfig `yaml:"modules,omitempty"`
	Workflow WorkflowConfig `yaml:"workflow,omitempty"`
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
		Teamix:  TeamixConfig{},
		Project: ProjectConfig{},
		Modules: []ModuleConfig{},
		Workflow: WorkflowConfig{AutoAdvance: true},
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
	return &cfg, nil
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
