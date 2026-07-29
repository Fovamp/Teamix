package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"gopkg.in/yaml.v3"
	"reasonix/internal/workflow"
)

// Workflow stages and template handlers.

func (ts *TeamixServer) handleWorkflowGet(w http.ResponseWriter, r *http.Request, u *userSession) {
	type stageJSON struct {
		Stage  string `json:"stage"`
		Label  string `json:"label"`
		Status string `json:"status"`
	}
	statuses := u.workflow.Snapshot()
	orderedStages := u.workflow.OrderedStages()
	out := make([]stageJSON, 0, len(orderedStages))
	for _, st := range orderedStages {
		label := string(st)
		if l, ok := u.workflow.GetStageLabel(st); ok {
			label = l
		} else if l, ok := workflow.StageLabels[st]; ok {
			label = l
		}
		status := string(statuses[st])
		if status == "" {
			status = "pending"
		}
		out = append(out, stageJSON{Stage: string(st), Label: label, Status: string(status)})
	}
	current := string(u.workflow.CurrentStage())
	writeJSON(w, map[string]any{
		"stages":  out,
		"current": current,
	})
}


func (ts *TeamixServer) handleWorkflowAdvance(w http.ResponseWriter, r *http.Request, u *userSession) {
	ok := u.workflow.Advance()
	if !ok {
		writeJSON(w, map[string]any{"ok": false})
		return
	}
	// Auto-run impact analysis when entering the analysis stage
	if u.workflow.CurrentStage() == "analysis" {
		result := ts.runImpactAnalysis(u.name)
		u.workflow.SetImpactAnalysis(result)
	}
	// Return updated stages so the frontend can update the progress bar immediately
	type stageJSON struct {
		Stage  string `json:"stage"`
		Label  string `json:"label"`
		Status string `json:"status"`
	}
	statuses := u.workflow.Snapshot()
	orderedStages := u.workflow.OrderedStages()
	out := make([]stageJSON, 0, len(orderedStages))
	for _, st := range orderedStages {
		label := string(st)
		if l, ok := u.workflow.GetStageLabel(st); ok {
			label = l
		} else if l, ok := workflow.StageLabels[st]; ok {
			label = l
		}
		status := string(statuses[st])
		if status == "" {
			status = "pending"
		}
		out = append(out, stageJSON{Stage: string(st), Label: label, Status: string(status)})
	}
	writeJSON(w, map[string]any{"ok": true, "stages": out, "current": string(u.workflow.CurrentStage())})
}


func (ts *TeamixServer) handleWorkflowRollback(w http.ResponseWriter, r *http.Request, u *userSession) {
	ok := u.workflow.Rollback()
	writeJSON(w, map[string]any{"ok": ok})
}


func (ts *TeamixServer) handleWorkflowSetStage(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Stage string `json:"stage"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Stage == "" {
		http.Error(w, "stage is required", http.StatusBadRequest)
		return
	}
	ok := u.workflow.SetStage(workflow.Stage(body.Stage))
	// Auto-run impact analysis when entering the analysis stage
	if ok && u.workflow.CurrentStage() == "analysis" {
		result := ts.runImpactAnalysis(u.name)
		u.workflow.SetImpactAnalysis(result)
	}
	writeJSON(w, map[string]any{"ok": ok})
}


func (ts *TeamixServer) handleTemplateGet(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "missing name", http.StatusBadRequest)
		return
	}
	tmplDir := filepath.Join(".", ".teamix", "workflows")
	path := filepath.Join(tmplDir, name+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, "template not found", http.StatusNotFound)
		return
	}
	var t workflow.Template
	if err := yaml.Unmarshal(data, &t); err != nil {
		http.Error(w, "invalid template", http.StatusInternalServerError)
		return
	}
	writeJSON(w, t)
}


func (ts *TeamixServer) handleTemplateSave(w http.ResponseWriter, r *http.Request, u *userSession) {
	// Only architects can save
	isArchitect := false
	architects := ts.getArchitects()
	for _, a := range architects {
		if a == u.name {
			isArchitect = true
			break
		}
	}
	if !isArchitect {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var body struct {
		Name   string              `json:"name"`
		Label  string              `json:"label"`
		Desc   string              `json:"description"`
		Stages []workflow.TemplateStage `json:"stages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if body.Name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	t := workflow.Template{
		Name:        body.Name,
		Label:       body.Label,
		Description: body.Desc,
		Stages:      body.Stages,
	}
	data, err := yaml.Marshal(&t)
	if err != nil {
		http.Error(w, "marshal error", http.StatusInternalServerError)
		return
	}
	tmplDir := filepath.Join(".", ".teamix", "workflows")
	outPath := filepath.Join(tmplDir, body.Name+".yaml")
	if err := os.WriteFile(outPath, data, 0644); err != nil {
		http.Error(w, "write error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}


func (ts *TeamixServer) handleTemplateDelete(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	architects := ts.getArchitects()
	if !contains(architects, u.name) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	tmplDir := filepath.Join(".", ".teamix", "workflows")
	target := filepath.Join(tmplDir, body.Name+".yaml")
	if err := os.Remove(target); err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, map[string]any{"ok": true})
			return
		}
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}


func (ts *TeamixServer) handleWorkflowTemplates(w http.ResponseWriter, r *http.Request) {
	tmplDir := filepath.Join(".", ".teamix", "workflows")
	tmpls, err := workflow.LoadTemplates(tmplDir)
	if err != nil {
		writeJSON(w, []map[string]any{})
		return
	}
	type tJSON struct {
		Name        string `json:"name"`
		Label       string `json:"label"`
		Description string `json:"description,omitempty"`
	}
	out := make([]tJSON, 0, len(tmpls))
	for _, t := range tmpls {
		out = append(out, tJSON{Name: t.Name, Label: t.Label, Description: t.Description})
	}
	writeJSON(w, out)
}


func (ts *TeamixServer) handleWorkflowSelect(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Template string `json:"template"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// Load template and apply stages to user's workflow
	tmplDir := filepath.Join(".", ".teamix", "workflows")
	tmplFile := filepath.Join(tmplDir, body.Template+".yaml")
	data, err := os.ReadFile(tmplFile)
	if err != nil {
		http.Error(w, "template not found", http.StatusNotFound)
		return
	}
	var t workflow.Template
	if err := yaml.Unmarshal(data, &t); err != nil {
		http.Error(w, "invalid template", http.StatusBadRequest)
		return
	}
	// Apply template to user's workflow state
	stages := make([]workflow.Stage, 0, len(t.Stages))
	for _, s := range t.Stages {
		stages = append(stages, workflow.Stage(s.Name))
	}

	u.workflow.SetSessionInfo(u.ctrl.SessionDir(), strings.TrimSuffix(filepath.Base(u.ctrl.SessionPath()), ".jsonl"))
	u.workflow.SelectTemplate(stages, t.Stages)
	writeJSON(w, map[string]any{"ok": true, "stages": len(stages)})
}

