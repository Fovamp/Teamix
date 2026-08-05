package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	reasonixAgent "reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	reasonixStore "reasonix/internal/store"
	"reasonix/internal/workflow"
	"strings"
	"time"
)

// Core agent interaction handlers: submit, approve, events, sessions, models.

func (ts *TeamixServer) handleEvents(w http.ResponseWriter, r *http.Request, u *userSession) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, unsubscribe := u.bc.Subscribe()
	defer unsubscribe()

	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case data, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-keepalive.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (ts *TeamixServer) handleSubmit(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Input string `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Input == "" {
		http.Error(w, "missing input", http.StatusBadRequest)
		return
	}
	trimmed := strings.TrimSpace(body.Input)
	if strings.HasPrefix(trimmed, "!") {
		http.Error(w, "shell commands are unavailable over HTTP", http.StatusForbidden)
		return
	}
	if strings.HasPrefix(trimmed, "/model ") {
		ref := strings.TrimSpace(strings.TrimPrefix(trimmed, "/model"))
		if ref != "" {
			if err := ts.switchModel(u, ref); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	// Workflow commands
	if u.workflow != nil && u.workflow.CurrentStage() != "" {
		lower := strings.ToLower(trimmed)
		if lower == "/advance" || lower == "进入下一阶段" || lower == "下一阶段" || lower == "进入下一个阶段" || lower == "下一个阶段" || lower == "next stage" || lower == "advance" {
			if !u.workflow.Advance() {
				http.Error(w, "cannot advance", http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if lower == "/rollback" || lower == "回到上一阶段" || lower == "上一阶段" || lower == "rollback" {
			if !u.workflow.Rollback() {
				http.Error(w, "cannot rollback", http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if strings.HasPrefix(lower, "/setstage ") || strings.HasPrefix(lower, "跳转到") || strings.HasPrefix(lower, "跳到") {
			// Keyword-based stage jumping has been removed.
			// AI handles jump intent via __JUMP_TO__ marker.
			// Fall through to normal AI processing.
		}
	}
	// Inject current workflow stage prompt + overview
	input := body.Input
	if u.workflow != nil {
		// Ensure workflow has session info for persist
		if u.workflow.SessionDir == "" && u.ctrl.SessionDir() != "" {
			u.workflow.SetSessionInfo(u.ctrl.SessionDir(), strings.TrimSuffix(filepath.Base(u.ctrl.SessionPath()), ".jsonl"))
		}
		stage := u.workflow.CurrentStage()
		if stage != "" {
			prompt := u.workflow.GetStagePrompt(stage)
			overview := "[Workflow Stages]\n"
			ordered := u.workflow.OrderedStages()
			for _, st := range ordered {
				var prefix, label string
				if st == stage {
					prefix = "> "
				} else {
					prefix = "  "
				}
				label = string(st)
				if l, ok := u.workflow.GetStageLabel(st); ok {
					label = l
				}
				if l, ok := workflow.StageLabels[st]; ok {
					label = l
				}
				overview += prefix + " " + label + "\n"
			}
			overview += "\n[Current Stage: " + string(stage) + "]\n"
			if prompt != "" {
				overview += prompt + "\n\n"
			}
			// Inject impact analysis result if available (auto-cleared after read)
			if result := u.workflow.GetImpactAnalysis(); result != "" {
				overview += result + "\n\n"
			}
			overview += "\n\n【工作流指令】\n1. 你当前只能执行【" + string(stage) + "】阶段的工作，不要提前执行后续阶段的工作。\n2. 当你认为当前阶段的工作已经全部完成时，请在回复末尾单独一行输出 阶段完成: <原因>。不要在其他地方输出这个标记。\n---\n"
			input = overview + input
		}
	}
	// Plan B: read MCP call chain analysis file written by agent, send additional notifications
	if u.workflow != nil && u.workflow.CurrentStage() == "analysis" && u.ctrl.SessionDir() != "" {
		sessionID := strings.TrimSuffix(filepath.Base(u.ctrl.SessionPath()), ".jsonl")
		impactDir := filepath.Join(ts.workspaceRoot, ".teamix", "impact-analysis")
		impactFile := filepath.Join(impactDir, sessionID+".json")
		if data, err := os.ReadFile(impactFile); err == nil {
			type mcpResult struct {
				CallerFiles []string `json:"callerFiles"`
			}
			var res mcpResult
			if json.Unmarshal(data, &res) == nil && len(res.CallerFiles) > 0 {
				for _, cf := range res.CallerFiles {
					cmd := exec.Command("git", "log", "--format=%an", "-5", cf)
					cmd.Dir = ts.workspaceRoot
					out, err := cmd.Output()
					if err != nil {
						continue
					}
					seen := make(map[string]bool)
					for _, line := range strings.Split(string(out), "\n") {
						user := strings.TrimSpace(line)
						if user == "" || seen[user] || !safeTokenName(user) {
							continue
						}
						seen[user] = true
						msg := u.name + " 修改的代码被 " + cf + " 调用，请确认是否影响你的工作"
						noti := notification{
							ID:          fmt.Sprintf("n%d", time.Now().UnixNano()),
							FromUser:    u.name,
							ToUser:      user,
							Project:     u.selectedProject,
							Message:     msg,
							FileChanged: cf,
							Read:        false,
							Time:        time.Now(),
						}
						// 统一写入个人通知文件（project 仅作展示）
						notis := ts.loadNotifications(user)
						if len(notis) > 100 {
							notis = notis[len(notis)-100:]
						}
						ts.saveNotifications(user, append(notis, noti))
					}
				}
			}
			os.Remove(impactFile)
		}
	}

	u.ctrl.SubmitHTTP(input)
	w.WriteHeader(http.StatusNoContent)
}

func (ts *TeamixServer) handleCancel(w http.ResponseWriter, _ *http.Request, u *userSession) {
	u.ctrl.Cancel()
	w.WriteHeader(http.StatusNoContent)
}

func (ts *TeamixServer) handleApprove(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		ID          string `json:"id"`
		Allow       bool   `json:"allow"`
		Session     bool   `json:"session"`
		Persist     bool   `json:"persist"`
		Scope       string `json:"scope"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	u.ctrl.Approve(body.ID, body.Allow, body.Session, body.Persist)
	w.WriteHeader(http.StatusNoContent)
}

func (ts *TeamixServer) handlePlan(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Plan bool `json:"plan"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	u.ctrl.SetPlanMode(body.Plan)
	w.WriteHeader(http.StatusNoContent)
}

func (ts *TeamixServer) handleCompact(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Instructions string `json:"instructions"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	u.ctrl.Compact(r.Context(), body.Instructions)
	w.WriteHeader(http.StatusAccepted)
}

func (ts *TeamixServer) handleNewSession(w http.ResponseWriter, _ *http.Request, u *userSession) {
	u.workflow = workflow.NewEmptyState("", "")
	// Force cancel any stuck running turn before creating a new session.
	u.ctrl.Cancel()
	if err := u.ctrl.NewSession(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Ensure session path exists so /sessions can find it
	sp := u.ctrl.SessionPath()
	if sp == "" {
		sp = reasonixAgent.NewSessionPath(u.ctrl.SessionDir(), u.ctrl.Label())
		u.ctrl.SetSessionPath(sp)
	}
	if _, err := os.Stat(sp); os.IsNotExist(err) {
		os.WriteFile(sp, []byte{}, 0644)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (ts *TeamixServer) handleRewind(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Turn  int    `json:"turn"`
		Scope string `json:"scope"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	scope := control.RewindCode // 'code' 命中默认值
	switch body.Scope {
	case "both":
		scope = control.RewindBoth
	case "conversation":
		scope = control.RewindConversation
	}
	before := len(u.ctrl.History())
	if err := u.ctrl.Rewind(body.Turn, scope); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	removed := before - len(u.ctrl.History())
	if removed < 0 {
		removed = 0
	}
	writeJSON(w, map[string]any{"ok": "rewound", "removed": removed})
}

func (ts *TeamixServer) handleFork(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Turn int    `json:"turn"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// 对齐 desktop 分叉语义：分叉后自动切换到新会话，新会话继承分叉轮的完整上下文。
	// 未指定名字时继承当前会话的标题（新分支看起来与原会话一致）。
	// turn < 0（前端"分叉会话"按钮）走 tip 分叉 Branch：从最新消息分叉、
	// 继承全部轮次，不依赖前端 turn 计数（turn_started 事件缺失等会造成错位）。
	name := body.Name
	if name == "" {
		if meta, ok, _ := reasonixAgent.LoadBranchMeta(u.ctrl.SessionPath()); ok && meta.Name != "" {
			name = meta.Name
		}
	}
	var (
		path string
		err  error
	)
	if body.Turn < 0 {
		path, err = u.ctrl.Branch(name)
	} else {
		path, err = u.ctrl.ForkNamed(body.Turn, name)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]string{"path": path})
}

func (ts *TeamixServer) handleSummarize(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Turn *int   `json:"turn"`
		Mode string `json:"mode"` // "from"（从此轮起总结）| "upto"（总结到此轮）
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Turn != nil {
		var err error
		if body.Mode == "upto" {
			err = u.ctrl.SummarizeUpTo(r.Context(), *body.Turn)
		} else {
			err = u.ctrl.SummarizeFrom(r.Context(), *body.Turn)
		}
		if err != nil {
			// 与 fork/rewind 一致：失败用 HTTP 400 返回，前端才能 toast 出原因
			// （200 + {ok:false} 会让前端当成成功而静默）。
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	} else {
		u.ctrl.Compact(r.Context(), "")
	}
	w.WriteHeader(http.StatusAccepted)
}

func (ts *TeamixServer) handleToolApprovalMode(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	u.ctrl.SetToolApprovalMode(body.Mode)
	w.WriteHeader(http.StatusNoContent)
}

func (ts *TeamixServer) handleAutoApproveTools(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		On bool `json:"on"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	u.ctrl.SetAutoApproveTools(body.On)
	w.WriteHeader(http.StatusNoContent)
}

func (ts *TeamixServer) handleBypass(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		On bool `json:"on"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	u.ctrl.SetBypass(body.On)
	w.WriteHeader(http.StatusNoContent)
}

func (ts *TeamixServer) handleGoal(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Goal string `json:"goal"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if body.Goal == "" {
		u.ctrl.ClearGoal()
	} else {
		u.ctrl.SetGoal(body.Goal)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (ts *TeamixServer) handleAnswer(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		ID      string            `json:"id"`
		Answers []event.AskAnswer `json:"answers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	u.ctrl.AnswerQuestion(body.ID, body.Answers)
	w.WriteHeader(http.StatusNoContent)
}

func (ts *TeamixServer) handleResume(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Path == "" {
		http.Error(w, "missing path", http.StatusBadRequest)
		return
	}
	loaded, err := reasonixAgent.LoadSession(body.Path)
	if err != nil {
		http.Error(w, "load session: "+err.Error(), http.StatusBadRequest)
		return
	}
	u.ctrl.Resume(loaded, body.Path)
	// Reload workflow state for this session
	sd := u.ctrl.SessionDir()
	sp := u.ctrl.SessionPath()
	if sd != "" && sp != "" {
		sid := strings.TrimSuffix(filepath.Base(sp), ".jsonl")
		u.workflow = workflow.LoadState(sd, sid)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (ts *TeamixServer) handleForget(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "missing name", http.StatusBadRequest)
		return
	}
	u.ctrl.ForgetMemory(body.Name)
	// Backward compatibility: if the memory was saved before slug normalization,
	// the file might exist under the raw name. Try to remove it directly.
	if memSet := u.ctrl.Memory(); memSet != nil && memSet.Store.Dir != "" {
		rawPath := filepath.Join(memSet.Store.Dir, body.Name+".md")
		os.Remove(rawPath) // ignore error if file doesn't exist
	}
	w.WriteHeader(http.StatusNoContent)
}

func (ts *TeamixServer) handleHistory(w http.ResponseWriter, r *http.Request, u *userSession) {
	msgs := u.ctrl.History()
	writeJSON(w, historyMessages(msgs))
}

func (ts *TeamixServer) handleContext(w http.ResponseWriter, r *http.Request, u *userSession) {
	used, window := u.ctrl.ContextSnapshot()
	writeJSON(w, map[string]any{"used": used, "window": window})
}

func (ts *TeamixServer) handleStatus(w http.ResponseWriter, r *http.Request, u *userSession) {
	rs := u.ctrl.RuntimeStatus()
	used, window := u.ctrl.ContextSnapshot()
	hit, miss := u.ctrl.SessionCache()
	var bal any
	if b, err := u.ctrl.Balance(r.Context()); err == nil && b != nil {
		bal = map[string]any{"display": b.Display(), "available": b.Available}
	}
	var lastUsage any
	if lu := u.ctrl.LastUsage(); lu != nil {
		lastUsage = map[string]any{
			"totalTokens":      lu.TotalTokens,
			"promptTokens":     lu.PromptTokens,
			"completionTokens": lu.CompletionTokens,
			"cacheHitTokens":   lu.CacheHitTokens,
			"cacheMissTokens":  lu.CacheMissTokens,
		}
	}
	writeJSON(w, map[string]any{
		"label":            u.ctrl.Label(),
		"running":          rs.Running,
		"plan":             u.ctrl.PlanMode(),
		"autoApproveTools": u.ctrl.AutoApproveTools(),
		"bypass":           u.ctrl.Bypass(),
		"toolApprovalMode": u.ctrl.ToolApprovalMode(),
		"goal":             u.ctrl.Goal(),
		"goalStatus":       u.ctrl.GoalStatus(),
		"cwd":              u.ctrl.SessionDir(),
		"workspaceRoot":    ts.workspaceRoot,
		"used":             used,
		"window":           window,
		"cacheHit":         hit,
		"cacheMiss":        miss,
		"user":             u.name,
		"selectedProject":  u.selectedProject,
		"balance":          bal,
		"lastUsage":        lastUsage,
	})
}

func (ts *TeamixServer) handleSessions(w http.ResponseWriter, r *http.Request, u *userSession) {
	dir := u.ctrl.SessionDir()
	if dir == "" {
		writeJSON(w, []any{})
		return
	}
	type sessionEntry struct {
		Name    string `json:"name"`
		Path    string `json:"path"`
		Title   string `json:"title,omitempty"`
		Turns   int    `json:"turns,omitempty"`
		Current bool   `json:"current,omitempty"`
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		writeJSON(w, []any{})
		return
	}
	current := filepath.Clean(u.ctrl.SessionPath())
	var out []sessionEntry
	for _, e := range entries {
		if e.IsDir() || !reasonixStore.IsSessionTranscriptName(e.Name()) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if reasonixAgent.IsCleanupPending(path) {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".jsonl")
		entry := sessionEntry{Name: name, Path: path, Current: filepath.Clean(path) == current}
		if first, turns := reasonixAgent.SessionPreview(path); turns > 0 {
			entry.Turns = turns
			entry.Title = sessionTitle(u.ctrl, e.Name(), first)
		}
		out = append(out, entry)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if out == nil {
		out = []sessionEntry{}
	}
	writeJSON(w, out)
}

func (ts *TeamixServer) handleModels(w http.ResponseWriter, r *http.Request, u *userSession) {
	cfg, err := config.Load()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type modelEntry struct {
		Ref      string `json:"ref"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Kind     string `json:"kind,omitempty"`
		Active   bool   `json:"active,omitempty"`
		Default  bool   `json:"default,omitempty"`
	}
	current := teamixCurrentModelRef(u.ctrl)
	label := u.ctrl.Label()
	seen := make(map[string]bool) // dedup by model name
	var out []modelEntry
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if !p.Configured() {
			continue
		}
		models := p.ChatModelList()
		if len(models) == 0 {
			models = p.ModelList()
		}
		for _, model := range models {
			ref := p.Name + "/" + model
			if seen[model] {
				continue
			}
			seen[model] = true
			active := ref == current || p.Name == current
			if !active && current == label && model == label {
				if len(out) == 0 {
					active = true
				} else {
					active = ref == cfg.DefaultModel
				}
			}
			out = append(out, modelEntry{
				Ref:      ref,
				Provider: p.Name,
				Model:    model,
				Kind:     p.Kind,
				Active:   active,
				Default:  ref == cfg.DefaultModel || p.Name == cfg.DefaultModel,
			})
		}
	}
	if out == nil {
		out = []modelEntry{}
	}
	writeJSON(w, map[string]any{
		"current": current, "label": label, "default": cfg.DefaultModel,
		"models": out,
		// 透明化选择：模型列表只含公网模型（内部 Qwen 不在 reasonix.toml，天然不可选）；
		// offline 为"仅本地模式"开关状态（手动全走 Qwen 唯一入口）。
		"offline": ts.offline.Load(),
	})
}

func (ts *TeamixServer) handleCheckpoints(w http.ResponseWriter, _ *http.Request, u *userSession) {
	cps := u.ctrl.Checkpoints()
	writeJSON(w, cps)
}

// branchView 在 BranchInfo 基础上补父会话标题，前端分支弹窗可显示
// "分叉自 <父标题>" 而不是无意义的父 ID 前几位。
type branchView struct {
	reasonixAgent.BranchInfo
	ParentTitle string `json:"parent_title,omitempty"`
}

func (ts *TeamixServer) handleBranches(w http.ResponseWriter, _ *http.Request, u *userSession) {
	branches, err := u.ctrl.Branches()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sessDir := u.ctrl.SessionDir()
	parentTitles := map[string]string{}
	out := make([]branchView, 0, len(branches))
	for _, b := range branches {
		v := branchView{BranchInfo: b}
		if pid := b.ParentID; pid != "" {
			title, ok := parentTitles[pid]
			if !ok {
				title = sessionPreviewTitle(sessDir, pid)
				parentTitles[pid] = title
			}
			v.ParentTitle = title
		}
		out = append(out, v)
	}
	tree := u.ctrl.BranchTreeText()
	writeJSON(w, map[string]any{"branches": out, "tree": tree})
}

// sessionPreviewTitle 返回父会话（按 BranchID 定位）的第一条用户消息作为标题，
// 供分支弹窗标注父分支。找不到会话或没有消息时回退为父 ID。
func sessionPreviewTitle(sessDir, parentID string) string {
	if sessDir == "" || parentID == "" {
		return parentID
	}
	path := filepath.Join(sessDir, parentID+".jsonl")
	first, turns := reasonixAgent.SessionPreview(path)
	if turns <= 0 || first == "" {
		return parentID
	}
	if r := []rune(first); len(r) > 60 {
		return string(r[:60]) + "…"
	}
	return first
}

// handleBranchSwitch 切换分支：直接调 ctrl.SwitchBranch，不走 agent 对话
// （避免 "/switch xxx" 被当作用户消息提交、产生 "switched to branch" 通知进会话流）。
func (ts *TeamixServer) handleBranchSwitch(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Ref string `json:"ref"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Ref) == "" {
		http.Error(w, "bad request: ref required", http.StatusBadRequest)
		return
	}
	info, err := u.ctrl.SwitchBranch(strings.TrimSpace(body.Ref))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "branch": info})
}

func (ts *TeamixServer) handleSkills(w http.ResponseWriter, _ *http.Request, u *userSession) {
	writeJSON(w, []any{})
}

func (ts *TeamixServer) handleTodos(w http.ResponseWriter, _ *http.Request, u *userSession) {
	writeJSON(w, []any{})
}

func (ts *TeamixServer) switchModel(u *userSession, ref string) error {
	if u.ctrl.Running() {
		return fmt.Errorf("cannot switch model while a turn is running")
	}
	cur := u.ctrl
	if err := cur.Snapshot(); err != nil {
		slog.Warn("teamix: snapshot before model switch", "err", err)
	}
	prevPath := cur.SessionPath()
	carried := cur.History()

	bc := NewBroadcaster()
	newCtrl, err := boot.Build(context.Background(), boot.Options{
		Model:               ref,
		RequireKey:          true,
		Sink:                bc,
		Stderr:              os.Stderr,
		ExcludedPluginNames: ts.excludedMachineMCPNames(),
		MemoryUserDir:       filepath.Join(u.userRoot, ".teamix"),
		ExcludeHomeSkills:   true,
		WrapProvider:        ts.headroomHook,
		Router:              ts.routerCfg(u.name),
	})
	if err != nil {
		return fmt.Errorf("switch model: %w", err)
	}
	newPath := reasonixAgent.ContinueSessionPath(prevPath, newCtrl.SessionDir(), newCtrl.Label())
	newCtrl.AdoptHistory(carried, newPath)

	u.ctrl = newCtrl
	u.bc = bc
	cur.Close()
	return nil
}
