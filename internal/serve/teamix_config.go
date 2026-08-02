package serve

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"reasonix/internal/config"
	"reasonix/internal/capabilities"
	"reasonix/internal/keypool"
)

// Configuration, MCP, Skills, and Secrets management handlers.

func (ts *TeamixServer) handleCapabilitiesGet(w http.ResponseWriter, r *http.Request, u *userSession) {
	writeJSON(w, ts.capCfg)
}


func (ts *TeamixServer) handleCapabilitiesSave(w http.ResponseWriter, r *http.Request, u *userSession) {
	if !ts.isArchitect(u) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var body struct {
		Kind string          `json:"kind"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Kind == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
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
		http.Error(w, "unknown kind", http.StatusBadRequest)
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
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var body struct {
		EnvName string `json:"envName"`
		Value   string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Value == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
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
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var body struct {
		EnvName string `json:"envName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.EnvName == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if removed := ts.keyPool.RemoveKey(body.EnvName); !removed {
		http.Error(w, "key not found", http.StatusNotFound)
		return
	}
	ts.keyPool.Save(ts.workspaceRoot)
	slog.Info("keypool: key removed", "env", body.EnvName, "by", u.name)
	writeJSON(w, map[string]bool{"ok": true})
}


func (ts *TeamixServer) handleKeyPoolStrategy(w http.ResponseWriter, r *http.Request, u *userSession) {
	if !ts.isArchitect(u) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var body struct {
		Strategy string `json:"strategy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Strategy == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ts.keyPool.SetStrategy(keypool.Strategy(body.Strategy))
	ts.keyPool.Save(ts.workspaceRoot)
	writeJSON(w, map[string]bool{"ok": true})
}


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
		Source    string     `json:"source"` // "global" | "user"
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
				Name: s.Name, Transport: s.Transport, Tools: s.Tools, ToolList: tools, Status: "connected", Source: "global",
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
		Name        string `json:"name"`
		Enabled     bool   `json:"enabled"`
		Scope       string `json:"scope"`
		Description string `json:"description"`
		Source      string `json:"source"` // "global" | "user"
	}
	var out []skillView
	for _, s := range u.ctrl.AllSkills() {
		out = append(out, skillView{
			Name: s.Name, Enabled: u.ctrl.SkillEnabled(s.Name), Scope: string(s.Scope), Description: s.Description, Source: string(s.Scope),
		})
	}
	if out == nil {
		out = []skillView{}
	}
	writeJSON(w, out)
}


func (ts *TeamixServer) handleMCPAdd(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Name      string   `json:"name"`
		Command   string   `json:"command"`
		Args      []string `json:"args"`
		Transport string   `json:"transport"`
		Scope     string   `json:"scope"` // "private" (default) | "global"
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if body.Scope == "global" {
		if !ts.isArchitect(u) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if err := ts.saveGlobalMCP(body.Name, body.Command, body.Args, body.Transport); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Broadcast to all online users
		ts.broadcastMCP(config.PluginEntry{
			Name: body.Name, Type: body.Transport, Command: body.Command, Args: body.Args,
		})
		writeJSON(w, map[string]any{"ok": true, "scope": "global"})
		return
	}
	// Private scope (default)
	if body.Scope != "global" && body.Scope != "" && !ts.isArchitect(u) {
		// non-architect can only add private
	}
	_, err := u.ctrl.AddMCPServer(config.PluginEntry{
		Name: body.Name, Type: body.Transport, Command: body.Command, Args: body.Args,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "scope": "private"})
}


func (ts *TeamixServer) handleMCPRemove(w http.ResponseWriter, r *http.Request, u *userSession) {
	if !ts.isArchitect(u) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
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
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var body struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := u.ctrl.SetSkillEnabled(body.Name, body.Enabled); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}



// globalMCPPath returns the path to global/.reasonix/mcp.json
func (ts *TeamixServer) globalMCPPath() string {
	return filepath.Join(ts.workspaceRoot, ".reasonix", "mcp.json")
}

// saveGlobalMCP adds an MCP server entry to the global mcp.json file.
func (ts *TeamixServer) saveGlobalMCP(name, command string, args []string, transport string) error {
	path := ts.globalMCPPath()
	os.MkdirAll(filepath.Dir(path), 0o755)

	// Read existing
	doc := struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
			Type    string   `json:"type"`
		} `json:"mcpServers"`
	}{MCPServers: make(map[string]struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
		Type    string   `json:"type"`
	})}

	if f, err := os.Open(path); err == nil {
		defer f.Close()
		json.NewDecoder(f).Decode(&doc)
	}

	doc.MCPServers[name] = struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
		Type    string   `json:"type"`
	}{Command: command, Args: args, Type: transport}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create global mcp.json: %w", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

// removeGlobalMCP removes an MCP server entry from the global mcp.json file.
func (ts *TeamixServer) removeGlobalMCP(name string) error {
	path := ts.globalMCPPath()
	doc := struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
			Type    string   `json:"type"`
		} `json:"mcpServers"`
	}{MCPServers: make(map[string]struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
		Type    string   `json:"type"`
	})}

	if f, err := os.Open(path); err == nil {
		defer f.Close()
		json.NewDecoder(f).Decode(&doc)
	}
	delete(doc.MCPServers, name)

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create global mcp.json: %w", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

// broadcastMCP sends ConnectMCPServer to all online users.
func (ts *TeamixServer) broadcastMCP(entry config.PluginEntry) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	for _, sess := range ts.sessions {
		if _, err := sess.ctrl.ConnectMCPServer(entry); err != nil {
			slog.Warn("teamix: broadcast MCP failed", "user", sess.name, "name", entry.Name, "err", err)
		}
	}
}

// broadcastMCPRemove disconnects an MCP server from all online users.
func (ts *TeamixServer) broadcastMCPRemove(name string) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	for _, sess := range ts.sessions {
		sess.ctrl.DisconnectMCPServer(name)
	}
}
