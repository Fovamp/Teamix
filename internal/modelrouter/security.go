package modelrouter

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"

	"reasonix/internal/provider"
)

// ReasonSensitiveForcedInternal = 安全层覆盖路由决定：敏感内容强制内部（fail-closed）。
const ReasonSensitiveForcedInternal = "sensitive_forced_internal"

// ReasonClosedLoopFallback = 闭环 Fail-Close：外部输出还原校验失败，丢弃并回退内部重生成。
const ReasonClosedLoopFallback = "closed_loop_fallback_internal"

// ReasonToolArgSensitive = 入参扫描命中机密清单（tool call 参数路径即泄露点）。
const ReasonToolArgSensitive = "tool_arg_sensitive_confidential"

// ReasonInternalPrompt = System Prompt 含内部标记，强制内部。
const ReasonInternalPrompt = "internal_prompt_marker"

// ReasonSecretDetected = 内容检测兜底：消息中出现密钥模式（API key 等），强制内部。
const ReasonSecretDetected = "secret_pattern_detected_internal"

// HasSecretPattern 内容级兜底（判定第二层）：检测文本中的密钥/凭证模式。
// 工具分类管"结构化入口"，这里管"手打机密文本/AGENTS.md 里硬编码密钥"。
func HasSecretPattern(content string) bool {
	for _, c := range builtinPseudoClasses {
		if c.kind == "apikey" && c.re.MatchString(content) {
			return true
		}
	}
	return false
}

// SensitiveRules 是机密黑名单（.teamix/config.yaml 的 sensitive 段）：
// 命中 dirs/files 的内容强制走内部模型，配置层无外部路径。
type SensitiveRules struct {
	Dirs  []string // 目录前缀，如 "tenders/"、"data/"
	Files []string // 文件名 glob，如 "*.pem"

	// cache 路径→是否命中的静态判定缓存（P2 性能项）：同一会话内
	// 配置不变，路径敏感级判定结果可复用；随 SensitiveRules 实例
	// 生命周期（每次 Build 重建，天然随配置热加载失效）。
	cache sync.Map // map[string]bool
}

// MatchPath 判断路径是否命中机密清单（带缓存）。
func (s *SensitiveRules) MatchPath(path string) bool {
	if s == nil {
		return false
	}
	if v, ok := s.cache.Load(path); ok {
		return v.(bool)
	}
	hit := s.matchPathUncached(path)
	s.cache.Store(path, hit)
	return hit
}

func (s *SensitiveRules) matchPathUncached(path string) bool {
	p := filepath.ToSlash(path)
	for _, d := range s.Dirs {
		d = filepath.ToSlash(d)
		if strings.HasPrefix(p, d) || strings.Contains(p, "/"+strings.TrimSuffix(d, "/")+"/") {
			return true
		}
	}
	base := filepath.Base(p)
	for _, f := range s.Files {
		if ok, _ := filepath.Match(f, base); ok {
			return true
		}
	}
	return false
}

// ContainsDir 宽松判定：文本中是否包含任一机密目录名（含尾斜杠前缀匹配）。
// 用于任意访问类工具（bash command / web_fetch url）的**自由文本**参数——
// `bash cat tenders/x` 虽不是合法路径，但文本含机密目录名即视为命中（防绕过）。
func (s *SensitiveRules) ContainsDir(text string) bool {
	if s == nil || len(s.Dirs) == 0 {
		return false
	}
	for _, d := range s.Dirs {
		d = filepath.ToSlash(strings.TrimSpace(d))
		if d == "" {
			continue
		}
		if strings.Contains(text, d) {
			return true
		}
	}
	return false
}

// scanToolArgs 扫描 assistant 消息中的 tool call 参数：若 Arguments JSON 里出现
// 命中机密清单的路径，则该请求整体升级为 confidential（强制内部，fail-closed）。
// MCP 工具（参数名不可预期）对全部字符串值做路径匹配。
func (s *SensitiveRules) scanToolArgs(msgs []provider.Message) bool {
	if s == nil {
		return false
	}
	for _, m := range msgs {
		for _, tc := range m.ToolCalls {
			if strings.HasPrefix(tc.Name, "mcp__") {
				if s.argsAllValuesSensitive(tc.Arguments) {
					return true
				}
			} else if s.argsContainSensitivePath(tc.Arguments) {
				return true
			}
		}
	}
	return false
}

// argsContainSensitivePath 解析 tool 参数 JSON，检查路径字段与任意访问类工具的
// 自由文本字段：path/dir/file/src/target 严格路径匹配；command/cmd/url 宽松包含
// 目录名（防 `bash cat tenders/x` 绕过文件工具的拦截）。
func (s *SensitiveRules) argsContainSensitivePath(args string) bool {
	if args == "" {
		return false
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(args), &m); err != nil {
		// 非 JSON 参数（少见）→ 直接当原文匹配
		return s.MatchPath(args)
	}
	for _, key := range []string{"path", "dir", "file", "src", "target"} {
		if v, ok := m[key].(string); ok && v != "" && s.MatchPath(v) {
			return true
		}
	}
	for _, key := range []string{"command", "cmd", "url"} {
		if v, ok := m[key].(string); ok && v != "" && s.ContainsDir(v) {
			return true
		}
	}
	return false
}

// argsAllValuesSensitive 用于 MCP 工具（参数名不可预期，如 file_path/uri/location）：
// 对全部字符串值做路径匹配（值含路径特征才查，防误伤普通字段）。
func (s *SensitiveRules) argsAllValuesSensitive(args string) bool {
	if args == "" {
		return false
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(args), &m); err != nil {
		return s.MatchPath(args)
	}
	for _, v := range m {
		if sv, ok := v.(string); ok && sv != "" && looksLikePath(sv) && s.MatchPath(sv) {
			return true
		}
	}
	return false
}

// looksLikePath 粗判字符串像路径（含斜杠或已知路径扩展名），避免对任意字段值误伤。
func looksLikePath(s string) bool {
	return strings.Contains(s, "/") || strings.Contains(s, "\\") ||
		strings.Contains(s, ".docx") || strings.Contains(s, ".pdf") ||
		strings.Contains(s, ".xlsx") || strings.Contains(s, ".pem") || strings.Contains(s, ".key")
}

// hasInternalPrompt 检查 system 消息是否含内部提示词标记（System Prompt 分级机制）。
// marker 为空则跳过（不启用该机制）。
func hasInternalPrompt(msgs []provider.Message, marker string) bool {
	if marker == "" {
		return false
	}
	for _, m := range msgs {
		if m.Role == provider.RoleSystem && strings.Contains(m.Content, marker) {
			return true
		}
	}
	return false
}

// hasToolCalls 检查消息里是否含 assistant 工具调用（带工具调用视为复杂请求）。
func hasToolCalls(msgs []provider.Message) bool {
	for _, m := range msgs {
		if len(m.ToolCalls) > 0 {
			return true
		}
	}
	return false
}

// securityForceReason 返回安全层强制内部的 reason（空 = 放行）。
// 检查顺序：敏感级标记 → 入参扫描（tool call 路径命中机密清单）→ system prompt 标记。
// 此裁决独立于路由规则、不可被覆盖——非 public 内容配置层就没有外部路径。
func (r *RouterProvider) securityForceReason(req provider.Request) string {
	// redact 允许假名化出网（P1）；internal/confidential 强制内部（fail-closed）
	if req.Sensitivity != "" && req.Sensitivity != provider.SensitivityPublic && req.Sensitivity != provider.SensitivityRedact {
		return ReasonSensitiveForcedInternal
	}
	if r.cfg.Sensitive != nil && r.cfg.Sensitive.scanToolArgs(req.Messages) {
		return ReasonToolArgSensitive
	}
	if r.cfg.InternalPromptMarker != "" && hasInternalPrompt(req.Messages, r.cfg.InternalPromptMarker) {
		return ReasonInternalPrompt
	}
	// 内容级兜底：任何消息（含 system/AGENTS.md 指令）出现密钥模式 → 强制内部
	for _, m := range req.Messages {
		if m.Content != "" && HasSecretPattern(m.Content) {
			return ReasonSecretDetected
		}
	}
	return ""
}
