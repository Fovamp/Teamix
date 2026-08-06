package modelrouter

import (
	"context"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

// fakeProvider 实现 provider.Provider，用于测试。
type fakeProvider struct {
	name        string
	firstErr    error // 非 nil：Stream 返回 channel 首 chunk 为 ChunkError
	text        string
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	if f.firstErr != nil {
		ch <- provider.Chunk{Type: provider.ChunkError, Err: f.firstErr}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: f.text}
	}
	close(ch)
	return ch, nil
}

func pool(kind Kind, name string, maxTokens, maxParallel int, ps ...provider.Provider) *Pool {
	roles := map[provider.Purpose]bool{
		provider.PurposeExecute:  true,
		provider.PurposePlan:     true,
		provider.PurposeSubagent: true,
	}
	return &Pool{Kind: kind, Name: name, Providers: ps, Roles: roles, MaxInputTokens: maxTokens, MaxParallel: maxParallel}
}

func testRouter(offline bool) *RouterProvider {
	internal := pool(KindInternal, "qwen", 100, 0, &fakeProvider{name: "qwen", text: "internal-ok"})
	external := pool(KindExternal, "deepseek", 0, 0, &fakeProvider{name: "deepseek", text: "external-ok"})
	return New(Config{
		Internal: internal,
		External: external,
		RoleDefault: map[provider.Purpose]Kind{
			provider.PurposeExecute:  KindExternal,
			provider.PurposePlan:     KindInternal,
			provider.PurposeSubagent: KindInternal,
		},
		Offline: offline,
	})
}

func streamText(t *testing.T, r *RouterProvider, req provider.Request) (string, error) {
	t.Helper()
	ch, err := r.Stream(context.Background(), req)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for c := range ch {
		switch c.Type {
		case provider.ChunkText:
			sb.WriteString(c.Text)
		case provider.ChunkError:
			return sb.String(), c.Err
		}
	}
	return sb.String(), nil
}

func TestEstimateTokens(t *testing.T) {
	cn := EstimateTokens([]provider.Message{{Role: provider.RoleUser, Content: strings.Repeat("中", 100)}})
	if cn < 60 || cn > 100 {
		t.Errorf("CJK 100 chars ≈ %d tokens, want ~75", cn)
	}
	en := EstimateTokens([]provider.Message{{Role: provider.RoleUser, Content: strings.Repeat("a", 400)}})
	if en < 80 || en > 120 {
		t.Errorf("latin 400 chars ≈ %d tokens, want ~100", en)
	}
}

func TestRouteExecuteExternal(t *testing.T) {
	r := testRouter(false)
	var got Decision
	r.OnDecision = func(d Decision) { got = d }
	text, err := streamText(t, r, provider.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if text != "external-ok" {
		t.Errorf("execute routed to %q, want external", text)
	}
	if got.Pool != KindExternal || got.Reason != ReasonRuleExternal {
		t.Errorf("decision = %+v, want external/rule", got)
	}
}

func TestRouteGuardForcesInternal(t *testing.T) {
	r := testRouter(false)
	text, err := streamText(t, r, provider.Request{Purpose: provider.PurposeGuard})
	if err != nil {
		t.Fatal(err)
	}
	if text != "internal-ok" {
		t.Errorf("guard routed to %q, want internal (forced)", text)
	}
}

func TestRouteOfflineAllInternal(t *testing.T) {
	r := testRouter(true)
	var got Decision
	r.OnDecision = func(d Decision) { got = d }
	text, err := streamText(t, r, provider.Request{Purpose: provider.PurposeExecute})
	if err != nil {
		t.Fatal(err)
	}
	if text != "internal-ok" {
		t.Errorf("offline execute routed to %q, want internal", text)
	}
	if got.Reason != ReasonOffline {
		t.Errorf("reason = %q, want offline_internal", got.Reason)
	}
}

func TestContextOverflowToExternal(t *testing.T) {
	r := testRouter(false) // internal max_input_tokens = 100
	var got Decision
	r.OnDecision = func(d Decision) { got = d }
	req := provider.Request{
		Purpose:  provider.PurposePlan, // 默认 internal
		Messages: []provider.Message{{Role: provider.RoleUser, Content: strings.Repeat("中", 500)}},
	}
	text, err := streamText(t, r, req)
	if err != nil {
		t.Fatal(err)
	}
	if text != "external-ok" {
		t.Errorf("overflow plan routed to %q, want external", text)
	}
	if got.Reason != ReasonContextOverflow {
		t.Errorf("reason = %q, want context_overflow_to_external", got.Reason)
	}
}

func TestFallbackOnFirstChunkError(t *testing.T) {
	internal := pool(KindInternal, "qwen", 0, 0, &fakeProvider{name: "qwen", text: "internal-ok"})
	external := pool(KindExternal, "deepseek", 0, 0, &fakeProvider{name: "deepseek", firstErr: errors.New("upstream 429")})
	r := New(Config{Internal: internal, External: external})
	var reasons []string
	r.OnDecision = func(d Decision) { reasons = append(reasons, d.Reason) }
	text, err := streamText(t, r, provider.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if text != "internal-ok" {
		t.Errorf("fallback text = %q, want internal-ok", text)
	}
	if len(reasons) != 2 || reasons[0] != ReasonRuleExternal || reasons[1] != ReasonFallbackInternal {
		t.Errorf("reasons = %v, want [rule_execute_external fallback_external_to_internal]", reasons)
	}
}

func TestFailClosedNoPool(t *testing.T) {
	r := New(Config{}) // 无任何池
	ch, err := r.Stream(context.Background(), provider.Request{})
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	for c := range ch {
		if c.Type == provider.ChunkText {
			text.WriteString(c.Text)
		}
	}
	if text.Len() == 0 {
		t.Fatal("expected non-empty error message")
	}
	if !strings.Contains(text.String(), "fail-closed") {
		t.Errorf("chunk text = %q, want fail-closed hint", text.String())
	}
}
