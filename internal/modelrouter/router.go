// Package modelrouter 实现双模型协作的路由网关（RouterProvider）。
//
// 挂在 boot.Options.WrapProvider 外层，与 headroom 同级可叠加。职责：
//   - 按用途分工（execute/plan/subagent/classify/guard/compress/cheap）
//   - 窗口预检（超内部池窗口且非强制内部时升级外部池）
//   - 首 chunk 前失败自动降级（外部⇄内部）
//   - fail-closed：路由层异常一律回内部池（安全层后续任务接入）
//
// 安全层（敏感判定/假名化/入参扫描）是独立组件，在 P1 接入，不在此包。
package modelrouter

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"reasonix/internal/provider"
)

// Kind 模型池类型。
type Kind string

const (
	KindInternal Kind = "internal" // 本地/内网模型池（Qwen），数据不出内网
	KindExternal Kind = "external" // 云端 API 模型池（DeepSeek）
)

// route_reason 常量——每次路由决策记录一条，供审计与排查。
const (
	ReasonRuleExternal     = "rule_execute_external"      // 用途规则 → 外部池
	ReasonRuleInternal     = "rule_role_internal"         // 用途规则 → 内部池
	ReasonGuardInternal    = "rule_guard_internal"        // guardian/compress 强制内部
	ReasonContextOverflow  = "context_overflow_to_external" // 超内部窗口 → 外部
	ReasonOffline          = "offline_internal"           // 离线模式 → 内部
	ReasonFailClosed       = "fail_closed_internal"       // 路由层异常/无池 → 内部兜底
	ReasonFallbackInternal = "fallback_external_to_internal" // 外部首 chunk 失败 → 内部
	ReasonFallbackExternal = "fallback_internal_to_external" // 内部首 chunk 失败 → 外部
)

// Pool 是一个模型池：内部池或外部池，含一个或多个候选 provider 实例。
type Pool struct {
	Kind           Kind
	Name           string // 池名，如 "qwen" / "deepseek"
	Providers      []provider.Provider
	Roles          map[provider.Purpose]bool
	MaxInputTokens int // 窗口预检阈值；<=0 表示不预检
	MaxParallel    int // 并发闸；<=0 表示不限

	initOnce sync.Once
	sem      chan struct{}
}

func (p *Pool) ensure() {
	p.initOnce.Do(func() {
		if p.MaxParallel > 0 {
			p.sem = make(chan struct{}, p.MaxParallel)
		}
	})
}

// Config 是路由配置。Internal/External 可独立为空（对应池不可用时 fail-closed 兜底）。
type Config struct {
	Internal    *Pool
	External    *Pool
	RoleDefault map[provider.Purpose]Kind // 用途 → 默认池；缺省按 execute→external（若存在）处理
	Offline     bool                      // 离线模式：全走内部（手动"全走 Qwen"唯一入口）

	// 安全层雏形（P0）：数据敏感级裁决 + 入参扫描 + System Prompt 分级标记。
	// 与路由层分离的完整 SecurityGate 在 P1 接入，此处提供不可绕过的强制内部通道。
	Sensitive          *SensitiveRules
	InternalPromptMarker string // system 消息含此标记 → 强制内部（空 = 不启用）

	// 假名化（P1）：PseudoDict 为字典映射（真实值 → 保形假名，如 张三→客户甲）。
	// redact 敏感级出网时自动应用；正则类（手机号/身份证/邮箱/密钥/日期/金额）
	// 内置启用。空字典 = 只启用正则类。
	PseudoDict map[string]string

	// QuotaCheck 外部池可用性检查（配额/预算门禁）：nil = 不限。
	// 返回 false 时外部请求**柔性降级**到内部池（ReasonQuota），不报错。
	QuotaCheck func() bool

	// EasyThreshold 难度路由阈值（P2 轻量启发式）：execute 请求估计 token 数
	// 低于此值且无工具调用 → 判定"简单"→ 降级内部池省成本（ReasonDifficulty）。
	// <=0 禁用。
	EasyThreshold int
}

// ReasonQuota = 配额/预算超限，外部请求柔性降级内部。
const ReasonQuota = "quota_exceeded_fallback_internal"

// ReasonDifficulty = 难度路由（轻量启发式）：简单 execute 请求（短、无工具调用）
// 降级内部池省成本（RouteLLM 思想的简化版：只降级不升级，保守）。
const ReasonDifficulty = "difficulty_route_internal"

// Decision 是一次请求的路由决策结果（审计/route_reason 用）。
type Decision struct {
	RequestID      string
	Purpose        provider.Purpose
	Sensitivity    provider.Sensitivity // 请求携带的数据敏感级（空 = 未标记）
	Pool           Kind
	Model          string // 池名
	Reason         string // route_reason
	EstimatedTokens int
}

// RouterProvider 实现 provider.Provider，按用途/窗口路由到内部或外部池。
type RouterProvider struct {
	cfg Config
	id  atomic.Uint64

	// OnDecision 是审计回调（P0 骨架：埋点；P1 接 JSONL 落盘 + 泄露三信号）。
	OnDecision func(Decision)
	// pseudo 会话级假名化器：redact 出网时对请求假名化、对输出还原。
	pseudo *Pseudonymizer
	// OnRestoreFail 输出还原校验失败回调（模型幻觉新假名 → 审计告警）。
	// 参数：requestID（关联原请求，全链路 trace）+ issue。
	OnRestoreFail func(requestID, issue string)
}

func New(cfg Config) *RouterProvider {
	if cfg.Internal != nil {
		cfg.Internal.ensure()
	}
	if cfg.External != nil {
		cfg.External.ensure()
	}
	r := &RouterProvider{cfg: cfg, pseudo: NewPseudonymizer()}
	if cfg.PseudoDict != nil {
		for real, fake := range cfg.PseudoDict {
			r.pseudo.AddDict(real, fake)
		}
	}
	return r
}

func (r *RouterProvider) Name() string { return "model-router" }

// Stream 路由一次流式请求。返回的 channel 可能包含一次静默降级（首 chunk 失败）。
// redact 敏感级且目标外部池时：请求先假名化（出网副本），输出流还原为真实值。
func (r *RouterProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	d, err := r.decide(req)
	if err != nil {
		return nil, err
	}
	r.emit(d.Decision)

	pool := d.pool
	// redact 出网：生成临时假名副本（本地上下文保持真实值，禁止写回 messages）
	redacted := req.Sensitivity == provider.SensitivityRedact && pool.Kind == KindExternal && r.pseudo != nil
	orig := req
	if redacted {
		req.Messages = r.pseudo.ApplyMessages(req.Messages)
	}
	ch, err := r.streamPool(ctx, req, pool)
	if err != nil {
		// 启动即失败 → 降级到另一池（fail-closed 语义）
		if alt := r.altPool(d); alt != nil {
			d2 := d.withPool(alt, reasonForFallback(pool.Kind))
			r.emit(d2.Decision)
			return r.streamPool(ctx, req, alt)
		}
		return nil, err
	}
	if redacted {
		// 闭环 Fail-Close：缓冲外部输出 → 还原校验 → 失败丢弃并回退内部重生成（原始请求）
		return r.streamRedactClosedLoop(ctx, orig, req, pool, d.RequestID), nil
	}
	// 包装 channel：首 chunk 为 ChunkError 时静默降级
	return r.wrapFirstChunk(ctx, req, d, ch), nil
}

// streamRedactClosedLoop 是 redact 出网的闭环路径：
//  1. 缓冲外部模型完整输出（牺牲流式实时性，安全优先）
//  2. 假名还原 + 校验；校验失败（模型幻觉新假名/输出含未假名敏感值）→
//     丢弃污染输出，回退内部池用原始请求重新生成（fail-close，前端隐藏降级）
//  3. 校验通过 → 还原后的文本一次性转发
func (r *RouterProvider) streamRedactClosedLoop(ctx context.Context, orig, reqRedacted provider.Request, pool *Pool, requestID string) <-chan provider.Chunk {
	out := make(chan provider.Chunk)
	go func() {
		defer close(out)
		ch, err := r.streamPool(ctx, reqRedacted, pool)
		if err != nil {
			r.closedLoopFallback(ctx, orig, out, requestID)
			return
		}
		var buf strings.Builder
		streamed := false
		for c := range ch {
			switch c.Type {
			case provider.ChunkText:
				buf.WriteString(c.Text)
			case provider.ChunkToolCall, provider.ChunkToolCallStart, provider.ChunkToolCallArgsDelta:
				// redact 场景模型一般不回工具调用；若出现则透传并记录告警（args 还原 P2）
				if r.OnRestoreFail != nil {
					r.OnRestoreFail(requestID, "tool_call_in_redact_stream")
				}
				if buf.Len() > 0 {
					out <- provider.Chunk{Type: provider.ChunkText, Text: r.pseudo.Restore(buf.String())}
					buf.Reset()
				}
				streamed = true
				out <- c
			case provider.ChunkError:
				r.closedLoopFallback(ctx, orig, out, requestID)
				return
			}
		}
		if streamed && buf.Len() > 0 {
			out <- provider.Chunk{Type: provider.ChunkText, Text: r.pseudo.Restore(buf.String())}
			return
		}
		restored, ok := r.pseudo.RestoreWithCheck(buf.String())
		if !ok {
			if r.OnRestoreFail != nil {
				r.OnRestoreFail(requestID, "closed_loop_detected: "+restored)
			}
			r.closedLoopFallback(ctx, orig, out, requestID)
			return
		}
		if restored != "" {
			out <- provider.Chunk{Type: provider.ChunkText, Text: restored}
		}
	}()
	return out
}

// closedLoopFallback 回退内部池用原始请求（未假名化）重新生成，并记录审计。
func (r *RouterProvider) closedLoopFallback(ctx context.Context, orig provider.Request, out chan<- provider.Chunk, requestID string) {
	internal := r.cfg.Internal
	if internal == nil {
		out <- provider.Chunk{Type: provider.ChunkError, Err: fmt.Errorf("modelrouter: closed-loop fallback requires internal pool")}
		return
	}
	if r.OnDecision != nil {
		r.OnDecision(Decision{
			RequestID:      requestID,
			Purpose:        orig.Purpose,
			Reason:         ReasonClosedLoopFallback,
			Pool:           KindInternal,
			Model:          internal.Name,
			EstimatedTokens: EstimateTokens(orig.Messages),
		})
	}
	ch, err := r.streamPool(ctx, orig, internal)
	if err != nil {
		out <- provider.Chunk{Type: provider.ChunkError, Err: err}
		return
	}
	for c := range ch {
		out <- c
	}
}

// decision 是内部决策载体（含选定池）。
type decision struct {
	Decision
	pool *Pool
}

func (d decision) withPool(p *Pool, reason string) decision {
	d2 := d
	d2.pool = p
	d2.Pool = p.Kind
	d2.Model = p.Name
	d2.Reason = reason
	return d2
}

func (r *RouterProvider) decide(req provider.Request) (decision, error) {
	purpose := req.Purpose
	if purpose == "" {
		purpose = provider.PurposeExecute
	}
	est := EstimateTokens(req.Messages)
	rid := fmt.Sprintf("r-%d", r.id.Add(1))

	// 强制内部用途：guardian/compress（内容天然敏感，fail-closed）
	if purpose == provider.PurposeGuard || purpose == provider.PurposeCompress {
		if r.cfg.Internal != nil {
			return decision{Decision: Decision{RequestID: rid, Sensitivity: req.Sensitivity, Purpose: purpose, Pool: KindInternal, Model: r.cfg.Internal.Name, Reason: ReasonGuardInternal, EstimatedTokens: est}, pool: r.cfg.Internal}, nil
		}
		return decision{}, fmt.Errorf("modelrouter: %s requires internal pool but none configured", purpose)
	}

	// 安全层裁决（独立于路由规则，不可绕过）：非 public 敏感级 / 入参扫描命中 /
	// system 提示词含内部标记 → 一律强制内部池（fail-closed）。
	if reason := r.securityForceReason(req); reason != "" {
		if r.cfg.Internal != nil {
			return decision{Decision: Decision{RequestID: rid, Sensitivity: req.Sensitivity, Purpose: purpose, Pool: KindInternal, Model: r.cfg.Internal.Name, Reason: reason, EstimatedTokens: est}, pool: r.cfg.Internal}, nil
		}
		return decision{}, fmt.Errorf("modelrouter: sensitive content requires internal pool but none configured")
	}

	// 离线模式：全走内部
	if r.cfg.Offline {
		if r.cfg.Internal != nil {
			return decision{Decision: Decision{RequestID: rid, Sensitivity: req.Sensitivity, Purpose: purpose, Pool: KindInternal, Model: r.cfg.Internal.Name, Reason: ReasonOffline, EstimatedTokens: est}, pool: r.cfg.Internal}, nil
		}
	}

	// 用途默认规则
	kind, ok := r.cfg.RoleDefault[purpose]
	if !ok {
		kind = KindExternal
		if r.cfg.External == nil {
			kind = KindInternal
		}
	}
	pool := r.poolOf(kind)
	if pool == nil {
		// fail-closed 兜底：目的池缺失 → 内部池（无内部则报错，绝不裸奔到不存在的池）
		pool = r.cfg.Internal
		if pool == nil {
			return decision{}, fmt.Errorf("modelrouter: no pool for purpose %s (fail-closed)", purpose)
		}
		kind = KindInternal
	}

	// 配额门禁：外部池不可用（配额超限）→ 柔性降级内部池（不报错，用户无感）
	if kind == KindExternal && r.cfg.QuotaCheck != nil && !r.cfg.QuotaCheck() {
		if r.cfg.Internal != nil {
			return decision{Decision: Decision{RequestID: rid, Sensitivity: req.Sensitivity, Purpose: purpose, Pool: KindInternal, Model: r.cfg.Internal.Name, Reason: ReasonQuota, EstimatedTokens: est}, pool: r.cfg.Internal}, nil
		}
	}

	// 难度路由（轻量启发式）：简单 execute 请求（token 低于阈值且无工具调用）
	// → 降级内部池省成本；复杂/带工具调用保持外部。只降级不升级（保守）。
	if kind == KindExternal && r.cfg.EasyThreshold > 0 && purpose == provider.PurposeExecute &&
		est < r.cfg.EasyThreshold && !hasToolCalls(req.Messages) && r.cfg.Internal != nil {
		return decision{Decision: Decision{RequestID: rid, Sensitivity: req.Sensitivity, Purpose: purpose, Pool: KindInternal, Model: r.cfg.Internal.Name, Reason: ReasonDifficulty, EstimatedTokens: est}, pool: r.cfg.Internal}, nil
	}

	// 窗口预检：目标内部池但内容超窗 → 升级外部池（非强制内部场景）
	if kind == KindInternal && r.cfg.External != nil && pool.MaxInputTokens > 0 && est > pool.MaxInputTokens {
		return decision{Decision: Decision{RequestID: rid, Sensitivity: req.Sensitivity, Purpose: purpose, Pool: KindExternal, Model: r.cfg.External.Name, Reason: ReasonContextOverflow, EstimatedTokens: est}, pool: r.cfg.External}, nil
	}

	return decision{Decision: Decision{RequestID: rid, Sensitivity: req.Sensitivity, Purpose: purpose, Pool: kind, Model: pool.Name, Reason: reasonFor(kind), EstimatedTokens: est}, pool: pool}, nil
}

func (r *RouterProvider) poolOf(kind Kind) *Pool {
	if kind == KindInternal {
		return r.cfg.Internal
	}
	return r.cfg.External
}

// altPool 返回降级目标池（外部→内部；内部→外部），不存在则 nil。
func (r *RouterProvider) altPool(d decision) *Pool {
	if d.pool == nil {
		return r.cfg.Internal
	}
	if d.pool.Kind == KindExternal {
		return r.cfg.Internal
	}
	return r.cfg.External
}

func reasonFor(kind Kind) string {
	if kind == KindInternal {
		return ReasonRuleInternal
	}
	return ReasonRuleExternal
}

func reasonForFallback(from Kind) string {
	if from == KindExternal {
		return ReasonFallbackInternal
	}
	return ReasonFallbackExternal
}

// streamPool 从池内选 provider（P0 取首个候选）并持并发闸发起流。
func (r *RouterProvider) streamPool(ctx context.Context, req provider.Request, pool *Pool) (<-chan provider.Chunk, error) {
	pool.ensure()
	if len(pool.Providers) == 0 {
		return nil, fmt.Errorf("modelrouter: pool %s has no providers", pool.Name)
	}
	if pool.sem != nil {
		select {
		case pool.sem <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	// 故障分层：外部池短时抖动自动重试最多 2 次（仅非机密请求）；
	// 机密/假名化请求（internal/confidential/redact）禁止重试——防重复传输敏感数据。
	maxRetries := 0
	if pool.Kind == KindExternal && (req.Sensitivity == "" || req.Sensitivity == provider.SensitivityPublic) {
		maxRetries = 2
	}
	p := pool.Providers[0]
	for attempt := 0; ; attempt++ {
		ch, err := p.Stream(ctx, req)
		if err == nil {
			// 流生命周期结束（channel 关闭）时释放并发闸
			out := make(chan provider.Chunk)
			go func() {
				defer func() {
					if pool.sem != nil {
						<-pool.sem
					}
				}()
				for c := range ch {
					out <- c
				}
				close(out)
			}()
			return out, nil
		}
		if attempt >= maxRetries {
			if pool.sem != nil {
				<-pool.sem
			}
			return nil, err
		}
	}
}

// wrapFirstChunk 转发 chunk；若首 chunk 为 ChunkError 且存在另一池，静默降级。
func (r *RouterProvider) wrapFirstChunk(ctx context.Context, req provider.Request, d decision, ch <-chan provider.Chunk) <-chan provider.Chunk {
	out := make(chan provider.Chunk)
	go func() {
		defer close(out)
		first, ok := <-ch
		if !ok {
			return
		}
		if first.Type == provider.ChunkError && first.Err != nil {
			if alt := r.altPool(d); alt != nil {
				d2 := d.withPool(alt, reasonForFallback(d.pool.Kind))
				r.emit(d2.Decision)
				ch2, err := r.streamPool(ctx, req, alt)
				if err != nil {
					out <- provider.Chunk{Type: provider.ChunkError, Err: err}
					return
				}
				for c := range ch2 {
					out <- c
				}
				return
			}
		}
		out <- first
		for c := range ch {
			out <- c
		}
	}()
	return out
}

func (r *RouterProvider) emit(d Decision) {
	if r.OnDecision != nil {
		r.OnDecision(d)
	}
}

// BootConfig 是 boot 层集成载体：由宿主（serve）构建，boot 在 buildProv 时
// 挂到所有模型调用外层。external 为会话当前模型 provider（可已被 headroom
// 包装）；internal 为本地/内网模型池（Qwen），nil 时该池不可用（fail-closed）。
type BootConfig struct {
	Internal             provider.Provider
	Offline              bool
	Sensitive            *SensitiveRules
	InternalPromptMarker string
	Audit                func(Decision)
	// 假名化字典（真实值 → 保形假名）；redact 出网时应用。
	PseudoDict map[string]string
	// OnRestoreFail 输出还原校验失败回调（requestID + issue，审计告警/全链路 trace）。
	OnRestoreFail func(requestID, issue string)
	// QuotaCheck 外部池可用性检查（配额门禁），nil = 不限。
	QuotaCheck func() bool
}

// Wrap 构造 RouterProvider 并包住 external provider（当前模型）。
// 无内部池时仍可工作：强制内部用途/敏感内容会 fail-closed 报错。
func (bc *BootConfig) Wrap(external provider.Provider) provider.Provider {
	cfg := Config{
		RoleDefault: map[provider.Purpose]Kind{
			provider.PurposeExecute:  KindExternal,
			provider.PurposePlan:     KindInternal,
			provider.PurposeSubagent: KindInternal,
			provider.PurposeClassify: KindInternal,
			provider.PurposeCheap:    KindInternal,
		},
		Offline:              bc.Offline,
		Sensitive:            bc.Sensitive,
		InternalPromptMarker: bc.InternalPromptMarker,
		PseudoDict:           bc.PseudoDict,
		QuotaCheck:           bc.QuotaCheck,
	}
	if bc.Internal != nil {
		cfg.Internal = &Pool{Kind: KindInternal, Name: "internal", Providers: []provider.Provider{bc.Internal}}
	}
	if external != nil {
		cfg.External = &Pool{Kind: KindExternal, Name: "external", Providers: []provider.Provider{external}}
	}
	r := New(cfg)
	if bc.Audit != nil {
		r.OnDecision = bc.Audit
	}
	if bc.OnRestoreFail != nil {
		r.OnRestoreFail = bc.OnRestoreFail
	}
	return r
}
