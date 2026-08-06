package modelrouter

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

// echoProvider 回显请求内容（模拟外部模型收到假名化请求后复述）。
type echoProvider struct{ name string }

func (e *echoProvider) Name() string { return e.name }

func (e *echoProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 1)
	for _, m := range req.Messages {
		if m.Content != "" {
			ch <- provider.Chunk{Type: provider.ChunkText, Text: m.Content}
			break
		}
	}
	close(ch)
	return ch, nil
}

func redactRouter() *RouterProvider {
	internal := pool(KindInternal, "qwen", 0, 0, &fakeProvider{name: "qwen", text: "internal-ok"})
	external := pool(KindExternal, "deepseek", 0, 0, &echoProvider{name: "deepseek"})
	r := New(Config{
		Internal: internal,
		External: external,
		RoleDefault: map[provider.Purpose]Kind{
			provider.PurposeExecute: KindExternal,
		},
		PseudoDict: map[string]string{"张三": "客户甲"},
	})
	return r
}

func TestRedactOutboundPseudonymizesAndRestores(t *testing.T) {
	r := redactRouter()
	var restoreIssues []string
	r.OnRestoreFail = func(requestID, issue string) { restoreIssues = append(restoreIssues, issue) }

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
	// echo 返回假名化后的内容 → 还原回真实值
	if !strings.Contains(text, "张三") {
		t.Errorf("restore failed, no real value: %q", text)
	}
	if strings.Contains(text, "PH") && !strings.Contains(text, "13800138000") {
		t.Errorf("pseudonym leaked through: %q", text)
	}
	if len(restoreIssues) != 0 {
		t.Errorf("unexpected restore issues: %v", restoreIssues)
	}
}

func TestRedactNotAppliedToInternal(t *testing.T) {
	// redact 但路由到内部池（如 guard 强制内部）→ 不假名化，原样发内部
	internal := pool(KindInternal, "qwen", 0, 0, &echoProvider{name: "qwen"})
	external := pool(KindExternal, "deepseek", 0, 0, &fakeProvider{name: "deepseek", text: "external-ok"})
	r := New(Config{
		Internal: internal,
		External: external,
		RoleDefault: map[provider.Purpose]Kind{
			provider.PurposeExecute: KindExternal,
		},
	})
	text, err := streamText(t, r, provider.Request{
		Purpose:     provider.PurposeGuard, // 强制内部
		Sensitivity: provider.SensitivityRedact,
		Messages: []provider.Message{{
			Role:    provider.RoleUser,
			Content: "张三 13800138000",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "张三") || !strings.Contains(text, "13800138000") {
		t.Errorf("internal should receive real values: %q", text)
	}
}
