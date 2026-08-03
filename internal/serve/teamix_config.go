package serve

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	// 真实来源：name 在公共 mcp.json 中 → global，否则 user（私有）。
	global := ts.loadGlobalMCPServers()
	var out []serverView
	host := u.ctrl.Host()
	if host != nil {
		for _, s := range host.Servers() {
			var tools []toolInfo
			for _, t := range s.ToolList {
				tools = append(tools, toolInfo{Name: t.Name, Description: t.Description})
			}
			src := "user"
			if _, isGlobal := global[s.Name]; isGlobal {
				src = "global"
			}
			out = append(out, serverView{
				Name: s.Name, Transport: s.Transport, Tools: s.Tools, ToolList: tools, Status: "connected", Source: src,
			})
		}
		for _, f := range host.Failures() {
			src := "user"
			if _, isGlobal := global[f.Name]; isGlobal {
				src = "global"
			}
			out = append(out, serverView{
				Name: f.Name, Transport: f.Transport, Status: "failed", Error: f.Error, Source: src,
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
		mod         int64  `json:"-"`      // 文件修改时间，用于排序
	}
	var out []skillView
	for _, s := range u.ctrl.AllSkills() {
		var mod int64
		if s.Path != "" && s.Path != "(builtin)" {
			if fi, err := os.Stat(s.Path); err == nil {
				mod = fi.ModTime().Unix()
			}
		}
		out = append(out, skillView{
			Name: s.Name, Enabled: u.ctrl.SkillEnabled(s.Name), Scope: string(s.Scope), Description: s.Description, Source: string(s.Scope),
			mod: mod,
		})
	}
	// 按文件修改时间 旧→新 排序（内置 skill 无文件排最前）：新增 skill 显示在列表末尾。
	sort.SliceStable(out, func(i, j int) bool { return out[i].mod < out[j].mod })
	if out == nil {
		out = []skillView{}
	}
	writeJSON(w, out)
}


func (ts *TeamixServer) handleMCPAdd(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Name      string          `json:"name"`
		Command   string          `json:"command"`
		Args      json.RawMessage `json:"args"` // string 或 []string 均兼容
		Transport string          `json:"transport"`
		Scope     string          `json:"scope"` // "private" (default) | "global"
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !safeTokenName(body.Name) {
		http.Error(w, `{"error":"invalid MCP name (letters, digits, _ - . only)"}`, http.StatusBadRequest)
		return
	}
	args := parseMCPArgs(body.Args)
	if body.Scope == "global" {
		if !ts.isArchitect(u) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if err := ts.saveGlobalMCP(body.Name, body.Command, args, body.Transport); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Broadcast to all online users
		ts.broadcastMCP(config.PluginEntry{
			Name: body.Name, Type: body.Transport, Command: body.Command, Args: args,
		})
		writeJSON(w, map[string]any{"ok": true, "scope": "global"})
		return
	}
	// Private scope (default)
	if body.Scope != "" && body.Scope != "private" {
		http.Error(w, `{"error":"invalid scope, expect private|global"}`, http.StatusBadRequest)
		return
	}
	// Persist private MCP into users/<name>/.teamix/config.yaml so it survives restart.
	if err := ts.saveUserMCP(u, body.Name, body.Command, args, body.Transport); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, err := u.ctrl.AddMCPServer(config.PluginEntry{
		Name: body.Name, Type: body.Transport, Command: body.Command, Args: args,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "scope": "private"})
}

// parseMCPArgs accepts either a JSON string (space-separated) or a JSON array.
func parseMCPArgs(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.Fields(s)
	}
	var arr []string
	if json.Unmarshal(raw, &arr) == nil {
		return arr
	}
	return nil
}


// saveUserMCP upserts a private MCP entry into users/<name>/.reasonix/mcp-private.json.
func (ts *TeamixServer) saveUserMCP(u *userSession, name, command string, args []string, transport string) error {
	specs := loadUserMCPServers(u.userRoot)
	specs[name] = mcpServerSpec{Command: command, Args: args, Type: transport}
	return writeMCPFile(userMCPPath(u.userRoot), specs)
}

// removeUserMCP removes a private MCP entry from users/<name>/.reasonix/mcp-private.json.
func (ts *TeamixServer) removeUserMCP(u *userSession, name string) (bool, error) {
	specs := loadUserMCPServers(u.userRoot)
	if _, ok := specs[name]; !ok {
		return false, nil
	}
	delete(specs, name)
	return true, writeMCPFile(userMCPPath(u.userRoot), specs)
}


func (ts *TeamixServer) handleMCPRemove(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// Global MCP: entry lives in public .reasonix/mcp.json — only architects may remove,
	// and every online user must be disconnected.
	if _, isGlobal := ts.loadGlobalMCPServers()[body.Name]; isGlobal {
		if !ts.isArchitect(u) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if err := ts.removeGlobalMCP(body.Name); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		ts.broadcastMCPRemove(body.Name) // disconnects the caller too
		writeJSON(w, map[string]bool{"ok": true})
		return
	}
	// Private MCP: remove from user config and disconnect own connection.
	if _, err := ts.removeUserMCP(u, body.Name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	u.ctrl.DisconnectMCPServer(body.Name)
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
