package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"reasonix/internal/workflow"
)

// Workflow stages and template handlers.

// 工作流模板目录：全局（workspaceRoot/.teamix/workflows，架构师维护）
// 与私有（users/<name>/.teamix/workflows，本人维护）。
func (ts *TeamixServer) globalWorkflowDir() string {
	return filepath.Join(ts.workspaceRoot, ".teamix", "workflows")
}

func (ts *TeamixServer) userWorkflowDir(u *userSession) string {
	return filepath.Join(u.userRoot, ".teamix", "workflows")
}

// wfTemplateWithTime 携带模板及文件修改时间，用于按“旧→新”排序（新增显示在底部）。
type wfTemplateWithTime struct {
	tpl workflow.Template
	mod time.Time
}

func loadWorkflowTemplatesByMtime(dir string) []wfTemplateWithTime {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []wfTemplateWithTime
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var t workflow.Template
		if err := yaml.Unmarshal(data, &t); err != nil || t.Name == "" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, wfTemplateWithTime{tpl: t, mod: info.ModTime()})
	}
	return out
}

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


func (ts *TeamixServer) handleTemplateGet(w http.ResponseWriter, r *http.Request, u *userSession) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "missing name", http.StatusBadRequest)
		return
	}
	// 编辑私有模板时前端会带 scope=private；默认先私有后全局查找。
	scope := r.URL.Query().Get("scope")
	dirs := []string{ts.globalWorkflowDir()}
	if scope == "private" {
		dirs = []string{ts.userWorkflowDir(u), ts.globalWorkflowDir()}
	}
	for _, dir := range dirs {
		path := filepath.Join(dir, name+".yaml")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var t workflow.Template
		if err := yaml.Unmarshal(data, &t); err != nil {
			continue
		}
		writeJSON(w, t)
		return
	}
	http.Error(w, "template not found", http.StatusNotFound)
}


func (ts *TeamixServer) handleTemplateSave(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Name   string                   `json:"name"`
		Label  string                   `json:"label"`
		Desc   string                   `json:"description"`
		Stages []workflow.TemplateStage `json:"stages"`
		Scope  string                   `json:"scope"` // "global" | "private" (default)
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if body.Name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	if body.Scope != "" && body.Scope != "global" && body.Scope != "private" {
		http.Error(w, `{"error":"invalid scope (global|private)"}`, http.StatusBadRequest)
		return
	}
	scope := body.Scope
	if scope == "" {
		scope = "private"
	}
	// 权限：全局仅架构师；私有任何登录用户可保存自己的工作流。
	var tmplDir string
	if scope == "global" {
		if !ts.isArchitect(u) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		tmplDir = ts.globalWorkflowDir()
	} else {
		tmplDir = ts.userWorkflowDir(u)
	}
	if err := os.MkdirAll(tmplDir, 0o755); err != nil {
		http.Error(w, "create dir failed", http.StatusInternalServerError)
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
	outPath := filepath.Join(tmplDir, body.Name+".yaml")
	if err := os.WriteFile(outPath, data, 0644); err != nil {
		http.Error(w, "write error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "name": body.Name, "scope": scope})
}


func (ts *TeamixServer) handleTemplateDelete(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Name  string `json:"name"`
		Scope string `json:"scope"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	scope := body.Scope
	if scope == "" {
		scope = "private"
	}
	// 权限：全局仅架构师；私有仅本人（列表中的私有模板都属于当前用户）。
	var tmplDir string
	if scope == "global" {
		if !ts.isArchitect(u) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		tmplDir = ts.globalWorkflowDir()
	} else {
		tmplDir = ts.userWorkflowDir(u)
	}
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


func (ts *TeamixServer) handleWorkflowTemplates(w http.ResponseWriter, r *http.Request, u *userSession) {
	type tJSON struct {
		Name        string `json:"name"`
		Label       string `json:"label"`
		Description string `json:"description,omitempty"`
		Source      string `json:"source"` // "global" | "private"
	}

	// 私有（当前用户）同名优先于全局：先收集私有，再跳过被覆盖的全局。
	priv := loadWorkflowTemplatesByMtime(ts.userWorkflowDir(u))
	glob := loadWorkflowTemplatesByMtime(ts.globalWorkflowDir())

	type entry struct {
		tJSON
		mod time.Time
	}
	seen := make(map[string]bool, len(priv)+len(glob))
	all := make([]entry, 0, len(priv)+len(glob))
	for _, t := range priv {
		seen[t.tpl.Name] = true
		all = append(all, entry{tJSON{Name: t.tpl.Name, Label: t.tpl.Label, Description: t.tpl.Description, Source: "private"}, t.mod})
	}
	for _, t := range glob {
		if seen[t.tpl.Name] {
			continue
		}
		all = append(all, entry{tJSON{Name: t.tpl.Name, Label: t.tpl.Label, Description: t.tpl.Description, Source: "global"}, t.mod})
	}
	// 按修改时间旧→新排序：新增的工作流显示在列表底部。
	sort.Slice(all, func(i, j int) bool { return all[i].mod.Before(all[j].mod) })

	out := make([]tJSON, 0, len(all))
	for _, e := range all {
		out = append(out, e.tJSON)
	}
	if out == nil {
		out = []tJSON{}
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
	// Load template and apply stages to user's workflow（私有同名优先于全局）
	tmplDirs := []string{ts.userWorkflowDir(u), ts.globalWorkflowDir()}
	var data []byte
	var err error
	for _, dir := range tmplDirs {
		data, err = os.ReadFile(filepath.Join(dir, body.Template+".yaml"))
		if err == nil {
			break
		}
	}
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

