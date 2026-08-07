package serve

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"reasonix/internal/config"
	"reasonix/internal/keypool"
	"reasonix/internal/plugin"
	"reasonix/internal/skill"
	"reasonix/internal/teamixconfig"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Configuration, MCP, Skills, and Secrets management handlers.

// GET /teamix/sensitive 返回当前生效的机密清单（dirs/files，所有人可读）。
func (ts *TeamixServer) handleSensitiveGet(w http.ResponseWriter, _ *http.Request, _ *userSession) {
	dirs, files := []string{}, []string{}
	if ts.globalCfg != nil && ts.globalCfg.Config != nil {
		dirs, files = ts.globalCfg.Config.Sensitive.Dirs, ts.globalCfg.Config.Sensitive.Files
	}
	if dirs == nil {
		dirs = []string{}
	}
	if files == nil {
		files = []string{}
	}
	writeJSON(w, map[string]any{"dirs": dirs, "files": files})
}

// POST /teamix/sensitive {dirs, files} 保存机密清单（仅 architect）。
// 写入独立文件 .teamix/sensitive.yaml（不碰 config.yaml 其他段），
// 下次会话构建热加载生效（currentSensitiveRules 每次 Build 重读）。
func (ts *TeamixServer) handleSensitiveSet(w http.ResponseWriter, r *http.Request, u *userSession) {
	if !ts.isArchitect(u) {
		http.Error(w, "仅架构师可修改机密清单", http.StatusForbidden)
		return
	}
	var body struct {
		Dirs  []string `json:"dirs"`
		Files []string `json:"files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad body: "+err.Error(), http.StatusBadRequest)
		return
	}
	// 服务端校验（与前端一致）：去空白、去空条目、去注释——空字符串条目会让
	// 前缀匹配恒真（HasPrefix(p,"")==true），导致全部文件工具被拦（功能瘫痪）。
	sc := teamixconfig.SensitiveConfig{
		Dirs:  cleanSensitiveList(body.Dirs),
		Files: cleanSensitiveList(body.Files),
	}
	if err := teamixconfig.SaveSensitiveOverlay(ts.workspaceRoot, sc); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// 即时更新内存快照（新会话构建时 currentSensitiveRules 重读 Load 也会带上）
	if ts.globalCfg != nil && ts.globalCfg.Config != nil {
		ts.globalCfg.Config.Sensitive = sc
	}
	slog.Info("teamix: sensitive list updated", "by", u.name, "dirs", len(sc.Dirs), "files", len(sc.Files))
	writeJSON(w, map[string]any{"ok": true, "dirs": sc.Dirs, "files": sc.Files})
}

// cleanSensitiveList 过滤空白/注释条目，防止空串进机密清单。
func cleanSensitiveList(items []string) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		it = strings.TrimSpace(it)
		if it == "" || strings.HasPrefix(it, "#") {
			continue
		}
		out = append(out, it)
	}
	return out
}

// globalSoulPath 返回全局 AI 人格（Soul）配置文件路径。
// 仿 mcp.json：全局单文件，架构师维护，全员继承（用户私有 soul 可覆盖）。
func globalSoulPath(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, ".reasonix", "soul.yaml")
}

// userSoulPath 返回用户私有 AI 人格配置文件路径。
// 与 mcp-private.json 对称：users/<name>/.reasonix/soul.yaml，本人 CRUD，覆盖全局。
func userSoulPath(userRoot string) string {
	return filepath.Join(userRoot, ".reasonix", "soul.yaml")
}

// Persona 描述一个可命名的人格（system prompt 模板）。
type Persona struct {
	Name         string `yaml:"name"         json:"name"`
	SystemPrompt string `yaml:"system_prompt" json:"systemPrompt"`
	Active       bool   `yaml:"active"       json:"active"`
}

// SoulConfig 描述一层（全局/私有）人格列表。
type SoulConfig struct {
	Personas []Persona `yaml:"personas" json:"personas"`
	// UseGlobal 仅私有层使用：用户指定使用全局人格列表中的哪一个人格
	// （普通用户不能改全局列表，只能从中选择一个自己生效的）。
	UseGlobal string `yaml:"use_global,omitempty" json:"useGlobal,omitempty"`
}

// loadSoulFile 读取指定路径的 soul.yaml；文件缺失或为空时返回空列表。
func loadSoulFile(path string) SoulConfig {
	var s SoulConfig
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	_ = yaml.Unmarshal(data, &s)
	if s.Personas == nil {
		s.Personas = []Persona{}
	}
	return s
}

// loadGlobalSoul 读取全局 soul.yaml。
func loadGlobalSoul(workspaceRoot string) SoulConfig {
	return loadSoulFile(globalSoulPath(workspaceRoot))
}

// loadUserSoul 读取用户私有 soul.yaml。
func loadUserSoul(userRoot string) SoulConfig {
	return loadSoulFile(userSoulPath(userRoot))
}

// activePersona 返回该层当前生效的人格（有 active 标记的那条）；无则返回 nil。
// 注意：不兜底取第一条——没有 active 标记意味着该层没有显式生效项，
// 应交给上层逻辑（useGlobal / 全局默认）决定，避免"自动生效"歧义。
func activePersona(cfg SoulConfig) *Persona {
	if len(cfg.Personas) == 0 {
		return nil
	}
	for i := range cfg.Personas {
		if cfg.Personas[i].Active {
			return &cfg.Personas[i]
		}
	}
	return nil
}

// effectiveSoul 返回用户实际生效的人格，优先级（均为个人选择，无全局强制）：
//  1. 私有人格列表中的 active
//  2. 私有层 useGlobal 指定的全局人格
//  3. 无 → 返回 nil（用 Reasonix 原生默认提示词）
func effectiveSoul(workspaceRoot, userRoot string) *Persona {
	userCfg := loadUserSoul(userRoot)
	if p := activePersona(userCfg); p != nil {
		return p
	}
	if userCfg.UseGlobal != "" {
		return findPersona(loadGlobalSoul(workspaceRoot), userCfg.UseGlobal)
	}
	return nil
}

// applySoulToSession 将用户当前生效人格的 system_prompt 热更新到其运行中的
// Controller，使下一次会话（NewSession）立即使用新人格，无需重新登录。
func (ts *TeamixServer) applySoulToSession(u *userSession) {
	p := effectiveSoul(ts.workspaceRoot, u.userRoot)
	prompt := ""
	if p != nil {
		prompt = p.SystemPrompt
	}
	u.ctrl.SetSystemPrompt(prompt)
}

// findPersona 在列表中按 name 查找人格。
func findPersona(cfg SoulConfig, name string) *Persona {
	for i := range cfg.Personas {
		if cfg.Personas[i].Name == name {
			return &cfg.Personas[i]
		}
	}
	return nil
}

// writeSoulFile 持久化一层人格列表；列表与 useGlobal 都为空则删除文件（恢复默认）。
func writeSoulFile(path string, cfg SoulConfig) error {
	if len(cfg.Personas) == 0 && cfg.UseGlobal == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// upsertPersona 在列表中新增或按 name 更新人格；activate=true 时同时设为当前生效。
// 新增的人格默认不生效，需用户显式"设为当前"。
func upsertPersona(list []Persona, name, prompt string, activate bool) []Persona {
	for i := range list {
		if list[i].Name == name {
			list[i].SystemPrompt = prompt
			if activate {
				for j := range list {
					list[j].Active = j == i
				}
			}
			return list
		}
	}
	return append(list, Persona{Name: name, SystemPrompt: prompt, Active: activate})
}

// removePersona 按 name 删除人格；删除当前生效项后，本层生效状态交由上层
// （useGlobal / 全局默认）决定，不再自动接替（保持"显式选择"语义）。
func removePersona(list []Persona, name string) []Persona {
	out := make([]Persona, 0, len(list))
	for _, p := range list {
		if p.Name == name {
			continue
		}
		out = append(out, p)
	}
	return out
}

// setActivePersona 将指定 name 设为本层当前生效；不存在则报错。
func setActivePersona(list []Persona, name string) error {
	for i := range list {
		if list[i].Name == name {
			for j := range list {
				list[j].Active = j == i
			}
			return nil
		}
	}
	return fmt.Errorf("persona %q not found", name)
}

// clearAllActive 清除本层所有 active 标记（用于互斥：选全局人格时取消私人生效）。
func clearAllActive(list []Persona) {
	for i := range list {
		list[i].Active = false
	}
}

func (ts *TeamixServer) handleSoulGet(w http.ResponseWriter, r *http.Request, u *userSession) {
	writeJSON(w, map[string]any{
		"global":  loadGlobalSoul(ts.workspaceRoot),
		"private": loadUserSoul(u.userRoot),
	})
}

// soulSave 处理新增/更新人格。
func (ts *TeamixServer) handleSoulSave(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Scope        string `json:"scope"`        // "global" | "private"（默认 private）
		Name         string `json:"name"`         // 人格名（必填）
		SystemPrompt string `json:"systemPrompt"` // 可为空（仅命名占位）
		Activate     bool   `json:"activate"`     // 保存后设为当前生效
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if body.Scope != "" && body.Scope != "global" && body.Scope != "private" {
		http.Error(w, `{"error":"invalid scope, expect private|global"}`, http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}
	if body.Scope == "global" {
		if !ts.isArchitect(u) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		// 全局人格是候选池：无 active 概念（activate 参数忽略），
		// 保存/编辑不影响任何用户 toml（个人 useGlobal 在 effectiveSoul 时动态解析）。
		cfg := loadGlobalSoul(ts.workspaceRoot)
		cfg.Personas = upsertPersona(cfg.Personas, name, body.SystemPrompt, false)
		if err := writeSoulFile(globalSoulPath(ts.workspaceRoot), cfg); err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}
		slog.Info("teamix: global soul saved", "by", u.name, "name", name)
		writeJSON(w, map[string]any{"ok": true, "scope": "global", "name": name})
		return
	}
	cfg := loadUserSoul(u.userRoot)
	cfg.Personas = upsertPersona(cfg.Personas, name, body.SystemPrompt, body.Activate)
	if err := writeSoulFile(userSoulPath(u.userRoot), cfg); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	ts.regenerateUserTOML(u.userRoot)
	ts.applySoulToSession(u)
	slog.Info("teamix: private soul saved", "user", u.name, "name", name)
	writeJSON(w, map[string]any{"ok": true, "scope": "private", "name": name})
}

// handleSoulDelete 删除一个命名人格。
func (ts *TeamixServer) handleSoulDelete(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Scope string `json:"scope"` // "global" | "private"（默认 private）
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if body.Name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}
	if body.Scope == "global" {
		if !ts.isArchitect(u) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		cfg := loadGlobalSoul(ts.workspaceRoot)
		cfg.Personas = removePersona(cfg.Personas, body.Name)
		if err := writeSoulFile(globalSoulPath(ts.workspaceRoot), cfg); err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}
		// 引用该全局人格的用户 useGlobal 仍指向已删条目 → effectiveSoul 返回 nil（回落默认），无需重写 toml
		// 但运行中的会话要刷新：若当前用户正 useGlobal 指向它，立即回落默认提示词
		ts.applySoulToSession(u)
		writeJSON(w, map[string]bool{"ok": true})
		return
	}
	cfg := loadUserSoul(u.userRoot)
	cfg.Personas = removePersona(cfg.Personas, body.Name)
	if err := writeSoulFile(userSoulPath(u.userRoot), cfg); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	ts.regenerateUserTOML(u.userRoot)
	ts.applySoulToSession(u)
	writeJSON(w, map[string]bool{"ok": true})
}

// handleSoulUse 普通用户从全局人格列表中选择一个作为自己生效的（不修改全局列表）。
// 写入私有层 useGlobal 字段；仅当用户没有私有人格时生效（私有优先）。
func (ts *TeamixServer) handleSoulUse(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		// 空 name = 取消选择，恢复跟随全局默认
		cfg := loadUserSoul(u.userRoot)
		cfg.UseGlobal = ""
		if err := writeSoulFile(userSoulPath(u.userRoot), cfg); err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}
		ts.regenerateUserTOML(u.userRoot)
		ts.applySoulToSession(u)
		writeJSON(w, map[string]bool{"ok": true})
		return
	}
	// 校验该人格确实存在于全局列表
	if findPersona(loadGlobalSoul(ts.workspaceRoot), name) == nil {
		http.Error(w, `{"error":"global persona not found"}`, http.StatusNotFound)
		return
	}
	cfg := loadUserSoul(u.userRoot)
	cfg.UseGlobal = name
	// 互斥：选了全局人格，取消所有私有人格的生效标记
	clearAllActive(cfg.Personas)
	if err := writeSoulFile(userSoulPath(u.userRoot), cfg); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	ts.regenerateUserTOML(u.userRoot)
	ts.applySoulToSession(u)
	slog.Info("teamix: user selects global persona", "user", u.name, "name", name)
	writeJSON(w, map[string]bool{"ok": true})
}

// handleSoulActivate 将指定人格设为本层当前生效。
func (ts *TeamixServer) handleSoulActivate(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Scope string `json:"scope"` // "global" | "private"（默认 private）
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if body.Name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}
	if body.Scope == "global" {
		// 全局人格"设为当前" = 个人选择（写自己的 useGlobal），任何人可操作；
		// 全局人格本身是架构师提供的候选池，没有"强制生效"概念。
		if findPersona(loadGlobalSoul(ts.workspaceRoot), body.Name) == nil {
			http.Error(w, `{"error":"global persona not found"}`, http.StatusNotFound)
			return
		}
		cfg := loadUserSoul(u.userRoot)
		cfg.UseGlobal = body.Name
		clearAllActive(cfg.Personas)
		if err := writeSoulFile(userSoulPath(u.userRoot), cfg); err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}
		ts.regenerateUserTOML(u.userRoot)
		ts.applySoulToSession(u)
		writeJSON(w, map[string]bool{"ok": true})
		return
	}
	cfg := loadUserSoul(u.userRoot)
	if err := setActivePersona(cfg.Personas, body.Name); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusNotFound)
		return
	}
	// 互斥：激活了私有人格，清除之前选择的全局人格（useGlobal）
	cfg.UseGlobal = ""
	if err := writeSoulFile(userSoulPath(u.userRoot), cfg); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	ts.regenerateUserTOML(u.userRoot)
	ts.applySoulToSession(u)
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
	ts.refreshKeyPoolProvider()
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
	ts.refreshKeyPoolProvider()
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
	ts.refreshKeyPoolProvider()
	writeJSON(w, map[string]bool{"ok": true})
}

func (ts *TeamixServer) handleMCPServers(w http.ResponseWriter, r *http.Request, u *userSession) {
	type toolInfo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	type serverView struct {
		Name        string     `json:"name"`
		Transport   string     `json:"transport"`
		Tools       int        `json:"tools"`
		ToolList    []toolInfo `json:"toolList"`
		Status      string     `json:"status"`
		Error       string     `json:"error"`
		Source      string     `json:"source"`      // "global" | "user"
		Sensitivity string     `json:"sensitivity"` // 数据源敏感级声明（空 = 未声明 → internal 兜底）
		Enabled     bool       `json:"enabled"`     // 启用/禁用开关
	}
	// 真实来源：name 在公共 mcp.json 中 → global，否则 user（私有）。
	global := ts.loadGlobalMCPServers()
	private := loadUserMCPServers(u.userRoot)
	sensByServer := ts.mcpSensitivityMap(u.userRoot)
	// 配置里存在该 server 吗（已移除的跳过——移除后 host 仍保留断开记录，
	// 但配置已删，下次会话不加载；列表不再展示"僵尸"条目）
	inConfig := func(name string) bool {
		if _, ok := global[name]; ok {
			return true
		}
		_, ok := private[name]
		return ok
	}
	// enabled 从配置读取（nil/true = 启用，false = 禁用）
	enabledOf := func(name string) bool {
		if s, ok := global[name]; ok {
			return mcpEnabled(s)
		}
		if s, ok := private[name]; ok {
			return mcpEnabled(s)
		}
		return true
	}
	srcOf := func(name string) string {
		if _, isGlobal := global[name]; isGlobal {
			return "global"
		}
		return "user"
	}
	transportOf := func(name string) string {
		if s, ok := global[name]; ok && s.Type != "" {
			return s.Type
		}
		if s, ok := private[name]; ok && s.Type != "" {
			return s.Type
		}
		return "stdio"
	}
	var out []serverView
	seen := map[string]bool{}
	addView := func(v serverView) {
		if seen[v.Name] {
			return
		}
		seen[v.Name] = true
		out = append(out, v)
	}
	host := u.ctrl.Host()
	if host != nil {
		// 1) 已连接
		for _, s := range host.Servers() {
			if !inConfig(s.Name) {
				continue // 已移除：不显示（等会话重建清理）
			}
			var tools []toolInfo
			for _, t := range s.ToolList {
				tools = append(tools, toolInfo{Name: t.Name, Description: t.Description})
			}
			addView(serverView{
				Name: s.Name, Transport: s.Transport, Tools: s.Tools, ToolList: tools, Status: "connected", Source: srcOf(s.Name),
				Sensitivity: string(sensByServer[s.Name]), Enabled: enabledOf(s.Name),
			})
		}
		// 2) 连接测试中（启动握手在途）
		for _, name := range host.ConnectingServers() {
			if !inConfig(name) {
				continue
			}
			addView(serverView{
				Name: name, Transport: transportOf(name), Status: "initializing", Source: srcOf(name),
				Sensitivity: string(sensByServer[name]), Enabled: enabledOf(name),
			})
		}
		// 3) 连接失败（必须先于配置兜底，否则失败记录会被 seen 屏蔽、永远显示 testing）
		for _, f := range host.Failures() {
			if !inConfig(f.Name) {
				continue // 已移除：不显示
			}
			addView(serverView{
				Name: f.Name, Transport: f.Transport, Status: "failed", Error: f.Error, Source: srcOf(f.Name),
				Sensitivity: string(sensByServer[f.Name]), Enabled: enabledOf(f.Name),
			})
		}
	}
	// 4) 配置已保存但尚无运行状态（后台连接即将开始/排队）——保证
	// "点击添加立即显示"，哪怕参数是 123 也会立刻出现在列表里。
	// 已禁用（enabled=false）的 server 加载路径会跳过、永不连接 → 显示 disabled。
	names := make([]string, 0, len(global)+len(private))
	for name := range global {
		names = append(names, name)
	}
	for name := range private {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if seen[name] {
			continue
		}
		status := "initializing"
		if !enabledOf(name) {
			status = "disabled"
		}
		addView(serverView{
			Name: name, Transport: transportOf(name), Status: status, Source: srcOf(name),
			Sensitivity: string(sensByServer[name]), Enabled: enabledOf(name),
		})
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
		Sensitivity string `json:"sensitivity"` // 数据源敏感级声明（空 = 未声明）
		Source      string `json:"source"`      // "global" | "user"
		mod         int64  `json:"-"`           // 文件修改时间，用于排序
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
			Sensitivity: s.Sensitivity,
			mod:         mod,
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
		Name        string          `json:"name"`
		Command     string          `json:"command"`
		Args        json.RawMessage `json:"args"` // string 或 []string 均兼容
		Transport   string          `json:"transport"`
		Scope       string          `json:"scope"`       // "private" (default) | "global"
		Sensitivity string          `json:"sensitivity"` // public/internal/redact/confidential
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if body.Command == "" {
		http.Error(w, `{"error":"MCP 需要启动命令（command）"}`, http.StatusBadRequest)
		return
	}
	if !safeTokenName(body.Name) {
		http.Error(w, `{"error":"invalid MCP name (letters, digits, _ - . only)"}`, http.StatusBadRequest)
		return
	}
	args := parseMCPArgs(body.Args)
	entry := config.PluginEntry{Name: body.Name, Type: body.Transport, Command: body.Command, Args: args}
	if body.Scope == "global" {
		if !ts.isArchitect(u) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if err := ts.saveGlobalMCP(body.Name, body.Command, args, body.Transport, body.Sensitivity); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Broadcast to all online users（异步：逐个会话连接可能耗时，不阻塞添加返回）
		go ts.broadcastMCP(entry)
		// 连接测试放后台：立即返回，列表先显示"正在测试连接"，
		// 失败通过 host.RecordFailure 落 failed 状态供列表/前端轮询展示。
		go ts.connectMCPAsync(u, entry)
		writeJSON(w, map[string]any{"ok": true, "scope": "global"})
		return
	}
	// Private scope (default)
	if body.Scope != "" && body.Scope != "private" {
		http.Error(w, `{"error":"invalid scope, expect private|global"}`, http.StatusBadRequest)
		return
	}
	// Persist private MCP into users/<name>/.teamix/config.yaml so it survives restart.
	if err := ts.saveUserMCP(u, body.Name, body.Command, args, body.Transport, body.Sensitivity); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	go ts.connectMCPAsync(u, entry)
	writeJSON(w, map[string]any{"ok": true, "scope": "private"})
}

// connectMCPAsync 后台连接 MCP server，不阻塞 HTTP 响应（首次 npx 下载/启动
// 可能耗时数十秒）。连接失败记录到 host.failures，前端轮询列表即可看到
// failed + 具体错误，而不是添加请求一直挂着。
func (ts *TeamixServer) connectMCPAsync(u *userSession, entry config.PluginEntry) {
	if _, err := u.ctrl.ConnectMCPServer(entry); err != nil {
		if h := u.ctrl.Host(); h != nil {
			h.RecordFailure(plugin.Spec{
				Name: entry.Name, Type: entry.Type, Command: entry.Command, Args: entry.Args,
			}, err)
		}
		return
	}
	// 连接期间用户可能已把开关拨到禁用（toggle 断开了本会话）——
	// 若配置已禁用，把刚连上的 server 断开，避免"开关关着却显示已连接"。
	if !ts.mcpEnabledByConfig(u, entry.Name) {
		u.ctrl.DisconnectMCPServer(entry.Name)
	}
}

// mcpEnabledByConfig 按当前配置判断 server 是否启用（nil/true = 启用）。
func (ts *TeamixServer) mcpEnabledByConfig(u *userSession, name string) bool {
	if s, ok := ts.loadGlobalMCPServers()[name]; ok {
		return mcpEnabled(s)
	}
	if s, ok := loadUserMCPServers(u.userRoot)[name]; ok {
		return mcpEnabled(s)
	}
	return true
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
func (ts *TeamixServer) saveUserMCP(u *userSession, name, command string, args []string, transport, sensitivity string) error {
	specs := loadUserMCPServers(u.userRoot)
	specs[name] = mcpServerSpec{Command: command, Args: args, Type: transport, Sensitivity: sensitivity}
	return writeMCPFile(userMCPPath(u.userRoot), specs)
}

// setUserMCPEnabled 更新私有 MCP 的启用开关（保留其他字段）。
func (ts *TeamixServer) setUserMCPEnabled(u *userSession, name string, enabled bool) error {
	specs := loadUserMCPServers(u.userRoot)
	spec, ok := specs[name]
	if !ok {
		return fmt.Errorf("MCP %q 不存在", name)
	}
	e := enabled
	spec.Enabled = &e
	specs[name] = spec
	return writeMCPFile(userMCPPath(u.userRoot), specs)
}

// setGlobalMCPEnabled 更新全局 MCP 的启用开关（保留其他字段）。
func (ts *TeamixServer) setGlobalMCPEnabled(name string, enabled bool) error {
	specs := ts.loadGlobalMCPServers()
	spec, ok := specs[name]
	if !ok {
		return fmt.Errorf("MCP %q 不存在", name)
	}
	e := enabled
	spec.Enabled = &e
	specs[name] = spec
	return ts.writeGlobalMCPServers(specs)
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
	var body struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !ts.isArchitect(u) {
		// 普通用户仅可调整自己的私有 Skill（custom scope）；全局/内置由架构师管理
		allowed := false
		for _, s := range u.ctrl.AllSkills() {
			if s.Name == body.Name && s.Scope == skill.ScopeCustom {
				allowed = true
				break
			}
		}
		if !allowed {
			http.Error(w, "仅架构师可调整全局/内置 Skill", http.StatusForbidden)
			return
		}
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

// writeGlobalMCPServers 把整份全局 MCP 配置写回 .reasonix/mcp.json。
func (ts *TeamixServer) writeGlobalMCPServers(specs map[string]mcpServerSpec) error {
	path := ts.globalMCPPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	doc := struct {
		MCPServers map[string]mcpServerSpec `json:"mcpServers"`
	}{MCPServers: specs}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create global mcp.json: %w", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

// saveGlobalMCP adds an MCP server entry to the global mcp.json file.
func (ts *TeamixServer) saveGlobalMCP(name, command string, args []string, transport, sensitivity string) error {
	specs := ts.loadGlobalMCPServers()
	specs[name] = mcpServerSpec{Command: command, Args: args, Type: transport, Sensitivity: sensitivity}
	return ts.writeGlobalMCPServers(specs)
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
