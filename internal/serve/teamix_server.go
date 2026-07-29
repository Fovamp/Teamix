package serve

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/teamixconfig"
	"reasonix/internal/workflow"
	"reasonix/internal/control"
	"reasonix/internal/capabilities"
	"reasonix/internal/keypool"
)

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


// TeamixServer core types, constructor, and route registration.

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


func teamixGenerateToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		slog.Error("teamix: crypto/rand failed", "err", err)
	}
	return hex.EncodeToString(b)
}


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


func (ts *TeamixServer) withUser(next func(http.ResponseWriter, *http.Request, *userSession)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := ts.extractUser(r)
		if u == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r, u)
	}
}


func (ts *TeamixServer) GetSession(token string) *userSession {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.sessions[token]
}


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


func (ts *TeamixServer) Handler() http.Handler {
	return ts.mux
}


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

	// Vue3 app
	mux.HandleFunc("GET /v3/", ts.handleV3Index)
	mux.HandleFunc("GET /v3/assets/", ts.handleV3Assets)

	return logMiddleware(csrfGuard(mux))
}


func (ts *TeamixServer) handleV3Index(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(v3IndexHTML)
}

func (ts *TeamixServer) handleV3Assets(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	importPath := "webdist-v3" + path
	data, err := v3Assets.ReadFile(importPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ct := "application/octet-stream"
	if len(path) > 3 && path[len(path)-3:] == ".js" {
		ct = "application/javascript"
	} else if len(path) > 4 && path[len(path)-4:] == ".css" {
		ct = "text/css"
	} else if len(path) > 4 && path[len(path)-4:] == ".svg" {
		ct = "image/svg+xml"
	}
	w.Header().Set("Content-Type", ct)
	w.Write(data)
}

func (ts *TeamixServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(webIndexHTML)
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

