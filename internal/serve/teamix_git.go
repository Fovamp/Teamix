package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"reasonix/internal/teamixconfig"
)

func (ts *TeamixServer) handleGitCredentials(w http.ResponseWriter, r *http.Request, u *userSession) {
	uc, err := teamixconfig.LoadUserConfig(u.userRoot)
	if err != nil {
		http.Error(w, `{"error":"failed to load user config"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"sshKeyPath":    uc.Git.SSHKeyPath,
		"httpsUsername": uc.Git.HTTPSUsername,
		"configured":    uc.HasGitCredentials(),
	})
}

func (ts *TeamixServer) handleGitCredentialsSave(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		SSHKeyPath    string `json:"sshKeyPath"`
		HTTPSUsername string `json:"httpsUsername"`
		HTTPSPassword string `json:"httpsPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}

	uc, err := teamixconfig.LoadUserConfig(u.userRoot)
	if err != nil {
		http.Error(w, `{"error":"failed to load user config"}`, http.StatusInternalServerError)
		return
	}

	// Update git config
	if body.SSHKeyPath != "" {
		uc.Git.SSHKeyPath = body.SSHKeyPath
		// Clear HTTPS if switching to SSH
		uc.Git.HTTPSUsername = ""
		uc.Git.HTTPSPassword = ""
	} else if body.HTTPSUsername != "" {
		uc.Git.HTTPSUsername = body.HTTPSUsername
		uc.Git.HTTPSPassword = body.HTTPSPassword
		uc.Git.SSHKeyPath = ""
	}

	// Validate
	if err := uc.ValidateGitCredentials(); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	if err := uc.SaveUserConfig(u.userRoot); err != nil {
		http.Error(w, `{"error":"failed to save user config"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (ts *TeamixServer) handleGitValidate(w http.ResponseWriter, r *http.Request, u *userSession) {
	uc, err := teamixconfig.LoadUserConfig(u.userRoot)
	if err != nil {
		writeJSON(w, map[string]any{"valid": false, "error": "failed to load config"})
		return
	}
	if err := uc.ValidateGitCredentials(); err != nil {
		writeJSON(w, map[string]any{"valid": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"valid": true})
}

func (ts *TeamixServer) handleProjectSelect(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Project string `json:"project"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Project == "" {
		http.Error(w, `{"error":"project name required"}`, http.StatusBadRequest)
		return
	}

	// Validate project exists in global config
	proj := ts.globalCfg.Projects.FindProject(body.Project)
	if proj == nil {
		http.Error(w, `{"error":"project not found"}`, http.StatusNotFound)
		return
	}

	// Check git credentials
	uc, err := teamixconfig.LoadUserConfig(u.userRoot)
	if err != nil {
		http.Error(w, `{"error":"failed to load user config"}`, http.StatusInternalServerError)
		return
	}

	projPath := filepath.Join(u.userRoot, body.Project)

	// If project directory does not exist, clone it
	if _, err := os.Stat(projPath); os.IsNotExist(err) {
		if !uc.HasGitCredentials() {
			writeJSON(w, map[string]any{"ok": false, "needCredentials": true, "error": "git credentials not configured"})
			return
		}
		if err := ts.cloneProject(proj.Git, projPath, uc); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": "clone failed: " + err.Error()})
			return
		}
	}

	// Switch user session to this project
	u.selectedProject = body.Project

	// Switch controller workspace to project directory
	sessionDir := filepath.Join(projPath, ".teamix", "sessions")
	os.MkdirAll(sessionDir, 0o755)
	// Note: full controller workspace switch requires rebuilding;
	// for now, directory-based isolation is sufficient.

	writeJSON(w, map[string]any{
		"ok":      true,
		"project": body.Project,
		"path":    projPath,
		"cloned":  true,
	})
}

func (ts *TeamixServer) cloneProject(gitURL, targetPath string, uc *teamixconfig.UserConfig) error {
	cmd := exec.Command("git", "clone", gitURL, targetPath)

	// Set up git credentials via environment
	if uc.Git.SSHKeyPath != "" {
		cmd.Env = append(os.Environ(),
			"GIT_SSH_COMMAND=ssh -i "+uc.Git.SSHKeyPath+" -o StrictHostKeyChecking=accept-new",
		)
	} else if uc.Git.HTTPSUsername != "" {
		// For HTTPS, we need to embed credentials in the URL
		// Replace https:// with https://user:pass@
		if strings.HasPrefix(gitURL, "https://") {
			authURL := "https://" + uc.Git.HTTPSUsername + ":" + uc.Git.HTTPSPassword + "@" + strings.TrimPrefix(gitURL, "https://")
			cmd = exec.Command("git", "clone", authURL, targetPath)
		}
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return &gitError{msg: strings.TrimSpace(string(output)), err: err}
	}
	return nil
}

type gitError struct {
	msg string
	err error
}

func (e *gitError) Error() string { return e.msg }
