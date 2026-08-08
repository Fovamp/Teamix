package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"reasonix/internal/teamixconfig"
)

// nacos 模板配置 API：
//   GET/POST /teamix/nacos        团队级模板（.teamix/config.yaml nacos 段；POST 仅架构师）
//   GET/POST /teamix/nacos/user   用户级覆盖（users/<user>/.teamix/config.yaml nacos 段；本人）
// 生效逻辑见 nacosEnvFor：用户级优先，回落团队级；仅项目配置了 nacos 才注入。

// GET /teamix/nacos 团队级 nacos 模板（所有用户可读，密码不回显）。
func (ts *TeamixServer) handleNacosGet(w http.ResponseWriter, r *http.Request, u *userSession) {
	nc := ts.Nacos()
	if nc.Password != "" {
		nc.Password = "••••••" // 不回显
	}
	writeJSON(w, nc)
}

// POST /teamix/nacos 保存团队级 nacos 模板（仅架构师）。
// password 为空或占位符（未改）时保留原密码（不回显导致前端拿不到真实值）。
func (ts *TeamixServer) handleNacosSave(w http.ResponseWriter, r *http.Request, u *userSession) {
	if !ts.isArchitect(u) {
		http.Error(w, `{"error":"仅架构师可修改团队 nacos 模板"}`, http.StatusForbidden)
		return
	}
	var nc teamixconfig.NacosConfig
	if err := json.NewDecoder(r.Body).Decode(&nc); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	if nc.Password == "" || nc.Password == "••••••" {
		if g := ts.GlobalCfg(); g != nil && g.Config != nil {
			nc.Password = g.Config.Nacos.Password
		}
	}
	path := filepath.Join(ts.workspaceRoot, ".teamix", "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, `{"error":"read config.yaml: `+jsonEscape(err.Error())+`"}`, http.StatusInternalServerError)
		return
	}
	out, err := setNacosInConfig(data, nc)
	if err != nil {
		http.Error(w, `{"error":"parse config.yaml: `+jsonEscape(err.Error())+`"}`, http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		http.Error(w, `{"error":"write config.yaml: `+jsonEscape(err.Error())+`"}`, http.StatusInternalServerError)
		return
	}
	ts.setGlobalCfg(ts.loadGlobalConfig())
	writeJSON(w, map[string]any{"ok": true})
}

// GET /teamix/nacos/user 用户级覆盖（本人；密码不回显）。
func (ts *TeamixServer) handleNacosUserGet(w http.ResponseWriter, r *http.Request, u *userSession) {
	nc := ts.userNacos(u)
	if uc, err := teamixconfig.LoadUserConfig(u.userRoot); err == nil {
		nc = uc.Nacos
	}
	if nc.Password != "" {
		nc.Password = "••••••"
	}
	writeJSON(w, nc)
}

// POST /teamix/nacos/user 保存用户级覆盖（本人；密码未改保留原值）。
func (ts *TeamixServer) handleNacosUserSave(w http.ResponseWriter, r *http.Request, u *userSession) {
	var nc teamixconfig.NacosConfig
	if err := json.NewDecoder(r.Body).Decode(&nc); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	uc, err := teamixconfig.LoadUserConfig(u.userRoot)
	if err != nil {
		uc = teamixconfig.DefaultUserConfig()
	}
	if nc.Password == "" || nc.Password == "••••••" {
		nc.Password = uc.Nacos.Password
	}
	uc.Nacos = nc
	if err := uc.SaveUserConfig(u.userRoot); err != nil {
		http.Error(w, `{"error":"save user config: `+jsonEscape(err.Error())+`"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// setNacosInConfig 在 config.yaml 文本中更新 nacos 段（yaml.Node 级编辑，保留其他注释）。
func setNacosInConfig(data []byte, nc teamixconfig.NacosConfig) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	m := root.Content[0] // 顶层映射
	if m == nil || m.Kind != yaml.MappingNode {
		m = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content[0] = m
	}
	// 构造新 nacos 段（字段固定列出，空值写 ""）
	nm := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	nacosKV := [][2]string{
		{"server_addr", nc.ServerAddr},
		{"namespace", nc.Namespace},
		{"config_group", nc.ConfigGroup},
		{"username", nc.Username},
		{"password", nc.Password},
	}
	for _, kv := range nacosKV {
		nm.Content = append(nm.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: kv[0]},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: kv[1]},
		)
	}
	// 找/替换 nacos 键
	replaced := false
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == "nacos" {
			m.Content[i+1] = nm
			replaced = true
			break
		}
	}
	if !replaced {
		m.Content = append(m.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "nacos"},
			nm,
		)
	}
	return yaml.Marshal(&root)
}
