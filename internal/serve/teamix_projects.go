package serve

import (
	"net/http"
)

func (ts *TeamixServer) handleProjects(w http.ResponseWriter, r *http.Request, u *userSession) {
	if ts.globalCfg == nil || ts.globalCfg.Projects == nil {
		writeJSON(w, []any{})
		return
	}
	type projJSON struct {
		Name         string `json:"name"`
		Git          string `json:"git"`
		Description  string `json:"description"`
		ServiceCount int    `json:"serviceCount"`
	}
	out := make([]projJSON, 0, len(ts.globalCfg.Projects.Projects))
	for _, p := range ts.globalCfg.Projects.Projects {
		out = append(out, projJSON{
			Name:         p.Name,
			Git:          p.Git,
			Description:  p.Description,
			ServiceCount: len(p.Services),
		})
	}
	writeJSON(w, out)
}

func (ts *TeamixServer) handleProjectServices(w http.ResponseWriter, r *http.Request, u *userSession) {
	projectName := r.PathValue("name")
	if ts.globalCfg == nil || ts.globalCfg.Projects == nil {
		http.Error(w, `{"error":"no projects configured"}`, http.StatusNotFound)
		return
	}
	p := ts.globalCfg.Projects.FindProject(projectName)
	if p == nil {
		http.Error(w, `{"error":"project not found"}`, http.StatusNotFound)
		return
	}
	type svcJSON struct {
		Name      string   `json:"name"`
		Type      string   `json:"type"`
		Dir       string   `json:"dir"`
		Startup   string   `json:"startup"`
		Port      int      `json:"port"`
		DependsOn []string `json:"dependsOn,omitempty"`
	}
	out := make([]svcJSON, 0, len(p.Services))
	for _, s := range p.Services {
		depends := s.DependsOn
		if depends == nil {
			depends = []string{}
		}
		out = append(out, svcJSON{
			Name:      s.Name,
			Type:      s.Type,
			Dir:       s.Dir,
			Startup:   s.Startup,
			Port:      s.Port,
			DependsOn: depends,
		})
	}
	writeJSON(w, map[string]any{
		"project":  projectName,
		"services": out,
	})
}

func (ts *TeamixServer) handleProjectLegacy(w http.ResponseWriter, r *http.Request) {
	if ts.globalCfg == nil || ts.globalCfg.Projects == nil {
		writeJSON(w, map[string]any{
			"workspaceRoot": ts.workspaceRoot,
			"projectName":   "unknown",
			"projects":      []any{},
		})
		return
	}
	type projBrief struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	projects := make([]projBrief, 0, len(ts.globalCfg.Projects.Projects))
	for _, p := range ts.globalCfg.Projects.Projects {
		projects = append(projects, projBrief{Name: p.Name, Description: p.Description})
	}
	writeJSON(w, map[string]any{
		"workspaceRoot": ts.workspaceRoot,
		"projectName":   ts.globalCfg.Config.Teamix.Name,
		"projects":      projects,
	})
}
