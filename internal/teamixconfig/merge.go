package teamixconfig

// MergedConfig 是公共配置与用户私有配置合并后的有效配置。
//
// 合并规则（私有 > 公共）：
//   - 标量字段（language）：私有非空时覆盖公共值；
//   - 模型（default_model）仅公共可配（公司统一 token，不允许私人覆盖）。
// 技能/MCP/人格不再进配置合并：实体在 .reasonix/，经 reasonix.toml
// （[skills] paths / [[plugins]] / system_prompt_file）生效。
type MergedConfig struct {
	// DefaultModel 仅来自公共 teamix.default_model（模型不允许私人配置）。
	DefaultModel string
	Language     string
}

// Merge 把用户私有配置叠加到公共配置之上。
//   - global 为公共 .teamix/config.yaml 的解析结果；
//   - user 为私有 users/<name>/.teamix/config.yaml 的解析结果（可为 nil）。
func Merge(global *Config, user *UserConfig) *MergedConfig {
	m := &MergedConfig{Language: "zh"}
	if global != nil {
		m.DefaultModel = global.Teamix.DefaultModel
	}
	if user != nil && user.Preferences.Language != "" {
		m.Language = user.Preferences.Language
	}
	return m
}
