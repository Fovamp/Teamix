package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Template defines a workflow template loaded from YAML.
type Template struct {
	Name        string         `yaml:"name" json:"name"`
	Label       string         `yaml:"label" json:"label"`
	Description string         `yaml:"description" json:"description"`
	Stages      []TemplateStage `yaml:"stages" json:"stages"`
}

// TemplateStage defines one stage in a template.
type TemplateStage struct {
	Name   string `yaml:"name" json:"name"`
	Label  string `yaml:"label" json:"label"`
	Prompt string `yaml:"prompt" json:"prompt"`
}

// LoadTemplates reads all YAML files from the workflows directory.
func LoadTemplates(dir string) ([]*Template, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read workflows dir: %w", err)
	}
	var out []*Template
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var t Template
		if err := yaml.Unmarshal(data, &t); err != nil {
			continue
		}
		out = append(out, &t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
