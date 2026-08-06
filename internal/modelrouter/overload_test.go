package modelrouter

import (
	"context"
	"strings"
	"testing"
	"time"

	"reasonix/internal/provider"
)

// blockingProvider 永不返回（占住并发槽），started 在 Stream 开始时通知。
type blockingProvider struct {
	name    string
	started chan struct{}
}

func (b *blockingProvider) Name() string { return b.name }

func (b *blockingProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	if b.started != nil {
		close(b.started)
	}
	ch := make(chan provider.Chunk)
	go func() { <-ctx.Done(); close(ch) }()
	return ch, nil
}

func TestSensitiveOverloadNoExternalFallback(t *testing.T) {
	// 内部池并发 1 且被占满 → guard 请求过载 → 报错，绝不降级外部
	started := make(chan struct{})
	internal := pool(KindInternal, "qwen", 0, 1, &blockingProvider{name: "qwen", started: started})
	external := pool(KindExternal, "deepseek", 0, 0, &fakeProvider{name: "deepseek", text: "external-ok"})
	r := New(Config{
		Internal:       internal,
		External:       external,
		AcquireTimeout: 50 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	// 第一个请求占住并发槽（等 blocking provider 已开始）
	done := make(chan struct{})
	go func() {
		_, _ = r.Stream(ctx, provider.Request{Purpose: provider.PurposeGuard, Sensitivity: provider.SensitivityConfidential})
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("first request did not start")
	}
	time.Sleep(10 * time.Millisecond) // 让 streamPool 完成 acquire

	// 第二个 guard 请求：内部过载 → 必须报错（不能降级 external）
	_, err2 := r.Stream(context.Background(), provider.Request{Purpose: provider.PurposeGuard, Sensitivity: provider.SensitivityConfidential})
	if err2 == nil {
		t.Fatal("sensitive overload should error, not fall back to external")
	}
	if !strings.Contains(err2.Error(), "busy") {
		t.Errorf("err = %v, want ErrPoolBusy", err2)
	}
}

func TestNormalOverloadFallsBackToInternal(t *testing.T) {
	// 外部池过载 → 普通请求降级内部
	internal := pool(KindInternal, "qwen", 0, 0, &fakeProvider{name: "qwen", text: "internal-ok"})
	external := pool(KindExternal, "deepseek", 0, 1, &blockingProvider{name: "deepseek"})
	r := New(Config{
		Internal:       internal,
		External:       external,
		RoleDefault:    map[provider.Purpose]Kind{provider.PurposeExecute: KindExternal},
		AcquireTimeout: 50 * time.Millisecond,
	})
	// 占住外部槽
	ch, err := external.Providers[0].Stream(context.Background(), provider.Request{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ch }()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go func() {
		_, _ = r.Stream(ctx, provider.Request{Purpose: provider.PurposeExecute})
	}()
	time.Sleep(20 * time.Millisecond)

	text, err := streamText(t, r, provider.Request{Purpose: provider.PurposeExecute})
	if err != nil {
		t.Fatal(err)
	}
	if text != "internal-ok" {
		t.Errorf("overloaded external should fall back to internal, got %q", text)
	}
}
