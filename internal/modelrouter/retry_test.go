package modelrouter

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"reasonix/internal/provider"
)

// flakyProvider 前 failTimes 次 Stream 返回错误，之后成功。
type flakyProvider struct {
	name      string
	failTimes int32
	calls     atomic.Int32
}

func (f *flakyProvider) Name() string { return f.name }

func (f *flakyProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	f.calls.Add(1)
	if f.calls.Load() <= f.failTimes {
		return nil, errors.New("upstream 500")
	}
	ch := make(chan provider.Chunk, 1)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	close(ch)
	return ch, nil
}

func TestExternalRetryTwicePublic(t *testing.T) {
	internal := pool(KindInternal, "qwen", 0, 0, &fakeProvider{name: "qwen", text: "internal"})
	ext := &flakyProvider{name: "deepseek", failTimes: 2}
	external := pool(KindExternal, "deepseek", 0, 0, ext)
	r := New(Config{Internal: internal, External: external})

	text, err := streamText(t, r, provider.Request{}) // 空 sensitivity = public 可重试
	if err != nil {
		t.Fatal(err)
	}
	if text != "ok" {
		t.Errorf("text = %q, want ok (after retries)", text)
	}
	if ext.calls.Load() != 3 {
		t.Errorf("calls = %d, want 3 (1+2 retries)", ext.calls.Load())
	}
}

func TestNoRetryForSensitive(t *testing.T) {
	internal := pool(KindInternal, "qwen", 0, 0, &fakeProvider{name: "qwen", text: "internal"})
	ext := &flakyProvider{name: "deepseek", failTimes: 99}
	external := pool(KindExternal, "deepseek", 0, 0, ext)
	r := New(Config{Internal: internal, External: external})

	// redact 请求禁止重试（防重复传输敏感数据）→ 一次失败即降级内部
	text, err := streamText(t, r, provider.Request{
		Purpose:     provider.PurposeExecute,
		Sensitivity: provider.SensitivityRedact,
	})
	if err != nil {
		t.Fatal(err)
	}
	if text != "internal" {
		t.Errorf("text = %q, want internal fallback", text)
	}
	if ext.calls.Load() != 1 {
		t.Errorf("sensitive calls = %d, want 1 (no retry)", ext.calls.Load())
	}
}

func TestQuotaExceededFallbackInternal(t *testing.T) {
	internal := pool(KindInternal, "qwen", 0, 0, &fakeProvider{name: "qwen", text: "internal-ok"})
	external := pool(KindExternal, "deepseek", 0, 0, &fakeProvider{name: "deepseek", text: "external-ok"})
	quotaHit := false
	r := New(Config{
		Internal: internal,
		External: external,
		RoleDefault: map[provider.Purpose]Kind{
			provider.PurposeExecute: KindExternal,
		},
		QuotaCheck: func() bool { return !quotaHit },
	})
	var got Decision
	r.OnDecision = func(d Decision) { got = d }

	// 配额未超 → 外部
	text, err := streamText(t, r, provider.Request{Purpose: provider.PurposeExecute})
	if err != nil {
		t.Fatal(err)
	}
	if text != "external-ok" {
		t.Errorf("quota ok text = %q, want external", text)
	}

	// 配额超限 → 柔性降级内部（不报错）
	quotaHit = true
	text, err = streamText(t, r, provider.Request{Purpose: provider.PurposeExecute})
	if err != nil {
		t.Fatal(err)
	}
	if text != "internal-ok" {
		t.Errorf("quota exceeded text = %q, want internal (soft degrade)", text)
	}
	if got.Reason != ReasonQuota {
		t.Errorf("reason = %q, want quota_exceeded_fallback_internal", got.Reason)
	}
}
