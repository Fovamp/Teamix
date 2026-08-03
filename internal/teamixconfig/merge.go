package teamixconfig

// MergedConfig 是公共配置与用户私有配置合并后的有效配置。
//
// 合并规则（私有 > 公共）：
//   - 标量字段（default_model / language）：私有非空时覆盖公共值；
//   - 列表字段（MCP）：公共在前、私有追加；同名条目以私有为准（覆盖公共）。
type MergedConfig struct {
	DefaultModel string
	Language     string
	// MCP 是合并后的有效 MCP 列表（公共 + 私有，同名私有覆盖公共）。
	MCP []PluginRef
	// GlobalMCP / PrivateMCP 保留分层视图，供 source 展示区分来源。
	GlobalMCP  []PluginRef
	PrivateMCP []PluginRef
}

// Merge 把用户私有配置叠加到公共配置之上。
//   - global 为公共 .teamix/config.yaml 的解析结果；
//   - user 为私有 users/<name>/.teamix/config.yaml 的解析结果（可为 nil）；
//   - globalMCP 为公共 .reasonix/mcp.json 中解析出的 MCP 列表（由 serve 层传入）。
func Merge(global *Config, user *UserConfig, globalMCP []PluginRef) *MergedConfig {
	m := &MergedConfig{
		Language:  "zh",
		GlobalMCP: globalMCP,
	}
	if global != nil {
		m.DefaultModel = global.Teamix.DefaultModel
	}
	if user != nil {
		if user.Preferences.Model != "" {
			m.DefaultModel = user.Preferences.Model
		}
		if user.Preferences.Language != "" {
			m.Language = user.Preferences.Language
		}
		m.PrivateMCP = user.MCP
	}

	// 合并 MCP：公共在前，私有追加；同名私有覆盖公共。
	m.MCP = make([]PluginRef, 0, len(m.GlobalMCP)+len(m.PrivateMCP))
	seen := make(map[string]int, len(m.GlobalMCP)+len(m.PrivateMCP))
	for _, p := range m.GlobalMCP {
		if p.Name == "" {
			continue
		}
		seen[p.Name] = len(m.MCP)
		m.MCP = append(m.MCP, p)
	}
	for _, p := range m.PrivateMCP {
		if p.Name == "" {
			continue
		}
		if idx, ok := seen[p.Name]; ok {
			m.MCP[idx] = p // 同名私有覆盖公共
		} else {
			seen[p.Name] = len(m.MCP)
			m.MCP = append(m.MCP, p)
		}
	}
	return m
}
