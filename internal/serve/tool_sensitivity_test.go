package serve

import (
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/teamixconfig"
)

// 内置工具敏感级链路：sensitive.yaml tools 段 → LoadAll 合并 → toolSensitivityMap
// → 声明归一化（非法档位丢弃）。agent 侧判定由 internal/agent 的
// TestToolResultSensitivityToolDeclaration 覆盖。
func TestToolSensitivityMapFromConfig(t *testing.T) {
	root := t.TempDir()
	if err := teamixconfig.EnsureGlobalWorkspace(root); err != nil {
		t.Fatalf("EnsureGlobalWorkspace: %v", err)
	}
	// 模拟前端安全页保存的 sensitive.yaml
	sc := teamixconfig.SensitiveConfig{
		Dirs:  []string{"tenders/"},
		Tools: map[string]string{
			"doc_kb_search": "public",       // 显式声明 → 覆盖默认 internal
			"web_fetch":     "redact",       // 假名化出网
			"bash":          "oops-invalid", // 非法档位 → 应被丢弃
		},
	}
	if err := teamixconfig.SaveSensitiveOverlay(root, sc); err != nil {
		t.Fatalf("SaveSensitiveOverlay: %v", err)
	}

	ts := &TeamixServer{workspaceRoot: root}
	ts.setGlobalCfg(ts.loadGlobalConfig())
	m := ts.toolSensitivityMap()

	if m["doc_kb_search"] != provider.SensitivityPublic {
		t.Errorf("doc_kb_search = %q, want public", m["doc_kb_search"])
	}
	if m["web_fetch"] != provider.SensitivityRedact {
		t.Errorf("web_fetch = %q, want redact", m["web_fetch"])
	}
	if _, ok := m["bash"]; ok {
		t.Errorf("invalid level should be dropped, got %q", m["bash"])
	}
	if len(m) != 2 {
		t.Errorf("map size = %d, want 2 (%v)", len(m), m)
	}
}

// 未声明任何工具档位 → 空 map（agent 走默认兜底）
func TestToolSensitivityMapEmpty(t *testing.T) {
	root := t.TempDir()
	if err := teamixconfig.EnsureGlobalWorkspace(root); err != nil {
		t.Fatalf("EnsureGlobalWorkspace: %v", err)
	}
	ts := &TeamixServer{workspaceRoot: root}
	ts.setGlobalCfg(ts.loadGlobalConfig())
	if m := ts.toolSensitivityMap(); len(m) != 0 {
		t.Fatalf("expected empty map, got %v", m)
	}
}
