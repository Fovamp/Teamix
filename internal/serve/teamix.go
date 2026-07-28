// Package serve exposes a control.Controller over HTTP. The TeamixServer
// extension adds multi-user support: each user gets an isolated Controller
// and Broadcaster, identified by a random token obtained via /teamix/login.
package serve

import (
	"reasonix/internal/memory"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	
	"strings"
	"sort"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/teamixconfig"
	"reasonix/internal/workflow"
	"reasonix/internal/control"
	"reasonix/internal/capabilities"
	"reasonix/internal/keypool"
	"reasonix/internal/event"
	reasonixAgent "reasonix/internal/agent"
	reasonixStore "reasonix/internal/store"
)

// userSession holds one user's isolated controller and event broadcaster.
type userSession struct {
	ctrl         control.SessionAPI
	bc           *Broadcaster
	name         string
	token        string
	workflow     *workflow.State
}

// TeamixServer wraps the HTTP server with multi-user session management.
// Each user authenticates via a simple name-based login and gets an isolated
// Controller + Broadcaster. All API routes extract the user from a token
// passed as query parameter or Authorization header.
type TeamixServer struct {
	mu        sync.RWMutex
	sessions  map[string]*userSession // token → session
	nameToTok map[string]string       // name → token (fast lookup)
	serveCfg  config.ServeConfig

	// Default model to use when building new user controllers.
	modelRef string
	// Runtime profile for new controllers.
	profile string

	// Workspace root directory
	workspaceRoot string

	// Project config from .teamix/config.yaml
	teamixCfg *teamixconfig.Config

	// Key pool for load-balanced API keys (architect-configured).
	keyPool *keypool.Pool
	// Project-level capability overrides.
	capCfg  *capabilities.AllConfigs

	// Embedded frontend assets.
	indexHTML []byte
	logo      []byte

	mux http.Handler
}

// NewTeamixServer creates a TeamixServer. No controllers are built until
// the first user logs in.
func NewTeamixServer(serveCfg config.ServeConfig, modelRef, profile string) *TeamixServer {
	ts := &TeamixServer{
		sessions:  make(map[string]*userSession),
		nameToTok: make(map[string]string),
		serveCfg:  serveCfg,
		modelRef:  modelRef,
		profile:   profile,
		indexHTML: indexHTML,
		logo:      logoWordmarkSVG,
	}
	ts.teamixCfg = ts.loadTeamixConfig()
	ts.keyPool = keypool.NewPool("DEEPSEEK_API_KEY")
	ts.keyPool.Load(ts.workspaceRoot)
	ts.capCfg = capabilities.LoadAll(ts.workspaceRoot)
	ts.mux = ts.buildHandler()
	return ts
}

// generateToken returns a cryptographically random hex string.
func teamixGenerateToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		slog.Error("teamix: crypto/rand failed", "err", err)
	}
	return hex.EncodeToString(b)
}

// Login authenticates (or creates) a user session by name.
// Returns the session, whether it was newly created, and any error.
func (ts *TeamixServer) Login(name string) (*userSession, bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, false, errors.New("name is required")
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Reuse existing session if the user already logged in.
	if tok, ok := ts.nameToTok[name]; ok {
		if sess, ok := ts.sessions[tok]; ok {
			return sess, false, nil
		}
	}

	// Create a new user session.
	token := teamixGenerateToken()

	// Acquire a load-balanced API key from the pool.
	ts.keyPool.Acquire()
	bc := NewBroadcaster()

	ctrl, err := boot.Build(context.Background(), boot.Options{
		Model:      ts.modelRef,
		RequireKey: true,
		Sink:       bc,
		Stderr:     os.Stderr,
		TokenMode:  ts.profile,
		WorkspaceRoot: ts.workspaceRoot,
		SessionDir: filepath.Join(config.SessionDir(), name),
	})
	if err != nil {
		return nil, false, fmt.Errorf("build controller for %q: %w", name, err)
	}

	ctrl.EnableInteractiveApproval()
	ctrl.EnsureSessionPath()

	sess := &userSession{
		ctrl:     ctrl,
		bc:       bc,
		name:     name,
		token:    token,
		workflow: workflow.NewEmptyState("", ""),
	}
	ts.sessions[token] = sess
	ts.nameToTok[name] = token

	slog.Info("teamix: user logged in", "name", name, "token_prefix", token[:8])
	return sess, true, nil
}

// GetSession returns the user session for a given token, or nil.
func (ts *TeamixServer) GetSession(token string) *userSession {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.sessions[token]
}

// extractUser extracts the user session from the request by checking
// the "token" query parameter or Authorization: Bearer header.
func (ts *TeamixServer) extractUser(r *http.Request) *userSession {
	token := r.URL.Query().Get("token")
	if token == "" {
		if ah := r.Header.Get("Authorization"); strings.HasPrefix(ah, "Bearer ") {
			token = strings.TrimPrefix(ah, "Bearer ")
		}
	}
	if token == "" {
		return nil
	}
	return ts.GetSession(token)
}

// withUser is middleware that extracts the user token and calls the next
// handler with the user's session. It responds 401 if the token is missing
// or invalid.
func (ts *TeamixServer) withUser(next func(http.ResponseWriter, *http.Request, *userSession)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := ts.extractUser(r)
		if u == nil {
			http.Error(w, `{"error":"unauthorized","message":"please login first"}`, http.StatusUnauthorized)
			return
		}
		next(w, r, u)
	}
}

// Handler returns the HTTP handler.
func (ts *TeamixServer) Handler() http.Handler {
	return ts.mux
}

// RunGraceful serves with graceful shutdown. It listens for SIGINT/SIGTERM on
// the provided context and drains active connections for up to 10 seconds
// before returning.
func (ts *TeamixServer) RunGraceful(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           ts.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("teamix: shutting down gracefully")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Warn("teamix: graceful shutdown failed", "err", err)
		}
		return <-errCh
	}
}


// buildHandler creates the route mux.
func (ts *TeamixServer) SetWorkspaceRoot(wr string) {
	ts.workspaceRoot = wr
	cfg, err := teamixconfig.Load(ts.workspaceRoot)
	if err != nil {
		slog.Warn("teamix: failed to load .teamix/config.yaml", "err", err)
	}
	ts.teamixCfg = cfg
}

func (ts *TeamixServer) loadTeamixConfig() *teamixconfig.Config {
	cfg, err := teamixconfig.Load(ts.workspaceRoot)
	if err != nil {
		slog.Warn("teamix: failed to load .teamix/config.yaml", "err", err)
		return teamixconfig.DefaultConfig()
	}
	return cfg
}

func (ts *TeamixServer) ReloadModules() {
	ts.teamixCfg = ts.loadTeamixConfig()
}

func (ts *TeamixServer) buildHandler() http.Handler {
	mux := http.NewServeMux()

	// Public routes (no auth required)
	mux.HandleFunc("GET /", ts.handleIndex)
	mux.HandleFunc("GET /assets/logo-wordmark.svg", ts.handleLogo)
	mux.HandleFunc("POST /teamix/login", ts.handleLogin)

	// Authenticated routes — each delegates to a per-user handler.
	mux.HandleFunc("GET /events", ts.withUser(ts.handleEvents))
	mux.HandleFunc("GET /history", ts.withUser(ts.handleHistory))
	mux.HandleFunc("GET /context", ts.withUser(ts.handleContext))
	mux.HandleFunc("GET /status", ts.withUser(ts.handleStatus))
	mux.HandleFunc("GET /sessions", ts.withUser(ts.handleSessions))
	mux.HandleFunc("GET /models", ts.withUser(ts.handleModels))
	mux.HandleFunc("GET /checkpoints", ts.withUser(ts.handleCheckpoints))
	mux.HandleFunc("GET /branches", ts.withUser(ts.handleBranches))
	mux.HandleFunc("GET /skills", ts.withUser(ts.handleSkills))
	mux.HandleFunc("GET /todos", ts.withUser(ts.handleTodos))
	mux.HandleFunc("GET /teamix/modules", ts.handleModules)
	mux.HandleFunc("GET /teamix/tree", ts.handleTree)
	mux.HandleFunc("GET /teamix/workflows/templates", ts.handleWorkflowTemplates)
	mux.HandleFunc("GET /teamix/workflows/template", ts.handleTemplateGet)
	mux.HandleFunc("POST /teamix/workflows/template/save", ts.withUser(ts.handleTemplateSave))
	mux.HandleFunc("POST /teamix/workflows/template/delete", ts.withUser(ts.handleTemplateDelete))
	mux.HandleFunc("GET /teamix/user/role", ts.withUser(ts.handleUserRole))
	mux.HandleFunc("POST /teamix/workflows/select", ts.withUser(ts.handleWorkflowSelect))
	mux.HandleFunc("GET /teamix/file", ts.handleFile)
	mux.HandleFunc("GET /teamix/project", ts.handleProject)
	mux.HandleFunc("GET /teamix/workflow", ts.withUser(ts.handleWorkflowGet))
	mux.HandleFunc("POST /teamix/workflow/advance", ts.withUser(ts.handleWorkflowAdvance))
	mux.HandleFunc("POST /teamix/workflow/rollback", ts.withUser(ts.handleWorkflowRollback))
	mux.HandleFunc("POST /teamix/workflow/setstage", ts.withUser(ts.handleWorkflowSetStage))
	mux.HandleFunc("GET /teamix/memory", ts.withUser(ts.handleMemoryList))
	mux.HandleFunc("POST /teamix/memory/save", ts.withUser(ts.handleMemorySave))
	mux.HandleFunc("POST /teamix/upload", ts.withUser(ts.handleUpload))

	// Capabilities & Secrets (architect-only writes)
	mux.HandleFunc("GET /teamix/capabilities", ts.withUser(ts.handleCapabilitiesGet))
	mux.HandleFunc("POST /teamix/capabilities/save", ts.withUser(ts.handleCapabilitiesSave))
	mux.HandleFunc("GET /teamix/secrets/status", ts.withUser(ts.handleSecretsStatus))
	mux.HandleFunc("POST /teamix/secrets/set", ts.withUser(ts.handleSecretsSet))
	mux.HandleFunc("POST /teamix/secrets/delete", ts.withUser(ts.handleSecretsDelete))
	mux.HandleFunc("POST /teamix/keypool/strategy", ts.withUser(ts.handleKeyPoolStrategy))
	mux.HandleFunc("GET /teamix/mcp/servers", ts.withUser(ts.handleMCPServers))
	mux.HandleFunc("GET /teamix/skills", ts.withUser(ts.handleSkillsList))
	mux.HandleFunc("POST /teamix/mcp/add", ts.withUser(ts.handleMCPAdd))
	mux.HandleFunc("POST /teamix/mcp/remove", ts.withUser(ts.handleMCPRemove))
	mux.HandleFunc("POST /teamix/skills/toggle", ts.withUser(ts.handleSkillToggle))
	// Notification endpoints
	mux.HandleFunc("GET /teamix/notifications", ts.withUser(ts.handleNotifications))
	mux.HandleFunc("POST /teamix/notifications/read", ts.withUser(ts.handleNotificationRead))
	mux.HandleFunc("POST /teamix/notifications/create", ts.withUser(ts.handleNotificationCreate))

	mux.HandleFunc("POST /submit", ts.withUser(ts.handleSubmit))
	mux.HandleFunc("POST /cancel", ts.withUser(ts.handleCancel))
	mux.HandleFunc("POST /approve", ts.withUser(ts.handleApprove))
	mux.HandleFunc("POST /plan", ts.withUser(ts.handlePlan))
	mux.HandleFunc("POST /compact", ts.withUser(ts.handleCompact))
	mux.HandleFunc("POST /new", ts.withUser(ts.handleNewSession))
	mux.HandleFunc("POST /rewind", ts.withUser(ts.handleRewind))
	mux.HandleFunc("POST /fork", ts.withUser(ts.handleFork))
	mux.HandleFunc("POST /summarize", ts.withUser(ts.handleSummarize))
	mux.HandleFunc("POST /tool-approval-mode", ts.withUser(ts.handleToolApprovalMode))
	mux.HandleFunc("POST /auto-approve-tools", ts.withUser(ts.handleAutoApproveTools))
	mux.HandleFunc("POST /bypass", ts.withUser(ts.handleBypass))
	mux.HandleFunc("POST /goal", ts.withUser(ts.handleGoal))
	mux.HandleFunc("POST /answer", ts.withUser(ts.handleAnswer))
	mux.HandleFunc("POST /resume", ts.withUser(ts.handleResume))
	mux.HandleFunc("POST /forget", ts.withUser(ts.handleForget))
	mux.HandleFunc("POST /delete-session", ts.withUser(ts.handleDeleteSession))

	return logMiddleware(csrfGuard(mux))
}

// ── Public handlers ──

func (ts *TeamixServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	lang := "auto"
	if cfg, err := config.Load(); err == nil {
		if dl := cfg.DesktopLanguage(); dl != "" {
			lang = dl
		}
	}
	html := string(ts.indexHTML)
	html = strings.ReplaceAll(html, "__LANG__", lang)
	_, _ = w.Write([]byte(html))
}

func (ts *TeamixServer) handleLogo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(ts.logo)
}

func (ts *TeamixServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeJSON(w, map[string]string{"error": "name is required"})
		return
	}
	sess, isNew, err := ts.Login(body.Name)
	if err != nil {
		slog.Error("teamix: login failed", "name", body.Name, "err", err)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	slog.Info("teamix: login", "name", body.Name, "new", isNew)
	writeJSON(w, map[string]any{
		"token":    sess.token,
		"userName": sess.name,
		"new":      isNew,
	})
}

// ── Authenticated handlers ──

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
		http.Error(w, `{"error":"stage is required"}`, http.StatusBadRequest)
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

func (ts *TeamixServer) handleProject(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"workspaceRoot": ts.workspaceRoot,
		"projectName":   filepath.Base(ts.workspaceRoot),
	})
}

func (ts *TeamixServer) handleUpload(w http.ResponseWriter, r *http.Request, u *userSession) {
	// Limit upload size to 50MB
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "upload too large or bad form", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	relPath := r.FormValue("path")
	if relPath == "" {
		relPath = header.Filename
	}
	// Sanitize: prevent path traversal
	relPath = filepath.Clean(relPath)
	if strings.Contains(relPath, "..") || filepath.IsAbs(relPath) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	fullPath := filepath.Join(ts.workspaceRoot, filepath.FromSlash(relPath))
	// Ensure within workspace
	absRoot, _ := filepath.Abs(ts.workspaceRoot)
	absPath, _ := filepath.Abs(fullPath)
	if !strings.HasPrefix(absPath, absRoot) {
		http.Error(w, "path outside workspace", http.StatusForbidden)
		return
	}
	// Create parent dirs
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		http.Error(w, "cannot create directory", http.StatusInternalServerError)
		return
	}
	dst, err := os.Create(fullPath)
	if err != nil {
		http.Error(w, "cannot create file", http.StatusInternalServerError)
		return
	}
	defer dst.Close()
	if _, err := io.Copy(dst, file); err != nil {
		http.Error(w, "write error", http.StatusInternalServerError)
		return
	}
	slog.Info("teamix: file uploaded", "path", relPath, "user", u.name)
	writeJSON(w, map[string]any{"ok": true, "path": filepath.ToSlash(relPath)})
}

type treeEntry struct {
	Name     string       `json:"name"`
	Path     string       `json:"path"`
	IsDir    bool         `json:"isDir"`
	Children []*treeEntry `json:"children,omitempty"`
}

func (ts *TeamixServer) handleFile(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" || strings.Contains(path, "..") {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	fullPath := filepath.Join(ts.workspaceRoot, filepath.FromSlash(path))
	absRoot, _ := filepath.Abs(ts.workspaceRoot)
	absPath, _ := filepath.Abs(fullPath)
	if !strings.HasPrefix(absPath, absRoot) {
		http.Error(w, "path outside workspace", http.StatusForbidden)
		return
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	truncated := len(data) > 100000
	if truncated {
		data = data[:100000]
	}
	writeJSON(w, map[string]any{
		"path":       path,
		"body":       string(data),
		"size":       len(data),
		"truncated":  truncated,
	})
}

func (ts *TeamixServer) handleUserRole(w http.ResponseWriter, r *http.Request, u *userSession) {
	role := "developer"
	architects := ts.getArchitects()
	for _, a := range architects {
		if a == u.name {
			role = "architect"
			break
		}
	}
	writeJSON(w, map[string]any{"role": role, "user": u.name})
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

// getArchitects reads the architects list from the Agent's .teamix/config.yaml
// (the directory where the server runs from), NOT from the project directory.
func (ts *TeamixServer) getArchitects() []string {
	cfg, err := teamixconfig.Load(".")
	if err != nil || cfg == nil {
		return nil
	}
	return cfg.Workflow.Architects
}

func (ts *TeamixServer) handleTemplateDelete(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, `{"error":"name required"}`, http.StatusBadRequest)
		return
	}
	architects := ts.getArchitects()
	if !contains(architects, u.name) {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		return
	}
	tmplDir := filepath.Join(".", ".teamix", "workflows")
	target := filepath.Join(tmplDir, body.Name+".yaml")
	if err := os.Remove(target); err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, map[string]any{"ok": true})
			return
		}
		http.Error(w, `{"error":"delete failed"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
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

func (ts *TeamixServer) handleTree(w http.ResponseWriter, r *http.Request) {
	root := ts.workspaceRoot
	if root == "" {
		writeJSON(w, []*treeEntry{})
		return
	}
	tree := buildTree(root, "", 0, 15)
	writeJSON(w, tree)
}

func buildTree(base, relPath string, depth, maxDepth int) []*treeEntry {
	if depth > maxDepth {
		return nil
	}
	fullPath := filepath.Join(base, relPath)
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return nil
	}
	skipDirs := map[string]bool{
		".git": true, ".teamix": true, ".reasonix": true,
		"node_modules": true, "vendor": true, ".venv": true,
		"__pycache__": true, "bin": true, "obj": true,
		".idea": true, ".vscode": true, ".mvn": true, "target": true,
		"dist": true, ".gradle": true, "build": true, "out": true,
	}
	var out []*treeEntry
	for _, e := range entries {
		name := e.Name()
		if skipDirs[name] || strings.HasPrefix(name, ".") {
			continue
		}
		childRel := filepath.Join(relPath, name)
		entry := &treeEntry{Name: name, Path: filepath.ToSlash(childRel), IsDir: e.IsDir()}
		if e.IsDir() {
			entry.Children = buildTree(base, childRel, depth+1, maxDepth)
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func (ts *TeamixServer) handleModules(w http.ResponseWriter, r *http.Request) {
	type modJSON struct {
		Name        string `json:"name"`
		Path        string `json:"path"`
		Description string `json:"description,omitempty"`
	}
	cfg := ts.teamixCfg
	if cfg == nil {
		writeJSON(w, []modJSON{})
		return
	}
	out := make([]modJSON, 0, len(cfg.Modules))
	for _, m := range cfg.Modules {
		out = append(out, modJSON{Name: m.Name, Path: m.Path, Description: m.Description})
	}
	writeJSON(w, out)
}

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
		if strings.HasPrefix(lower, "/setstage ") || strings.HasPrefix(lower, "跳到") {
			var stageStr string
			if strings.HasPrefix(lower, "/setstage ") {
				stageStr = strings.TrimSpace(strings.TrimPrefix(lower, "/setstage "))
			} else {
				stageStr = strings.TrimSpace(strings.TrimPrefix(lower, "跳到"))
			}
			if stageStr != "" {
				if st,ok:=u.workflow.FindStageByLabel(stageStr);ok{stageStr=string(st)}else if !u.workflow.SetStage(workflow.Stage(stageStr)){
					http.Error(w, "invalid stage", http.StatusBadRequest)
					return
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}
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
				if st == stage { prefix = "> " } else { prefix = "  " }
				label = string(st)
				if l, ok := u.workflow.GetStageLabel(st); ok { label = l }
				if l, ok := workflow.StageLabels[st]; ok { label = l }
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
						if user == "" || seen[user] {
							continue
						}
						seen[user] = true
						msg := u.name + " 修改的代码被 " + cf + " 调用，请确认是否影响你的工作"
						noti := notification{
							ID:          fmt.Sprintf("n%d", time.Now().UnixNano()),
							FromUser:    u.name,
							ToUser:      user,
							Message:     msg,
							FileChanged: cf,
							Read:        false,
							Time:        time.Now(),
						}
						notis := ts.loadNotifications(user)
						if len(notis) > 100 {
							notis = notis[len(notis)-100:]
						}
						notis = append(notis, noti)
						ts.saveNotifications(user, notis)
					}
				}
			}
			os.Remove(impactFile)
		}
	}

	u.ctrl.SubmitHTTP(input)
	w.WriteHeader(http.StatusNoContent)
}

func (ts *TeamixServer) handleCancel(w http.ResponseWriter, r *http.Request, u *userSession) {
	u.ctrl.Cancel()
	w.WriteHeader(http.StatusNoContent)
}

func (ts *TeamixServer) handleApprove(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		ID      string `json:"id"`
		Allow bool `json:"allow"`
		Session bool `json:"session"`
		Persist bool `json:"persist"`
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

func (ts *TeamixServer) handleNewSession(w http.ResponseWriter, r *http.Request, u *userSession) {
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
		Turn int `json:"turn"`
		Scope string `json:"scope"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	scope := control.RewindCode
	if body.Scope == "conversation" {
		scope = control.RewindConversation
	}
	if err := u.ctrl.Rewind(body.Turn, scope); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]string{"ok": "rewound"})
}

func (ts *TeamixServer) handleFork(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Turn int `json:"turn"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	path, err := u.ctrl.Fork(body.Turn)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]string{"path": path})
}

func (ts *TeamixServer) handleSummarize(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Turn *int `json:"turn"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Turn != nil {
		u.ctrl.SummarizeFrom(r.Context(), *body.Turn)
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
		ID string `json:"id"`
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

// runImpactAnalysis collects changed files and returns them as context for the agent.
// Deep call chain analysis is handled by the agent via MCP tools in the workflow prompt.
// Notifications from agent analysis file are sent via Plan B reader in handleSubmit.
func (ts *TeamixServer) runImpactAnalysis(currentUser string) string {
	root := ts.workspaceRoot
	if root == "" { root = "." }
	cmd := exec.Command("git", "diff", "--name-only", "HEAD")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil { return "变更文件获取失败：" + err.Error() }
	changedFiles := strings.Fields(string(out))
	if len(changedFiles) == 0 { return "无未提交的变更文件。" }
	summary := "【变更文件列表】\n"
	summary += "以下文件已被修改（共 " + fmt.Sprint(len(changedFiles)) + " 个）：\n"
	for _, f := range changedFiles { summary += "  - " + f + "\n" }
	summary += "\n请使用 codebase-memory MCP 工具对这些文件中的导出函数/接口进行调用链分析，"
	summary += "然后将分析结果通过 write_file 写入 .teamix/impact-analysis/ 目录下的 JSON 文件，"
	summary += "系统将自动读取该文件并通知相关用户。\n"
	return summary
}
// ── Notification handlers ──

type notification struct {
	ID          string    `json:"id"`
	FromUser    string    `json:"fromUser"`
	ToUser      string    `json:"toUser"`
	Message     string    `json:"message"`
	FileChanged string    `json:"fileChanged,omitempty"`
	Read        bool      `json:"read"`
	Time        time.Time `json:"time"`
}

func (ts *TeamixServer) notiDir() string {
	base := ts.workspaceRoot
	if base == "" {
		base = config.ReasonixHomeDir()
	}
	dir := filepath.Join(base, ".teamix", "notifications")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func (ts *TeamixServer) notiFile(user string) string {
	return filepath.Join(ts.notiDir(), user+".json")
}

func (ts *TeamixServer) loadNotifications(user string) []notification {
	data, err := os.ReadFile(ts.notiFile(user))
	if err != nil {
		return []notification{}
	}
	var notis []notification
	if err := json.Unmarshal(data, &notis); err != nil {
		return []notification{}
	}
	for i := range notis {
		if notis[i].ID == "" {
			notis[i].ID = fmt.Sprintf("n%d", i)
		}
	}
	return notis
}

func (ts *TeamixServer) saveNotifications(user string, notis []notification) {
	data, _ := json.MarshalIndent(notis, "", "  ")
	_ = os.WriteFile(ts.notiFile(user), data, 0o644)
}

func (ts *TeamixServer) handleNotifications(w http.ResponseWriter, r *http.Request, u *userSession) {
	notis := ts.loadNotifications(u.name)
	writeJSON(w, notis)
}

func (ts *TeamixServer) handleNotificationRead(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	notis := ts.loadNotifications(u.name)
	changed := false
	for i := range notis {
		if body.ID == "" || notis[i].ID == body.ID {
			if !notis[i].Read {
				notis[i].Read = true
				changed = true
			}
			if body.ID != "" {
				break
			}
		}
	}
	if changed {
		ts.saveNotifications(u.name, notis)
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (ts *TeamixServer) handleNotificationCreate(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		ToUser      string `json:"toUser"`
		Message     string `json:"message"`
		FileChanged string `json:"fileChanged,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ToUser == "" || body.Message == "" {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	noti := notification{
		ID:          fmt.Sprintf("n%d", time.Now().UnixNano()),
		FromUser:    u.name,
		ToUser:      body.ToUser,
		Message:     body.Message,
		FileChanged: body.FileChanged,
		Read:        false,
		Time:        time.Now(),
	}
	notis := ts.loadNotifications(body.ToUser)
	if len(notis) > 100 {
		notis = notis[len(notis)-100:]
	}
	notis = append(notis, noti)
	ts.saveNotifications(body.ToUser, notis)
	writeJSON(w, map[string]bool{"ok": true})
}
func (ts *TeamixServer) handleDeleteSession(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `//\\`) {
		http.Error(w, "invalid session name", http.StatusBadRequest)
		return
	}
	dir := u.ctrl.SessionDir()
	if dir == "" {
		http.Error(w, "sessions disabled", http.StatusBadRequest)
		return
	}
	target := filepath.Join(dir, name+".jsonl")
	absDir, _ := filepath.Abs(dir)
	absTarget, _ := filepath.Abs(target)
	if !strings.HasPrefix(absTarget, absDir+string(os.PathSeparator)) && absTarget != absDir {
		http.Error(w, "invalid session path", http.StatusForbidden)
		return
	}
	if err := os.Remove(target); err != nil {
		if os.IsNotExist(err) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── GET handlers ──

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
		Turns int `json:"turns,omitempty"`
		Current bool `json:"current,omitempty"`
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
		Active bool `json:"active,omitempty"`
		Default bool `json:"default,omitempty"`
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
	writeJSON(w, map[string]any{"current": current, "label": label, "default": cfg.DefaultModel, "models": out})
}

func (ts *TeamixServer) handleCheckpoints(w http.ResponseWriter, r *http.Request, u *userSession) {
	cps := u.ctrl.Checkpoints()
	writeJSON(w, cps)
}

func (ts *TeamixServer) handleBranches(w http.ResponseWriter, r *http.Request, u *userSession) {
	branches, err := u.ctrl.Branches()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tree := u.ctrl.BranchTreeText()
	writeJSON(w, map[string]any{"branches": branches, "tree": tree})
}

func (ts *TeamixServer) handleSkills(w http.ResponseWriter, r *http.Request, u *userSession) {
	writeJSON(w, []any{})
}

func (ts *TeamixServer) handleTodos(w http.ResponseWriter, r *http.Request, u *userSession) {
	writeJSON(w, []any{})
}

// switchModel rebuilds a user's controller with a different model.
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
		Model:      ref,
		RequireKey: true,
		Sink:       bc,
		Stderr:     os.Stderr,
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

// sessionTitle generates a short title for a session.
func sessionTitle(ctrl control.SessionAPI, name, firstMsg string) string {
	if firstMsg != "" {
		if len(firstMsg) > 60 {
			return firstMsg[:60] + "…"
		}
		return firstMsg
	}
	return strings.TrimSuffix(name, ".jsonl")
}

// currentModelRef returns the model reference string for display.
func teamixCurrentModelRef(c control.SessionAPI) string {
	ref := strings.TrimSpace(c.ModelRef())
	if ref != "" {
		return ref
	}
	return strings.TrimSpace(c.Label())
}

// ── Capabilities & Secrets handlers ──

// isArchitect checks whether the user is listed in the architects config.
func (ts *TeamixServer) isArchitect(u *userSession) bool {
	for _, a := range ts.getArchitects() {
		if a == u.name {
			return true
		}
	}
	return false
}

func (ts *TeamixServer) handleCapabilitiesGet(w http.ResponseWriter, r *http.Request, u *userSession) {
	writeJSON(w, ts.capCfg)
}

func (ts *TeamixServer) handleCapabilitiesSave(w http.ResponseWriter, r *http.Request, u *userSession) {
	if !ts.isArchitect(u) {
		http.Error(w, `{"error":"forbidden","message":"only architects can modify capabilities"}`, http.StatusForbidden)
		return
	}
	var body struct {
		Kind string          `json:"kind"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Kind == "" {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	var err error
	switch body.Kind {
	case "mcp":
		var cfg capabilities.MCPConfig
		if e := json.Unmarshal(body.Data, &cfg); e == nil {
			err = capabilities.SaveMCP(ts.workspaceRoot, &cfg)
		}
	case "soul":
		var cfg capabilities.SoulConfig
		if e := json.Unmarshal(body.Data, &cfg); e == nil {
			err = capabilities.SaveSoul(ts.workspaceRoot, &cfg)
		}
	case "skills":
		var cfg capabilities.SkillsConfig
		if e := json.Unmarshal(body.Data, &cfg); e == nil {
			err = capabilities.SaveSkills(ts.workspaceRoot, &cfg)
		}
	case "gateway":
		var cfg capabilities.GatewayConfig
		if e := json.Unmarshal(body.Data, &cfg); e == nil {
			err = capabilities.SaveGateway(ts.workspaceRoot, &cfg)
		}
	default:
		http.Error(w, `{"error":"unknown kind"}`, http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	// Reload in-memory config
	ts.capCfg = capabilities.LoadAll(ts.workspaceRoot)
	writeJSON(w, map[string]bool{"ok": true})
}

func (ts *TeamixServer) handleSecretsStatus(w http.ResponseWriter, r *http.Request, u *userSession) {
	keys := ts.keyPool.List()
	enabled := make(map[string]bool)
	for _, k := range keys {
		enabled[k.EnvName] = true
	}
	writeJSON(w, map[string]any{
		"strategy": ts.keyPool.Strategy(),
		"keys":     enabled,
		"keyList":  keys,
		"target":   ts.keyPool.TargetEnv(),
	})
}

func (ts *TeamixServer) handleSecretsSet(w http.ResponseWriter, r *http.Request, u *userSession) {
	if !ts.isArchitect(u) {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		return
	}
	var body struct {
		EnvName string `json:"envName"`
		Value   string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Value == "" {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	if body.EnvName == "" {
		body.EnvName = ts.keyPool.TargetEnv() + "_" + fmt.Sprintf("%d", len(ts.keyPool.List())+1)
	}

	// Check if this env name already exists → update value, else add new
	updated := ts.keyPool.SetKeyValue(body.EnvName, body.Value)
	if updated {
		ts.keyPool.SetKeyEnabled(body.EnvName, true)
	} else {
		ts.keyPool.AddKey(body.EnvName, body.Value)
	}

	// Persist
	if err := ts.keyPool.Save(ts.workspaceRoot); err != nil {
		slog.Error("keypool: save failed", "err", err)
	}
	slog.Info("keypool: key configured", "env", body.EnvName, "by", u.name)
	writeJSON(w, map[string]bool{"ok": true})
}

func (ts *TeamixServer) handleSecretsDelete(w http.ResponseWriter, r *http.Request, u *userSession) {
	if !ts.isArchitect(u) {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		return
	}
	var body struct {
		EnvName string `json:"envName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.EnvName == "" {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	if removed := ts.keyPool.RemoveKey(body.EnvName); !removed {
		http.Error(w, `{"error":"key not found"}`, http.StatusNotFound)
		return
	}
	ts.keyPool.Save(ts.workspaceRoot)
	slog.Info("keypool: key removed", "env", body.EnvName, "by", u.name)
	writeJSON(w, map[string]bool{"ok": true})
}

func (ts *TeamixServer) handleKeyPoolStrategy(w http.ResponseWriter, r *http.Request, u *userSession) {
	if !ts.isArchitect(u) {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		return
	}
	var body struct {
		Strategy string `json:"strategy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Strategy == "" {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	ts.keyPool.SetStrategy(keypool.Strategy(body.Strategy))
	ts.keyPool.Save(ts.workspaceRoot)
	writeJSON(w, map[string]bool{"ok": true})
}

// ── Capabilities handlers (via Controller) ──

func (ts *TeamixServer) handleMCPServers(w http.ResponseWriter, r *http.Request, u *userSession) {
	type toolInfo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	type serverView struct {
		Name      string     `json:"name"`
		Transport string     `json:"transport"`
		Tools     int        `json:"tools"`
		ToolList  []toolInfo `json:"toolList"`
		Status    string     `json:"status"`
		Error     string     `json:"error"`
	}
	var out []serverView
	host := u.ctrl.Host()
	if host != nil {
		for _, s := range host.Servers() {
			var tools []toolInfo
			for _, t := range s.ToolList {
				tools = append(tools, toolInfo{Name: t.Name, Description: t.Description})
			}
			out = append(out, serverView{
				Name: s.Name, Transport: s.Transport, Tools: s.Tools, ToolList: tools, Status: "connected",
			})
		}
		for _, f := range host.Failures() {
			out = append(out, serverView{
				Name: f.Name, Transport: f.Transport, Status: "failed", Error: f.Error,
			})
		}
	}
	if out == nil {
		out = []serverView{}
	}
	writeJSON(w, out)
}

func (ts *TeamixServer) handleSkillsList(w http.ResponseWriter, r *http.Request, u *userSession) {
	type skillView struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
		Scope       string `json:"scope"`
		Description string `json:"description"`
	}
	var out []skillView
	for _, s := range u.ctrl.AllSkills() {
		out = append(out, skillView{
			Name: s.Name, Enabled: u.ctrl.SkillEnabled(s.Name), Scope: string(s.Scope), Description: s.Description,
		})
	}
	if out == nil {
		out = []skillView{}
	}
	writeJSON(w, out)
}

func (ts *TeamixServer) handleMCPAdd(w http.ResponseWriter, r *http.Request, u *userSession) {
	if !ts.isArchitect(u) {
		http.Error(w, `{""error":"forbidden"}"`, http.StatusForbidden)
		return
	}
	var body struct {
		Name      string `json:"name"`
		Command   string `json:"command"`
		Args      []string `json:"args"`
		Transport string `json:"transport"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	_, err := u.ctrl.AddMCPServer(config.PluginEntry{
		Name: body.Name,
		Type: body.Transport,
		Command: body.Command,
		Args: body.Args,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (ts *TeamixServer) handleMCPRemove(w http.ResponseWriter, r *http.Request, u *userSession) {
	if !ts.isArchitect(u) {
		http.Error(w, `{""error":"forbidden"}"`, http.StatusForbidden)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	if removed := u.ctrl.DisconnectMCPServer(body.Name); !removed {
		// Also try removing from config
		/* remove handled by DisconnectMCPServer */
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (ts *TeamixServer) handleSkillToggle(w http.ResponseWriter, r *http.Request, u *userSession) {
	if !ts.isArchitect(u) {
		http.Error(w, `{""error":"forbidden"}"`, http.StatusForbidden)
		return
	}
	var body struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	if err := u.ctrl.SetSkillEnabled(body.Name, body.Enabled); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// ── Memory handlers ──

func (ts *TeamixServer) handleMemoryList(w http.ResponseWriter, r *http.Request, u *userSession) {
	memSet := u.ctrl.Memory()
	type memEntry struct {
		Name        string `json:"name"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Type        string `json:"type"`
		Body        string `json:"body"`
	}
	var out []memEntry
	if memSet != nil && memSet.Store.Dir != "" {
		entries, err := os.ReadDir(memSet.Store.Dir)
		if err == nil {
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
					continue
				}
				name := strings.TrimSuffix(e.Name(), ".md")
				data, err := os.ReadFile(filepath.Join(memSet.Store.Dir, e.Name()))
				if err != nil {
					continue
				}
				text := string(data)
				title := name
				desc := ""
				typ := "user"
				body := text
				// Parse frontmatter if present
				if strings.HasPrefix(text, "---") {
					parts := strings.SplitN(text[3:], "---", 2)
					if len(parts) == 2 {
						fm := parts[0]
						body = strings.TrimSpace(parts[1])
						for _, line := range strings.Split(fm, "\n") {
							line = strings.TrimSpace(line)
							if strings.HasPrefix(line, "title:") {
								title = strings.TrimSpace(line[6:])
							} else if strings.HasPrefix(line, "description:") {
								desc = strings.TrimSpace(line[12:])
							} else if strings.HasPrefix(line, "type:") {
								typ = strings.TrimSpace(line[5:])
							}
						}
					}
				}
				out = append(out, memEntry{Name: name, Title: title, Description: desc, Type: typ, Body: body})
			}
		}
	}
	if out == nil {
		out = []memEntry{}
	}
	writeJSON(w, map[string]any{"memories": out, "dir": memSet.Store.Dir})
}

func (ts *TeamixServer) handleMemorySave(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Name        string `json:"name"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Type        string `json:"type"`
		Body        string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	// Build frontmatter
	fm := "---\n"
	fm += "title: " + body.Title + "\n"
	if body.Description != "" {
		fm += "description: " + body.Description + "\n"
	}
	if body.Type != "" {
		fm += "type: " + body.Type + "\n"
	}
	fm += "---\n"
	fullContent := fm + body.Body

	// Get the Store dir from the Controller
	memSet := u.ctrl.Memory()
	if memSet == nil || memSet.Store.Dir == "" {
		http.Error(w, `{"error":"memory store not available"}`, http.StatusInternalServerError)
		return
	}
	outPath := filepath.Join(memSet.Store.Dir, memory.Slug(body.Name)+".md")
	if err := os.WriteFile(outPath, []byte(fullContent), 0644); err != nil {
		http.Error(w, `{"error":"write failed"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "name": body.Name, "path": outPath})
}

// ── Helpers ──

