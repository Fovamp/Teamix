package modelrouter

import (
	"testing"

	"reasonix/internal/provider"
)

func sensitiveRouter() *RouterProvider {
	internal := pool(KindInternal, "qwen", 0, 0, &fakeProvider{name: "qwen", text: "internal-ok"})
	external := pool(KindExternal, "deepseek", 0, 0, &fakeProvider{name: "deepseek", text: "external-ok"})
	return New(Config{
		Internal: internal,
		External: external,
		RoleDefault: map[provider.Purpose]Kind{
			provider.PurposeExecute: KindExternal,
		},
		Sensitive: &SensitiveRules{
			Dirs:  []string{"tenders/", "data/"},
			Files: []string{"*.pem"},
		},
		InternalPromptMarker: "[INTERNAL]",
	})
}

func TestSensitiveMarkForcesInternal(t *testing.T) {
	r := sensitiveRouter()
	var got Decision
	r.OnDecision = func(d Decision) { got = d }
	text, err := streamText(t, r, provider.Request{
		Purpose:     provider.PurposeExecute,
		Sensitivity: provider.SensitivityConfidential,
	})
	if err != nil {
		t.Fatal(err)
	}
	if text != "internal-ok" {
		t.Errorf("confidential routed to %q, want internal (forced)", text)
	}
	if got.Reason != ReasonSensitiveForcedInternal {
		t.Errorf("reason = %q, want sensitive_forced_internal", got.Reason)
	}
}

func TestSensitivityPublicAllowed(t *testing.T) {
	r := sensitiveRouter()
	text, err := streamText(t, r, provider.Request{
		Purpose:     provider.PurposeExecute,
		Sensitivity: provider.SensitivityPublic,
	})
	if err != nil {
		t.Fatal(err)
	}
	if text != "external-ok" {
		t.Errorf("public routed to %q, want external", text)
	}
}

func TestToolArgSensitivePathForcesInternal(t *testing.T) {
	r := sensitiveRouter()
	var got Decision
	r.OnDecision = func(d Decision) { got = d }
	text, err := streamText(t, r, provider.Request{
		Purpose: provider.PurposeExecute,
		Messages: []provider.Message{{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{{
				Name:      "read_file",
				Arguments: `{"path": "tenders/项目A.docx"}`,
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if text != "internal-ok" {
		t.Errorf("tool arg hit routed to %q, want internal (forced)", text)
	}
	if got.Reason != ReasonToolArgSensitive {
		t.Errorf("reason = %q, want tool_arg_sensitive_confidential", got.Reason)
	}
}

func TestInternalPromptMarkerForcesInternal(t *testing.T) {
	r := sensitiveRouter()
	text, err := streamText(t, r, provider.Request{
		Purpose: provider.PurposeExecute,
		Messages: []provider.Message{{
			Role:    provider.RoleSystem,
			Content: "[INTERNAL] 内部架构说明……",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if text != "internal-ok" {
		t.Errorf("internal prompt routed to %q, want internal", text)
	}
}

func TestSensitiveRulesMatchPath(t *testing.T) {
	rules := &SensitiveRules{
		Dirs:  []string{"tenders/", "data/"},
		Files: []string{"*.pem"},
	}
	cases := []struct {
		path string
		want bool
	}{
		{"tenders/项目A.docx", true},
		{"data/test_transactions.csv", true},
		{"internal/risk/analyzer.go", false},
		{"secrets/key.pem", true},
		{"secrets/key.pem.bak", false},
	}
	for _, c := range cases {
		if got := rules.MatchPath(c.path); got != c.want {
			t.Errorf("MatchPath(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestSensitiveDefaultNoConfig(t *testing.T) {
	// 未配置敏感规则时，入参扫描不触发（默认行为，不误伤）
	r := New(Config{
		Internal:    pool(KindInternal, "qwen", 0, 0, &fakeProvider{name: "qwen", text: "internal-ok"}),
		External:    pool(KindExternal, "deepseek", 0, 0, &fakeProvider{name: "deepseek", text: "external-ok"}),
		RoleDefault: map[provider.Purpose]Kind{provider.PurposeExecute: KindExternal},
	})
	text, err := streamText(t, r, provider.Request{
		Purpose: provider.PurposeExecute,
		Messages: []provider.Message{{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{{
				Name:      "read_file",
				Arguments: `{"path": "tenders/x.docx"}`,
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if text != "external-ok" {
		t.Errorf("no rules: routed to %q, want external (no forced internal)", text)
	}
}

func TestSecretPatternForcesInternal(t *testing.T) {
	// 内容兜底（F）：消息中出现密钥模式（如 AGENTS.md 硬编码）→ 强制内部
	r := sensitiveRouter()
	text, err := streamText(t, r, provider.Request{
		Purpose: provider.PurposeExecute,
		Messages: []provider.Message{{
			Role:    provider.RoleSystem,
			Content: "内部规范：数据库密钥 sk-proj-abc123def456ghi789 仅内网使用",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if text != "internal-ok" {
		t.Errorf("secret pattern routed to %q, want internal (forced)", text)
	}
}

func TestHasSecretPattern(t *testing.T) {
	if !HasSecretPattern("连接串含 sk-abc123def456ghi789 结尾") {
		t.Error("apikey pattern not detected")
	}
	if HasSecretPattern("正常的技术讨论内容") {
		t.Error("false positive on normal content")
	}
}

// bash command 含机密目录名 → 拦截（防 `bash cat tenders/x` 绕过文件工具）
func TestScanToolArgsBashCommandSensitive(t *testing.T) {
	s := &SensitiveRules{Dirs: []string{"tenders/"}}
	if !s.scanToolArgs([]provider.Message{{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "t1", Name: "bash", Arguments: `{"command":"cat tenders/项目A.docx"}`}}}}) {
		t.Fatal("bash command containing tenders/ should be flagged")
	}
}

// bash command 不含机密目录 → 放行
func TestScanToolArgsBashCommandPublic(t *testing.T) {
	s := &SensitiveRules{Dirs: []string{"tenders/"}}
	if s.scanToolArgs([]provider.Message{{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "t1", Name: "bash", Arguments: `{"command":"ls internal/"}`}}}}) {
		t.Fatal("bash command without sensitive dir should pass")
	}
}

// MCP 工具参数名不可预期（file_path）→ 全部字符串值路径匹配
func TestScanToolArgsMCPCustomKey(t *testing.T) {
	s := &SensitiveRules{Dirs: []string{"tenders/"}}
	if !s.scanToolArgs([]provider.Message{{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "t1", Name: "mcp__fs__read", Arguments: `{"file_path":"tenders/项目A.docx"}`}}}}) {
		t.Fatal("MCP tool custom key file_path hitting tenders/ should be flagged")
	}
}

// ContainsDir 宽松匹配：自由文本含机密目录名即命中
func TestContainsDir(t *testing.T) {
	s := &SensitiveRules{Dirs: []string{"tenders/"}}
	if !s.ContainsDir("cat tenders/x") {
		t.Fatal("ContainsDir should match embedded dir name")
	}
	if s.ContainsDir("ls internal/") {
		t.Fatal("ContainsDir should not match unrelated text")
	}
}
