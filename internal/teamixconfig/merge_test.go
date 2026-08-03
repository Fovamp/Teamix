package teamixconfig

import "testing"

func TestMergeScalars(t *testing.T) {
	global := &Config{Teamix: TeamixConfig{Name: "Teamix", DefaultModel: "deepseek-v3"}}
	user := &UserConfig{Preferences: Preferences{Language: "en", Model: "qwen-max"}}
	m := Merge(global, user, nil)
	if m.DefaultModel != "qwen-max" {
		t.Errorf("user model should override global, got %q", m.DefaultModel)
	}
	if m.Language != "en" {
		t.Errorf("language should be user's, got %q", m.Language)
	}

	// 私有为空 → 回落到公共
	m2 := Merge(global, &UserConfig{Preferences: Preferences{Language: "en"}}, nil)
	if m2.DefaultModel != "deepseek-v3" {
		t.Errorf("empty user model should fall back to global, got %q", m2.DefaultModel)
	}
	if m2.Language != "en" {
		t.Errorf("language mismatch, got %q", m2.Language)
	}

	// 全部为空 → 默认
	m3 := Merge(nil, nil, nil)
	if m3.DefaultModel != "" || m3.Language != "zh" {
		t.Errorf("defaults wrong: model=%q lang=%q", m3.DefaultModel, m3.Language)
	}
}

func TestMergeMCPList(t *testing.T) {
	global := []PluginRef{
		{Name: "filesystem", Command: "npx", Args: []string{"@fs"}, Type: "stdio"},
		{Name: "shared", Command: "npx", Args: []string{"@shared"}},
	}
	user := &UserConfig{
		MCP: []PluginRef{
			{Name: "shared", Command: "npx", Args: []string{"@shared-private"}}, // 同名覆盖
			{Name: "private-only", Command: "python", Args: []string{"mcp.py"}},
		},
	}
	m := Merge(nil, user, global)
	if len(m.MCP) != 3 {
		t.Fatalf("expected 3 merged MCPs, got %d: %+v", len(m.MCP), m.MCP)
	}
	if m.MCP[0].Name != "filesystem" {
		t.Errorf("global-first order broken: %+v", m.MCP[0])
	}
	// shared 同名 → 私有覆盖
	var shared *PluginRef
	for i := range m.MCP {
		if m.MCP[i].Name == "shared" {
			shared = &m.MCP[i]
		}
	}
	if shared == nil || len(shared.Args) == 0 || shared.Args[0] != "@shared-private" {
		t.Errorf("private same-name MCP should override global, got %+v", shared)
	}
	if len(m.GlobalMCP) != 2 || len(m.PrivateMCP) != 2 {
		t.Errorf("layer views wrong: global=%d private=%d", len(m.GlobalMCP), len(m.PrivateMCP))
	}
}
