package agent

import (
	"encoding/json"
	"testing"

	"reasonix/internal/modelrouter"
	"reasonix/internal/provider"
)

// 机密清单命中 → 拦截该次工具调用（返回 true），不执行、不降级会话
func TestMarkToolSensitivityConfidential(t *testing.T) {
	a := &Agent{
		sensitiveRules: &modelrouter.SensitiveRules{
			Dirs: []string{"tenders/"}, Files: []string{"*.pem"},
		},
		sensitive: provider.SensitivityPublic,
	}
	blocked := a.markToolSensitivity("read_file", json.RawMessage(`{"path": "tenders/项目A.docx"}`))
	if !blocked {
		t.Fatal("read_file on tenders/ should be blocked")
	}
	// 拦截后会话敏感级不降级（机密内容未进上下文，本地窗口不被挤占）
	if a.sensitive != provider.SensitivityPublic {
		t.Fatalf("sensitive = %q, want public (blocked call must not downgrade session)", a.sensitive)
	}
}

func TestMarkToolSensitivityPublicPath(t *testing.T) {
	a := &Agent{
		sensitiveRules: &modelrouter.SensitiveRules{
			Dirs: []string{"tenders/"}, Files: []string{"*.pem"},
		},
	}
	if blocked := a.markToolSensitivity("read_file", json.RawMessage(`{"path": "internal/risk/analyzer.go"}`)); blocked {
		t.Fatal("code path must not be blocked")
	}
}

func TestMarkToolSensitivityGlobFile(t *testing.T) {
	a := &Agent{
		sensitiveRules: &modelrouter.SensitiveRules{
			Dirs: []string{"tenders/"}, Files: []string{"*.pem"},
		},
	}
	if !a.markToolSensitivity("glob", json.RawMessage(`{"file": "deploy/keys.pem"}`)) {
		t.Fatal("glob on *.pem should be blocked")
	}
}

func TestMarkToolSensitivityBadArgs(t *testing.T) {
	a := &Agent{
		sensitiveRules: &modelrouter.SensitiveRules{Dirs: []string{"tenders/"}},
	}
	if a.markToolSensitivity("read_file", json.RawMessage(`not-json`)) {
		t.Fatal("unparseable args must not block (cannot determine path)")
	}
}

// MCP 工具不拦截（声明级经消息标记聚合，internal/confidential 内容可进上下文但锁内部）
func TestMarkToolSensitivityMCPServerNotBlocked(t *testing.T) {
	a := &Agent{sensitiveRules: &modelrouter.SensitiveRules{Dirs: []string{"tenders/"}}}
	if a.markToolSensitivity("mcp__customer-db__query", json.RawMessage(`{"path": "tenders/x"}`)) {
		t.Fatal("MCP tools must not be blocked by the path blacklist")
	}
}

// 未声明 MCP 工具 → internal（数据入口默认兜底）
func TestToolResultSensitivityUndeclaredMCP(t *testing.T) {
	a := &Agent{}
	if s := a.toolResultSensitivity("mcp__customer-db__query", json.RawMessage(`{}`)); s != provider.SensitivityInternal {
		t.Fatalf("undeclared MCP tool = %q, want internal (default fallback)", s)
	}
}

// 声明了 public 的 MCP 工具 → public（可出网）
func TestToolResultSensitivityDeclaredMCP(t *testing.T) {
	a := &Agent{mcpSensitivity: map[string]provider.Sensitivity{"customer-db": provider.SensitivityPublic}}
	if s := a.toolResultSensitivity("mcp__customer-db__query", json.RawMessage(`{}`)); s != provider.SensitivityPublic {
		t.Fatalf("declared public MCP tool = %q, want public", s)
	}
}

// 声明了 confidential 的 MCP 工具 → confidential（内容锁内部，不拦截）
func TestToolResultSensitivityConfidentialMCP(t *testing.T) {
	a := &Agent{mcpSensitivity: map[string]provider.Sensitivity{"tender-db": provider.SensitivityConfidential}}
	if s := a.toolResultSensitivity("mcp__tender-db__list", json.RawMessage(`{}`)); s != provider.SensitivityConfidential {
		t.Fatalf("declared confidential MCP tool = %q, want confidential", s)
	}
}

// doc_kb_search（团队文档知识库入口）→ internal（默认不出网，符合兜底原则）
func TestToolResultSensitivityDocKBSearch(t *testing.T) {
	a := &Agent{}
	if s := a.toolResultSensitivity("doc_kb_search", json.RawMessage(`{"question":"x"}`)); s != provider.SensitivityInternal {
		t.Fatalf("doc_kb_search = %q, want internal (team docs must not egress by default)", s)
	}
}

// 内置工具显式声明优先：架构师把 doc_kb_search 声明为 public → 覆盖默认 internal
func TestToolResultSensitivityToolDeclaration(t *testing.T) {
	a := &Agent{toolSensitivity: map[string]provider.Sensitivity{
		"doc_kb_search": provider.SensitivityPublic,
		"web_fetch":     provider.SensitivityRedact,
	}}
	if s := a.toolResultSensitivity("doc_kb_search", json.RawMessage(`{}`)); s != provider.SensitivityPublic {
		t.Fatalf("declared public doc_kb_search = %q, want public", s)
	}
	if s := a.toolResultSensitivity("web_fetch", json.RawMessage(`{}`)); s != provider.SensitivityRedact {
		t.Fatalf("declared redact web_fetch = %q, want redact", s)
	}
	// 未声明的工具不受影响（web_fetch 默认空标记）
	a2 := &Agent{}
	if s := a2.toolResultSensitivity("web_fetch", json.RawMessage(`{}`)); s != "" {
		t.Fatalf("undeclared web_fetch = %q, want unmarked", s)
	}
}

// 其他普通 builtin（web_fetch 等）→ 空标记（走正常路由）
func TestToolResultSensitivityOtherBuiltin(t *testing.T) {
	a := &Agent{}
	if s := a.toolResultSensitivity("web_fetch", json.RawMessage(`{}`)); s != "" {
		t.Fatalf("web_fetch = %q, want unmarked", s)
	}
}

// 非文件类、非 MCP 工具（bash）→ 不设标记
func TestToolResultSensitivityOtherTool(t *testing.T) {
	a := &Agent{sensitiveRules: &modelrouter.SensitiveRules{Dirs: []string{"tenders/"}}}
	if s := a.toolResultSensitivity("bash", json.RawMessage(`{"command":"ls"}`)); s != "" {
		t.Fatalf("bash tool = %q, want empty (no marker)", s)
	}
}

// 会话初始敏感级（skill/记忆声明聚合）：internal 起会话即锁内部，读代码文件不降级也不升级
func TestBaseSensitivityInitial(t *testing.T) {
	a := &Agent{
		sensitiveRules:  &modelrouter.SensitiveRules{Dirs: []string{"tenders/"}},
		baseSensitivity: provider.SensitivityInternal,
		sensitive:       provider.SensitivityInternal,
	}
	if a.markToolSensitivity("read_file", json.RawMessage(`{"path": "internal/risk/analyzer.go"}`)) {
		t.Fatal("code path must not be blocked")
	}
	if a.sensitive != provider.SensitivityInternal {
		t.Fatalf("sensitive = %q, want internal (base pins session)", a.sensitive)
	}
}
