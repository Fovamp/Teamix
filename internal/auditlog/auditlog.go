// Package auditlog 实现 AI 调用审计日志（操作流向，泄露审计）。
//
// 每次模型请求一条 JSONL 记录，按 用户×天 分文件落盘：
//   .teamix/logs/ai-audit/<user>/<date>.jsonl
//
// 仅 architect 角色可查（serve 层权限校验，P1 提供查询接口）。
// 泄露三信号（供查询层标红）：
//   - Outbound.Sent=true：该请求出了网
//   - Sensitivity 非 public 且 Sent=true：事故（本该拦截的内容出去了）
//   - Alerts 含 closed_loop_detected：假名化漏了（真实值出网）
package auditlog

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Step 是决策链上的一步（route_reason 可观测）。
type Step struct {
	Step   string `json:"step"`
	Level  string `json:"level,omitempty"`  // 敏感档位
	Source string `json:"source,omitempty"` // 判定来源（mcp/skill/path/default）
	Tokens int    `json:"tokens,omitempty"`
	Window int    `json:"window,omitempty"`
	Model  string `json:"model,omitempty"`
	Reason string `json:"reason,omitempty"` // route_reason
}

// Outbound 描述这次请求的出网信息。
type Outbound struct {
	Sent         bool     `json:"sent"`                   // 是否出网（关键：泄露审计核心信号）
	Redacted     bool     `json:"redacted,omitempty"`     // 是否假名化后出网
	RulesHit     []string `json:"pseudonym_rules_hit,omitempty"`
	MappingCount int      `json:"mapping_count,omitempty"`
	Bytes        int      `json:"bytes,omitempty"`
}

// Cost 是本次请求的成本计量（P1 填充完整 token/费用）。
type Cost struct {
	InTokens  int     `json:"in_tokens,omitempty"`
	OutTokens int     `json:"out_tokens,omitempty"`
	EstUSD    float64 `json:"est_usd,omitempty"`
}

// Record 是一条完整的审计记录。
type Record struct {
	RequestID   string    `json:"request_id"`
	SessionID   string    `json:"session_id,omitempty"`
	User        string    `json:"user,omitempty"`
	Time        time.Time `json:"time"`
	Purpose     string    `json:"purpose"`
	Sensitivity string    `json:"sensitivity,omitempty"` // 数据敏感档位
	Trace       []Step    `json:"trace"`
	Outbound    Outbound  `json:"outbound"`
	Cost        Cost      `json:"cost,omitempty"`
	Alerts      []string  `json:"alerts,omitempty"`
}

// Writer 是异步 JSONL 审计写入器：Write 投递到队列，后台 goroutine 落盘，
// 不阻塞请求路径。队列满时丢弃并累计 Dropped（防请求路径被拖垮）。
type Writer struct {
	dir           string
	retentionDays int

	jobs    chan Record
	dropped atomic.Uint64

	mu      sync.Mutex
	files   map[string]*fileBuf // key: user/date
	closer  chan struct{}
	done    chan struct{}
	once    sync.Once
}

// fileBuf 持有一个审计文件：文件句柄 + 缓冲写入器（Close 时 flush 并关句柄）。
type fileBuf struct {
	f  *os.File
	bw *bufio.Writer
}

// New 创建审计写入器并启动后台落盘 goroutine。dir 为审计根目录
// （默认 .teamix/logs/ai-audit），retentionDays<=0 表示不清理旧文件。
func New(dir string, retentionDays int) *Writer {
	w := &Writer{
		dir:           dir,
		retentionDays: retentionDays,
		jobs:          make(chan Record, 256),
		files:         make(map[string]*fileBuf),
		closer:        make(chan struct{}),
		done:          make(chan struct{}),
	}
	if w.dir == "" {
		w.dir = ".teamix/logs/ai-audit"
	}
	w.cleanupOnce()
	go w.loop()
	return w
}

// Write 投递一条记录（异步；队列满则丢弃并计数，不影响请求）。
func (w *Writer) Write(r Record) {
	if w == nil {
		return
	}
	select {
	case w.jobs <- r:
	default:
		w.dropped.Add(1)
	}
}

// Dropped 返回因队列满被丢弃的记录数（监控告警用）。
func (w *Writer) Dropped() uint64 { return w.dropped.Load() }

// Close 停止后台 goroutine 并 flush + 关闭所有文件句柄。
func (w *Writer) Close() error {
	w.once.Do(func() {
		close(w.closer)
		<-w.done
		w.mu.Lock()
		defer w.mu.Unlock()
		for _, fb := range w.files {
			_ = fb.bw.Flush()
			_ = fb.f.Close()
		}
		w.files = make(map[string]*fileBuf)
	})
	return nil
}

func (w *Writer) loop() {
	defer close(w.done)
	for {
		select {
		case r := <-w.jobs:
			w.append(r)
		case <-w.closer:
			// 退出前排空剩余队列，避免 Close 时丢最后几条记录
			for {
				select {
				case r := <-w.jobs:
					w.append(r)
				default:
					return
				}
			}
		}
	}
}

// fileKey 生成 用户×天 分文件路径。
func (w *Writer) fileKey(r Record) string {
	user := r.User
	if user == "" {
		user = "_system"
	}
	return filepath.Join(w.dir, user, r.Time.Format("2006-01-02")+".jsonl")
}

func (w *Writer) append(r Record) {
	key := w.fileKey(r)
	line, err := marshal(r)
	if err != nil {
		return // 记录失败不阻塞；P1 告警
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	fb, ok := w.files[key]
	if !ok {
		if err := os.MkdirAll(filepath.Dir(key), 0o750); err != nil {
			return
		}
		f, err := os.OpenFile(key, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
		if err != nil {
			return
		}
		fb = &fileBuf{f: f, bw: bufio.NewWriter(f)}
		w.files[key] = fb
	}
	_, _ = fb.bw.Write(line)
	_ = fb.bw.WriteByte('\n')
	// 立即 flush：bufio 缓冲不落盘，teamix.exe 常驻时文件永远是 0 字节
	// （此前"Token 周报无数据"根因——只有 Close 时才 flush，进程不退不写）。
	_ = fb.bw.Flush()
}

// cleanupOnce 启动时清理超过保留期的旧文件（按目录 mtime 简单判断）。
func (w *Writer) cleanupOnce() {
	if w.retentionDays <= 0 {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -w.retentionDays)
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return
	}
	for _, u := range entries {
		if !u.IsDir() {
			continue
		}
		userDir := filepath.Join(w.dir, u.Name())
		files, _ := os.ReadDir(userDir)
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			if info.ModTime().Before(cutoff) {
				_ = os.Remove(filepath.Join(userDir, f.Name()))
			}
		}
	}
}

func marshal(r Record) ([]byte, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("auditlog marshal: %w", err)
	}
	return b, nil
}
