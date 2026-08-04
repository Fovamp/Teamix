package headroom

import (
	"context"
	"net"
	"testing"
	"time"

	"reasonix/internal/provider"
)

// mockProv 是 provider.Provider 的替身，记录收到的消息。
type mockProv struct {
	name string
	got  []provider.Message
}

func (m *mockProv) Name() string { return m.name }

func (m *mockProv) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	m.got = req.Messages
	ch := make(chan provider.Chunk)
	close(ch)
	return ch, nil
}

// headroomUp 探测本地 headroom proxy 是否在跑（集成测试，不在则跳过）。
func headroomUp(t *testing.T) *Client {
	t.Helper()
	conn, err := net.DialTimeout("tcp", "127.0.0.1:8788", 500*time.Millisecond)
	if err != nil {
		t.Skip("headroom proxy not running on :8788 — skipping integration test")
	}
	conn.Close()
	return New(Config{URL: "http://127.0.0.1:8788", MinChars: 10, Timeout: 5 * time.Second})
}

func bigToolResult(n int) string {
	b := []byte(`{"status":"ok","items":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, []byte(`{"id":`+itoa(i)+`,"name":"item-`+itoa(i)+`","value":`+itoa(i)+`}`)...)
	}
	b = append(b, ']', '}')
	return string(b)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}

func TestCompressProviderWritesBackToolOnly(t *testing.T) {
	c := headroomUp(t)
	inner := &mockProv{name: "deepseek"}
	wp := Wrap(inner, c, nil)

	big := bigToolResult(500) // 数 KB，超 MinChars=10
	small := `{"ok":true}`    // 低于阈值，不压
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "You are a coding agent."},
		{Role: provider.RoleUser, Content: "检查一下"},
		{Role: provider.RoleAssistant, Content: "好的", ToolCalls: []provider.ToolCall{{ID: "c1", Name: "bash"}}},
		{Role: provider.RoleTool, ToolCallID: "c1", Content: big},
		{Role: provider.RoleTool, ToolCallID: "c2", Content: small},
	}
	beforeBig := len(big)

	if _, err := wp.Stream(context.Background(), provider.Request{Messages: msgs}); err != nil {
		t.Fatalf("Stream: %v", err)
	}

	got := inner.got
	if len(got) != 5 {
		t.Fatalf("消息数变化: got %d, want 5", len(got))
	}
	// 结构保留
	if got[3].Role != provider.RoleTool || got[3].ToolCallID != "c1" {
		t.Fatalf("tool 消息结构丢失: %+v", got[3])
	}
	// 大 tool 结果被压缩
	if len(got[3].Content) >= beforeBig {
		t.Fatalf("大 tool 结果未被压缩: before=%d after=%d", beforeBig, len(got[3].Content))
	}
	// 小 tool 结果不压
	if got[4].Content != small {
		t.Fatalf("小 tool 结果不应被压: %q", got[4].Content)
	}
	// system/user/assistant 原样
	if got[0].Content != "You are a coding agent." || got[1].Content != "检查一下" || got[2].Content != "好的" {
		t.Fatalf("system/user/assistant 被改动: %+v", got[:3])
	}
	// 写回共享切片：原 msgs 也被压缩（证明前缀稳定）
	if len(msgs[3].Content) != len(got[3].Content) {
		t.Fatalf("未写回原切片: session=%d wire=%d", len(msgs[3].Content), len(got[3].Content))
	}
}

func TestCompressProviderFailOpen(t *testing.T) {
	// 指向一个不存在的地址 → Compress 失败 → 原样转发
	c := New(Config{URL: "http://127.0.0.1:1", MinChars: 10, Timeout: 500 * time.Millisecond})
	inner := &mockProv{name: "deepseek"}
	wp := Wrap(inner, c, nil)

	big := bigToolResult(500)
	msgs := []provider.Message{{Role: provider.RoleTool, ToolCallID: "c1", Content: big}}
	if _, err := wp.Stream(context.Background(), provider.Request{Messages: msgs}); err != nil {
		t.Fatalf("fail-open 不应报错: %v", err)
	}
	if inner.got[0].Content != big {
		t.Fatal("失败时应原样转发未压缩内容")
	}
	if wp.FailureCount() != 1 {
		t.Fatalf("FailureCount = %d, want 1", wp.FailureCount())
	}
}

func TestNoCandidateSkipsCompression(t *testing.T) {
	// client 指向坏地址，但没有候选消息时不应触发任何请求
	c := New(Config{URL: "http://127.0.0.1:1", MinChars: 10, Timeout: 100 * time.Millisecond})
	inner := &mockProv{name: "deepseek"}
	wp := Wrap(inner, c, nil)
	msgs := []provider.Message{{Role: provider.RoleUser, Content: "hi"}}
	if _, err := wp.Stream(context.Background(), provider.Request{Messages: msgs}); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if wp.AttemptCount() != 0 || wp.FailureCount() != 0 {
		t.Fatalf("无候选时不应触发压缩: attempts=%d failures=%d", wp.AttemptCount(), wp.FailureCount())
	}
}
