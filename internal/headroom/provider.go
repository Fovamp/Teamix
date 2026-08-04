package headroom

import (
	"context"
	"log"
	"sync/atomic"

	"reasonix/internal/provider"
)

// elog 是降级告警日志（默认标准库 log，输出到 stderr）。
var elog = log.Default()

// OnStats 是压缩统计回调（boot 侧接到 event sink，推前端展示）。
type OnStats func(s Stats)

// CompressProvider 包装底层 provider：Stream() 前把 role=tool 且超阈值的
// 消息交给 headroom 压缩，压缩结果就地写回 req.Messages（与 session.Messages
// 共享底层数组）→ 下一轮前缀字节与本轮一致，DeepSeek 前缀缓存继续命中。
//
// 任何压缩失败都 fail-open：原样转发底层 provider，用户请求绝不受影响。
type CompressProvider struct {
	inner provider.Provider
	c     *Client
	stats OnStats

	// 累计统计（供降级判断/诊断）
	attempts atomic.Int64
	failures atomic.Int64
}

// Wrap 返回一个带压缩的 provider 包装。stats 可为 nil。
func Wrap(inner provider.Provider, c *Client, stats OnStats) *CompressProvider {
	return &CompressProvider{inner: inner, c: c, stats: stats}
}

// Name 透传底层 provider 名称。
func (p *CompressProvider) Name() string { return p.inner.Name() }

// FailureCount 返回累计失败次数（连续失败可触发降级告警）。
func (p *CompressProvider) FailureCount() int64 { return p.failures.Load() }

// AttemptCount 返回累计压缩尝试次数。
func (p *CompressProvider) AttemptCount() int64 { return p.attempts.Load() }

// Stream 实现 provider.Provider：压缩 tool 结果后转发。
func (p *CompressProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	req.Messages = p.compress(ctx, req.Messages)
	return p.inner.Stream(ctx, req)
}

// compress 挑出可压缩的 tool 消息批量压缩并写回。
func (p *CompressProvider) compress(ctx context.Context, msgs []provider.Message) []provider.Message {
	if p.c == nil || len(msgs) == 0 {
		return msgs
	}
	minChars := p.c.MinChars()

	// 收集候选：role=tool、纯文本、体积超阈值。
	type candidate struct {
		idx int
		msg provider.Message
	}
	var cands []candidate
	for i := range msgs {
		m := &msgs[i]
		if m.Role != provider.RoleTool {
			continue
		}
		if len(m.Content) < minChars || len(m.Images) > 0 {
			continue // 太小不值得压；带图消息不压
		}
		cands = append(cands, candidate{idx: i, msg: *m})
	}
	if len(cands) == 0 {
		return msgs
	}

	p.attempts.Add(1)
	before := make([]provider.Message, len(cands))
	for i, c := range cands {
		before[i] = c.msg
	}

	compressed, stats, err := p.c.Compress(ctx, before)
	if err != nil {
		p.failures.Add(1)
		f := p.failures.Load()
		// 降级可观测：首次失败和每 50 次失败记一条日志，其余静默（fail-open 不打扰用户）。
		if f == 1 || f%50 == 0 {
			elog.Printf("teamix: headroom compression failed (%d consecutive) — requests continue uncompressed: %v", f, err)
		}
		return msgs // fail-open：原样转发
	}

	// 写回：headroom 返回的消息按序贴回原位置（保留 tool_call_id 等结构）。
	if len(compressed) == len(cands) {
		for i, c := range cands {
			msgs[c.idx].Content = compressed[i].Content
		}
	}
	if p.stats != nil {
		p.stats(stats)
	}
	return msgs
}

// _ 断言实现 provider.Provider。
var _ provider.Provider = (*CompressProvider)(nil)
