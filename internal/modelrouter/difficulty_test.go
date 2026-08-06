package modelrouter

import (
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func difficultyRouter() *RouterProvider {
	internal := pool(KindInternal, "qwen", 0, 0, &fakeProvider{name: "qwen", text: "internal-ok"})
	external := pool(KindExternal, "deepseek", 0, 0, &fakeProvider{name: "deepseek", text: "external-ok"})
	return New(Config{
		Internal: internal,
		External: external,
		RoleDefault: map[provider.Purpose]Kind{
			provider.PurposeExecute: KindExternal,
		},
		EasyThreshold: 800,
	})
}

func TestDifficultyRouteSimpleExecuteInternal(t *testing.T) {
	r := difficultyRouter()
	var got Decision
	r.OnDecision = func(d Decision) { got = d }
	text, err := streamText(t, r, provider.Request{
		Purpose: provider.PurposeExecute,
		Messages: []provider.Message{{
			Role:    provider.RoleUser,
			Content: "这句话是什么意思",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if text != "internal-ok" {
		t.Errorf("simple execute routed to %q, want internal (difficulty)", text)
	}
	if got.Reason != ReasonDifficulty {
		t.Errorf("reason = %q, want difficulty_route_internal", got.Reason)
	}
}

func TestDifficultyRouteComplexExecuteExternal(t *testing.T) {
	r := difficultyRouter()
	text, err := streamText(t, r, provider.Request{
		Purpose: provider.PurposeExecute,
		Messages: []provider.Message{{
			Role:    provider.RoleUser,
			Content: strings.Repeat("这是一个很长的复杂问题需要深入分析推理 ", 300), // > 800 tokens 估算
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if text != "external-ok" {
		t.Errorf("long execute routed to %q, want external", text)
	}
}

func TestDifficultyRouteToolCallExternal(t *testing.T) {
	r := difficultyRouter()
	text, err := streamText(t, r, provider.Request{
		Purpose: provider.PurposeExecute,
		Messages: []provider.Message{{
			Role: provider.RoleUser,
			Content: "看一下这个文件",
		}, {
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{{Name: "read_file", Arguments: `{"path":"x.go"}`}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if text != "external-ok" {
		t.Errorf("tool-call execute routed to %q, want external (complex)", text)
	}
}

func TestDifficultyRouteDisabled(t *testing.T) {
	internal := pool(KindInternal, "qwen", 0, 0, &fakeProvider{name: "qwen", text: "internal-ok"})
	external := pool(KindExternal, "deepseek", 0, 0, &fakeProvider{name: "deepseek", text: "external-ok"})
	r := New(Config{
		Internal: internal,
		External: external,
		RoleDefault: map[provider.Purpose]Kind{
			provider.PurposeExecute: KindExternal,
		},
		// EasyThreshold 未设 = 禁用
	})
	text, err := streamText(t, r, provider.Request{
		Purpose: provider.PurposeExecute,
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "短问题"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if text != "external-ok" {
		t.Errorf("threshold disabled: routed to %q, want external", text)
	}
}
