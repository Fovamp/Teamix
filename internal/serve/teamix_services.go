package serve

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// runningService tracks a service process started by a user.
type runningService struct {
	ID        string `json:"id"`
	Project   string `json:"project"`
	Service   string `json:"service"`
	Port      int    `json:"port"`
	PID       int    `json:"pid"`
	StartedAt time.Time `json:"startedAt"`
	cmd       *exec.Cmd
}

// serviceManager tracks running services per user.
type serviceManager struct {
	mu       sync.Mutex
	services map[string][]*runningService // token -> services
}

func newServiceManager() *serviceManager {
	return &serviceManager{services: make(map[string][]*runningService)}
}

func (sm *serviceManager) add(token string, svc *runningService) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.services[token] = append(sm.services[token], svc)
}

func (sm *serviceManager) list(token string) []*runningService {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.services[token]
}

func (sm *serviceManager) find(token, id string) *runningService {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	for _, s := range sm.services[token] {
		if s.ID == id {
			return s
		}
	}
	return nil
}

func (sm *serviceManager) remove(token, id string) *runningService {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	list := sm.services[token]
	for i, s := range list {
		if s.ID == id {
			sm.services[token] = append(list[:i], list[i+1:]...)
			return s
		}
	}
	return nil
}

func (sm *serviceManager) cleanupUser(token string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	for _, s := range sm.services[token] {
		sm.killProcess(s)
	}
	delete(sm.services, token)
}

func (sm *serviceManager) killProcess(svc *runningService) {
	if svc.cmd != nil && svc.cmd.Process != nil {
		slog.Info("teamix: stopping service", "id", svc.ID, "pid", svc.PID)
		// Send CTRL_BREAK_EVENT on Windows, SIGTERM on Unix
		if err := svc.cmd.Process.Signal(syscall.SIGTERM); err != nil {
			svc.cmd.Process.Kill()
		}
		// Give it a moment to shut down gracefully
		done := make(chan struct{})
		go func() {
			svc.cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			svc.cmd.Process.Kill()
		}
	}
}

var svcMgr = newServiceManager()

func (ts *TeamixServer) handleServiceStart(w http.ResponseWriter, r *http.Request, u *userSession) {
	projectName := r.PathValue("name")

	var body struct {
		Service string `json:"service"`
		Port    int    `json:"port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Service == "" {
		http.Error(w, `{"error":"service name required"}`, http.StatusBadRequest)
		return
	}

	// Validate project
	proj := ts.globalCfg.Projects.FindProject(projectName)
	if proj == nil {
		http.Error(w, `{"error":"project not found"}`, http.StatusNotFound)
		return
	}

	// Validate service
	svc := proj.FindService(body.Service)
	if svc == nil {
		http.Error(w, `{"error":"service not found in project"}`, http.StatusNotFound)
		return
	}

	// Determine port
	port := body.Port
	if port <= 0 {
		port = svc.Port
	}

	// Project working directory
	projPath := filepath.Join(u.userRoot, projectName)
	svcPath := filepath.Join(projPath, filepath.FromSlash(svc.Dir))

	if _, err := os.Stat(svcPath); os.IsNotExist(err) {
		http.Error(w, `{"error":"project not cloned yet, select project first"}`, http.StatusBadRequest)
		return
	}

	// Build startup command
	startupCmd := svc.Startup
	if port != svc.Port && port > 0 {
		// Inject custom port into startup command
		startupCmd = strings.ReplaceAll(startupCmd,
			fmt.Sprintf(":%d", svc.Port),
			fmt.Sprintf(":%d", port))
	}

	// Execute
	cmd := exec.Command("cmd", "/c", startupCmd)
	cmd.Dir = svcPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"start failed: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	svcID := fmt.Sprintf("%s-%s-%d", projectName, body.Service, cmd.Process.Pid)
	rs := &runningService{
		ID:        svcID,
		Project:   projectName,
		Service:   body.Service,
		Port:      port,
		PID:       cmd.Process.Pid,
		StartedAt: time.Now(),
		cmd:       cmd,
	}
	svcMgr.add(u.token, rs)

	slog.Info("teamix: service started", "user", u.name, "project", projectName,
		"service", body.Service, "pid", cmd.Process.Pid, "port", port)

	// Release process so it runs in background
	go func() {
		cmd.Wait()
		slog.Info("teamix: service exited", "id", svcID)
		svcMgr.remove(u.token, svcID)
	}()

	writeJSON(w, map[string]any{
		"ok":      true,
		"id":      svcID,
		"pid":     cmd.Process.Pid,
		"port":    port,
		"service": body.Service,
	})
}

func (ts *TeamixServer) handleServiceStop(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		http.Error(w, `{"error":"service id required"}`, http.StatusBadRequest)
		return
	}

	svc := svcMgr.find(u.token, body.ID)
	if svc == nil {
		http.Error(w, `{"error":"service not found"}`, http.StatusNotFound)
		return
	}

	svcMgr.killProcess(svc)
	svcMgr.remove(u.token, body.ID)

	writeJSON(w, map[string]bool{"ok": true})
}

func (ts *TeamixServer) handleServicesStatus(w http.ResponseWriter, r *http.Request, u *userSession) {
	list := svcMgr.list(u.token)
	if list == nil {
		list = []*runningService{}
	}
	type svcStatus struct {
		ID        string `json:"id"`
		Project   string `json:"project"`
		Service   string `json:"service"`
		Port      int    `json:"port"`
		PID       int    `json:"pid"`
		StartedAt string `json:"startedAt"`
	}
	out := make([]svcStatus, len(list))
	for i, s := range list {
		out[i] = svcStatus{
			ID: s.ID, Project: s.Project, Service: s.Service,
			Port: s.Port, PID: s.PID,
			StartedAt: s.StartedAt.Format(time.RFC3339),
		}
	}
	writeJSON(w, out)
}
