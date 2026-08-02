package teamixconfig

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type ProjectsConfig struct {
	Projects []ProjectEntry `yaml:"projects"`
}

type ProjectEntry struct {
	Name        string         `yaml:"name"`
	Git         string         `yaml:"git"`
	Description string         `yaml:"description"`
	Services    []ServiceEntry `yaml:"services"`
}

type ServiceEntry struct {
	Name      string   `yaml:"name"`
	Type      string   `yaml:"type"`
	Dir       string   `yaml:"dir"`
	Startup   string   `yaml:"startup"`
	Port      int      `yaml:"port"`
	DependsOn []string `yaml:"depends_on,omitempty"`
}

func (pe ProjectEntry) FindService(name string) *ServiceEntry {
	for i := range pe.Services {
		if pe.Services[i].Name == name {
			return &pe.Services[i]
		}
	}
	return nil
}

func (pc *ProjectsConfig) FindProject(name string) *ProjectEntry {
	for i := range pc.Projects {
		if pc.Projects[i].Name == name {
			return &pc.Projects[i]
		}
	}
	return nil
}

func DefaultProjectsConfig() *ProjectsConfig {
	return &ProjectsConfig{}
}

func LoadProjects(globalRoot string) (*ProjectsConfig, error) {
	path := filepath.Join(globalRoot, ".teamix", "projects.yaml")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultProjectsConfig(), nil
		}
		return nil, fmt.Errorf("open projects.yaml: %w", err)
	}
	defer f.Close()

	var pc ProjectsConfig
	if err := yaml.NewDecoder(f).Decode(&pc); err != nil {
		return nil, fmt.Errorf("parse projects.yaml: %w", err)
	}
	return &pc, nil
}
