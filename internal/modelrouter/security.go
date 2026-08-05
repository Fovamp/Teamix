package modelrouter

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"reasonix/internal/provider"
)

// ReasonSensitiveForcedInternal = 安全层覆盖路由决定：敏感内容强制内部（fail-closed）。
const ReasonSensitiveForcedInternal = "sensitive_forced_internal"

// ReasonToolArgSensitive = 入参扫描命中机密清单（tool call 参数路径即泄露点）。
const ReasonToolArgSensitive = "tool_arg_sensitive_confidential"

// ReasonInternalPrompt = System Prompt 含内部标记，强制内部。
const ReasonInternalPrompt = "internal_prompt_marker"

// SensitiveRules 是机密黑名单（.teamix/config.yaml 的 sensitive 段）：
// 命中 dirs/files 的内容强制走内部模型，配置层无外部路径。
type SensitiveRules struct {
	Dirs  []string // 目录前缀，如 "tenders/"、"data/"
	Files []string // 文件名 glob，如 "*.pem"
}

// MatchPath 判断路径是否命中机密清单。
func (s *SensitiveRules) MatchPath(path string) bool {
	if s == nil {
		return false
	}
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

// scanToolArgs 扫描 assistant 消息中的 tool call 参数：若 Arguments JSON 里出现
// 命中机密清单的路径，则该请求整体升级为 confidential（强制内部，fail-closed）。
func (s *SensitiveRules) scanToolArgs(msgs []provider.Message) bool {
	if s == nil {
		return false
	}
	for _, m := range msgs {
		for _, tc := range m.ToolCalls {
			if s.argsContainSensitivePath(tc.Arguments) {
				return true
			}
		}
	}
	return false
}

// argsContainSensitivePath 解析 tool 参数 JSON，检查常见的路径字段（path/dir/file/src）。
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
	return false
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

// securityForceReason 返回安全层强制内部的 reason（空 = 放行）。
// 检查顺序：敏感级标记 → 入参扫描（tool call 路径命中机密清单）→ system prompt 标记。
// 此裁决独立于路由规则、不可被覆盖——非 public 内容配置层就没有外部路径。
func (r *RouterProvider) securityForceReason(req provider.Request) string {
	if req.Sensitivity != "" && req.Sensitivity != provider.SensitivityPublic {
		return ReasonSensitiveForcedInternal
	}
	if r.cfg.Sensitive != nil && r.cfg.Sensitive.scanToolArgs(req.Messages) {
		return ReasonToolArgSensitive
	}
	if r.cfg.InternalPromptMarker != "" && hasInternalPrompt(req.Messages, r.cfg.InternalPromptMarker) {
		return ReasonInternalPrompt
	}
	return ""
}
