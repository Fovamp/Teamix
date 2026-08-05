package modelrouter

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

// hallucinateProvider 模拟外部模型输出一个映射表里不存在的假名（幻觉）。
type hallucinateProvider struct{ name string }

func (h *hallucinateProvider) Name() string { return h.name }

func (h *hallucinateProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 1)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "分析完成，客户编号 PH99 的信用风险偏高"}
	close(ch)
	return ch, nil
}

func TestClosedLoopFallbackOnHallucination(t *testing.T) {
	internal := pool(KindInternal, "qwen", 0, 0, &fakeProvider{name: "qwen", text: "内部模型生成的安全分析"})
	external := pool(KindExternal, "deepseek", 0, 0, &hallucinateProvider{name: "deepseek"})
	r := New(Config{
		Internal: internal,
		External: external,
		RoleDefault: map[provider.Purpose]Kind{
			provider.PurposeExecute: KindExternal,
		},
	})
	var alerts []string
	r.OnRestoreFail = func(issue string) { alerts = append(alerts, issue) }
	var reasons []string
	r.OnDecision = func(d Decision) { reasons = append(reasons, d.Reason) }

	text, err := streamText(t, r, provider.Request{
		Purpose:     provider.PurposeExecute,
		Sensitivity: provider.SensitivityRedact,
		Messages: []provider.Message{{
			Role:    provider.RoleUser,
			Content: "张三的号码 13800138000",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// 外部输出含幻觉假名 → 丢弃 → 内部池重生成
	if text != "内部模型生成的安全分析" {
		t.Errorf("closed-loop fallback text = %q, want internal regeneration", text)
	}
	if len(alerts) == 0 || !strings.Contains(alerts[0], "closed_loop_detected") {
		t.Errorf("expected closed_loop_detected alert, got %v", alerts)
	}
	found := false
	for _, r := range reasons {
		if r == ReasonClosedLoopFallback {
			found = true
		}
	}
	if !found {
		t.Errorf("reasons = %v, want closed_loop_fallback_internal", reasons)
	}
}

func TestClosedLoopPassThroughOnCleanOutput(t *testing.T) {
	internal := pool(KindInternal, "qwen", 0, 0, &fakeProvider{name: "qwen", text: "internal"})
	external := pool(KindExternal, "deepseek", 0, 0, &echoProvider{name: "deepseek"})
	r := New(Config{
		Internal: internal,
		External: external,
		RoleDefault: map[provider.Purpose]Kind{
			provider.PurposeExecute: KindExternal,
		},
	})
	var alerts []string
	r.OnRestoreFail = func(issue string) { alerts = append(alerts, issue) }

	text, err := streamText(t, r, provider.Request{
		Purpose:     provider.PurposeExecute,
		Sensitivity: provider.SensitivityRedact,
		Messages: []provider.Message{{
			Role:    provider.RoleUser,
			Content: "张三的号码 13800138000 请分析",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// echo 返回假名化内容 → 还原校验通过 → 直接转发还原后的真实值
	if !strings.Contains(text, "13800138000") {
		t.Errorf("clean output should restore real value: %q", text)
	}
	if len(alerts) != 0 {
		t.Errorf("unexpected alerts: %v", alerts)
	}
}
