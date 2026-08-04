// Package headroom 实现 Teamix 与官方 headroom（headroomlabs-ai/headroom，Python）
// 的 HTTP 集成：通过 headroom proxy 的 POST /v1/compress 压缩 tool 结果消息。
//
// 设计要点：
//   - 只压缩 role=tool 且体积超阈值的消息，system/user/assistant 不碰 → DeepSeek 前缀缓存稳定
//   - 压缩结果就地写回 req.Messages（与 session.Messages 共享底层数组）→ 下一轮前缀字节一致
//   - 任何失败一律 fail-open：不压缩、原样转发，用户请求绝不被压缩服务拖垮
package headroom

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"reasonix/internal/provider"
)

// Config 是 headroom 压缩层的配置（来源：.teamix/config.yaml 的 headroom 段）。
type Config struct {
	Enabled  bool
	URL      string // 如 http://127.0.0.1:8788（headroom proxy 地址）
	Model    string // 传给 headroom 用于 token 估算的模型名；空则 headroom 自行选择
	MinChars int    // content 小于该字节数的 tool 消息不压缩
	Timeout  time.Duration
}

// Stats 是一次压缩的统计，供 usage 事件/前端展示。
type Stats struct {
	Compressed   int // 实际被压缩的消息条数
	BeforeBytes  int
	AfterBytes   int
	TokensBefore int
	TokensAfter  int
}

// Client 是 headroom proxy 的 /v1/compress 客户端。零值不可用，用 New 构造。
type Client struct {
	baseURL  string
	model    string
	minChars int
	hc       *http.Client
}

// New 构造 Client。cfg.Timeout <= 0 时用默认 5s；cfg.Model 为空时用
// "deepseek-chat"（headroom 用 model 做 token 估算，字段必填，不影响压缩本身）。
func New(cfg Config) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	minChars := cfg.MinChars
	if minChars <= 0 {
		minChars = 2000
	}
	model := cfg.Model
	if model == "" {
		model = "deepseek-chat"
	}
	return &Client{
		baseURL:  cfg.URL,
		model:    model,
		minChars: minChars,
		hc:       &http.Client{Timeout: timeout},
	}
}

// MinChars 返回压缩体积阈值（供 wrapper 过滤）。
func (c *Client) MinChars() int { return c.minChars }

// 与官方 /v1/compress 协议对应的请求/响应结构（字段名实测于 0.33.0）。
type compressRequest struct {
	Messages []provider.Message `json:"messages"`
	Model    string             `json:"model,omitempty"`
	Config   compressConfig     `json:"config,omitempty"`
}

type compressConfig struct {
	CompressUserMessages bool `json:"compress_user_messages,omitempty"`
	ProtectRecent        int  `json:"protect_recent,omitempty"`
}

type compressResponse struct {
	Messages           []provider.Message `json:"messages"`
	TokensBefore       int                `json:"tokens_before"`
	TokensAfter        int                `json:"tokens_after"`
	TokensSaved        int                `json:"tokens_saved"`
	CompressionRatio   float64            `json:"compression_ratio"`
	TransformsApplied  []string           `json:"transforms_applied"`
	CompressionSkipped bool               `json:"compression_skipped"`
}

// Compress 把一组消息发给 headroom 压缩，返回压缩后的消息与统计。
// 失败（网络/超时/非 2xx）返回错误，调用方必须 fail-open。
func (c *Client) Compress(ctx context.Context, msgs []provider.Message) ([]provider.Message, Stats, error) {
	if len(msgs) == 0 {
		return msgs, Stats{}, nil
	}
	reqBody, err := json.Marshal(compressRequest{
		Messages: msgs,
		Model:    c.model,
		Config: compressConfig{
			CompressUserMessages: false, // tool 结果之外的都保持原样
			ProtectRecent:        0,
		},
	})
	if err != nil {
		return nil, Stats{}, fmt.Errorf("headroom: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/compress", bytes.NewReader(reqBody))
	if err != nil {
		return nil, Stats{}, fmt.Errorf("headroom: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, Stats{}, fmt.Errorf("headroom: post /v1/compress: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, Stats{}, fmt.Errorf("headroom: /v1/compress returned %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, Stats{}, fmt.Errorf("headroom: read response: %w", err)
	}
	var cr compressResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return nil, Stats{}, fmt.Errorf("headroom: decode response: %w", err)
	}
	if cr.Messages == nil {
		// fail-open：服务端显式跳过了压缩
		return msgs, Stats{}, nil
	}
	stats := Stats{
		Compressed:   len(msgs),
		TokensBefore: cr.TokensBefore,
		TokensAfter:  cr.TokensAfter,
	}
	for _, m := range msgs {
		stats.BeforeBytes += len(m.Content)
	}
	for _, m := range cr.Messages {
		stats.AfterBytes += len(m.Content)
	}
	return cr.Messages, stats, nil
}
