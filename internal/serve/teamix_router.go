package serve

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/auditlog"
	"reasonix/internal/frontmatter"
	"reasonix/internal/modelrouter"
	"reasonix/internal/provider"
	"reasonix/internal/teamixconfig"

	"github.com/joho/godotenv"
)

// buildInternalProvider 从环境变量构建内部模型池 provider（本地 Qwen）。
// 未配置 QWEN_BASE_URL/QWEN_MODEL/QWEN_API_KEY 时返回 nil（内部池不可用，
// 路由 fail-closed：强制内部用途/敏感内容将报错而不是出网）。
func buildInternalProvider() provider.Provider {
	base := os.Getenv("QWEN_BASE_URL")
	model := os.Getenv("QWEN_MODEL")
	key := os.Getenv("QWEN_API_KEY")
	if base == "" || model == "" || key == "" {
		return nil
	}
	p, err := provider.New("openai", provider.Config{
		Name:    "qwen-internal",
		BaseURL: base,
		Model:   model,
		APIKey:  key,
	})
	if err != nil {
		slog.Warn("teamix: internal model provider build failed", "err", err)
		return nil
	}
	return p
}

// buildKeyPoolProvider 从 keypool 当前选中的 key 构建外部模型 provider。
// 绕过 Reasonix credential store，key 唯一来源 = 工作区 .reasonix/secrets/pool.yaml。
// keypool 为空时返回 nil。
func (ts *TeamixServer) buildKeyPoolProvider() provider.Provider {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		return nil
	}
	p, err := provider.New("openai", provider.Config{
		Name:    "deepseek-keypool",
		BaseURL: "https://api.deepseek.com",
		Model:   "deepseek-v4-pro",
		APIKey:  key,
	})
	if err != nil {
		slog.Warn("teamix: keypool external provider build failed", "err", err)
		return nil
	}
	return p
}

// refreshKeyPoolProvider 密钥池配置变更后热更新外部 provider（无需重新登录）。
func (ts *TeamixServer) refreshKeyPoolProvider() {
	ts.keyPool.Acquire()
	ts.externalProvider = ts.buildKeyPoolProvider()
	slog.Info("teamix: keypool provider refreshed", "hasExternal", ts.externalProvider != nil)
}

// loadTeamixEnv 按 Reasonix 安全模式读取项目 .env：只将 Teamix 需要的变量
// （QWEN_* / RAGFLOW_*）注入进程环境，不污染其他变量。
func loadTeamixEnv(workspaceRoot string) {
	data, err := os.ReadFile(filepath.Join(workspaceRoot, ".env"))
	if err != nil {
		return
	}
	// 兼容 UTF-8 BOM（\ufeff），godotenv 解析前需要剥离
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	envMap, err := godotenv.Unmarshal(string(data))
	if err != nil {
		slog.Warn("teamix: cannot parse project .env", "err", err)
		return
	}
	for k, v := range envMap {
		upper := strings.ToUpper(k)
		if strings.HasPrefix(upper, "QWEN_") || strings.HasPrefix(upper, "RAGFLOW_") {
			if os.Getenv(k) == "" {
				os.Setenv(k, v)
				slog.Info("teamix: loaded from project .env", "key", k)
			}
		}
	}
}

// mcpSensitivityMap 聚合全局+私有 MCP 的声明敏感级（server 名 → 档位）。
// 未声明（旧配置无字段）不加入 → 运行期按未声明处理（internal 兜底）。
func (ts *TeamixServer) mcpSensitivityMap(userRoot string) map[string]provider.Sensitivity {
	out := make(map[string]provider.Sensitivity)
	add := func(specs map[string]mcpServerSpec) {
		for name, srv := range specs {
			if s := provider.NormalizeSensitivity(srv.Sensitivity); s != "" {
				out[name] = s
			}
		}
	}
	add(ts.loadGlobalMCPServers())
	add(loadUserMCPServers(userRoot))
	return out
}

// mcpSensitivityOf 从 spec map 取一个 server 的声明敏感级（空 = 未声明）。
func mcpSensitivityOf(specs map[string]mcpServerSpec, name string) string {
	if srv, ok := specs[name]; ok {
		return srv.Sensitivity
	}
	return ""
}

// readFrontmatterSensitivity 从 markdown 文件 frontmatter 读 sensitivity 字段（空 = 未声明）。
func readFrontmatterSensitivity(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	fm, _ := frontmatter.Split(strings.TrimPrefix(string(b), "\uFEFF"))
	return strings.TrimSpace(fm["sensitivity"])
}

// scanDirSensitivity 扫描目录下所有 <name>/SKILL.md 与 *.md 的 frontmatter
// sensitivity，聚合最高档（只升不降）。目录不存在返回空。
func scanDirSensitivity(dir string) provider.Sensitivity {
	var base provider.Sensitivity
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if name == "SKILL.md" || strings.HasSuffix(name, ".md") {
			if s := provider.NormalizeSensitivity(readFrontmatterSensitivity(path)); s != "" {
				base = provider.MaxSensitivity(base, s)
			}
		}
		return nil
	})
	return base
}

// baseSensitivityFor 计算会话初始敏感级（只升不降）：加载的 skill 与记忆的
// 声明敏感级聚合。来源：
//   - skill：全局 .reasonix/skills/ + 用户私有 .reasonix/skills/
//   - 记忆：用户私有 .teamix/memory/<project>/private/ + 全局 .teamix/memory/<project>/
//     （project 为空 = 未选项目，记忆暂不参与）
//
// 未声明（空）不参与；声明 internal+ 的 skill/记忆使会话初始即锁内部。
func (ts *TeamixServer) baseSensitivityFor(userRoot, project string) provider.Sensitivity {
	var base provider.Sensitivity
	base = provider.MaxSensitivity(base, scanDirSensitivity(filepath.Join(ts.workspaceRoot, ".reasonix", "skills")))
	base = provider.MaxSensitivity(base, scanDirSensitivity(filepath.Join(userRoot, ".reasonix", "skills")))
	if project != "" {
		base = provider.MaxSensitivity(base, scanDirSensitivity(filepath.Join(userRoot, ".teamix", "memory", project, "private")))
		base = provider.MaxSensitivity(base, scanDirSensitivity(filepath.Join(ts.workspaceRoot, ".teamix", "memory", project)))
	}
	return base
}

// sensitiveRulesFromCfg 把 .teamix/config.yaml 的 sensitive 段（机密黑名单）
// 转为路由敏感规则；未配置时返回 nil（无强制拦截，保持原行为）。
func sensitiveRulesFromCfg(cfg *teamixconfig.Config) *modelrouter.SensitiveRules {
	if cfg == nil || len(cfg.Sensitive.Dirs)+len(cfg.Sensitive.Files) == 0 {
		return nil
	}
	return &modelrouter.SensitiveRules{
		Dirs:  cfg.Sensitive.Dirs,
		Files: cfg.Sensitive.Files,
	}
}

// currentSensitiveRules 每次构建会话时重读 .teamix/config.yaml 的 sensitive 段
// （热加载：新增机密目录免重启，改配置下次新会话/切模型生效）。读失败回退启动快照。
func (ts *TeamixServer) currentSensitiveRules() *modelrouter.SensitiveRules {
	cfg, err := teamixconfig.Load(ts.workspaceRoot)
	if err != nil || cfg == nil {
		return ts.sensitiveRules
	}
	return sensitiveRulesFromCfg(cfg)
}

// QuotaTracker 三层配额计数（内存原子，P1）：个人日额 / 团队与全局月额。
// 只计数"出了网"的外部请求（OnDecision 里 Pool==external 时累加）。
// architect 角色豁免配额。P2 扩展：全局月额超限 = 预算降档（全降级内部 +
// 致命告警一次）。
type QuotaTracker struct {
	mu             sync.Mutex
	userDay        map[string]int // user → 当日出网次数
	userDayDate    string
	globalMonth    int
	globalMonthKey string
	perUserPerDay  int // <=0 不限
	globalPerMonth int // <=0 不限

	exceeded atomic.Bool // 预算（全局月额）超限标志，触发一次致命告警
}

func NewQuotaTracker(perUserPerDay, globalPerMonth int) *QuotaTracker {
	return &QuotaTracker{
		userDay:        make(map[string]int),
		perUserPerDay:  perUserPerDay,
		globalPerMonth: globalPerMonth,
	}
}

// dayKey/monthKey 基于日期（UTC 简单切日/切月）。
func dayKey() string   { return time.Now().Format("2006-01-02") }
func monthKey() string { return time.Now().Format("2006-01") }

// Allow 检查当前请求是否允许出网（个人日额 + 全局月额；architect 豁免）。
// 全局月额超限时置位 exceeded（预算降档：后续全降级内部 + 触发致命告警）。
func (q *QuotaTracker) Allow(user string, architect bool) bool {
	if q == nil {
		return true
	}
	if architect {
		return true
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.perUserPerDay > 0 {
		if q.userDayDate != dayKey() {
			q.userDay = make(map[string]int)
			q.userDayDate = dayKey()
		}
		if q.userDay[user] >= q.perUserPerDay {
			return false
		}
	}
	if q.globalPerMonth > 0 {
		if q.globalMonthKey != monthKey() {
			q.globalMonth = 0
			q.globalMonthKey = monthKey()
		}
		if q.globalMonth >= q.globalPerMonth {
			q.exceeded.Store(true)
			return false
		}
	}
	return true
}

// BudgetExceeded 报告预算（全局月额）是否已超限（供致命告警触发一次）。
func (q *QuotaTracker) BudgetExceeded() bool {
	if q == nil {
		return false
	}
	return q.exceeded.Load()
}

// Record 记录一次出网（配额消费）。
func (q *QuotaTracker) Record(user string) {
	if q == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.userDayDate != dayKey() {
		q.userDay = make(map[string]int)
		q.userDayDate = dayKey()
	}
	q.userDay[user]++
	if q.globalMonthKey != monthKey() {
		q.globalMonth = 0
		q.globalMonthKey = monthKey()
	}
	q.globalMonth++
}

// routerCfg 构造一次 boot.Build 的路由集成配置：内部池 + 敏感规则 + 审计 + 配额。
// 每次 Build 调用独立构造，使 Audit 回调能带上当前用户（per-user 日志）。
func (ts *TeamixServer) routerCfg(user string) *modelrouter.BootConfig {
	architect := ts.globalCfg != nil && ts.globalCfg.IsArchitect(user)
	// 权限细粒度：管理员可禁用指定用户的外部模型（users.yaml allow_external: false）
	userExternalAllowed := true
	if ts.globalCfg != nil {
		if u := ts.globalCfg.Users.FindUser(user); u != nil {
			userExternalAllowed = u.CanUseExternal()
		}
	}
	return &modelrouter.BootConfig{
		Internal:            ts.internalProvider,
		External:            ts.externalProvider,
		ExternalFromKeyPool: true,
		Sensitive:           ts.currentSensitiveRules(), // 热加载：每次 Build 重读机密清单
		Offline:             ts.offline.Load(),
		Audit: func(d modelrouter.Decision) {
			if ts.auditWriter != nil {
				ts.auditWriter.Write(auditRecord(user, d))
			}
			// 出网即消费配额
			if d.Pool == modelrouter.KindExternal && ts.quota != nil {
				ts.quota.Record(user)
			}
			// 预算降档（P2）：全局月额超限 → 致命告警一次（webhook + 审计）
			if ts.quota != nil && ts.quota.BudgetExceeded() && ts.budgetNotified.CompareAndSwap(false, true) {
				ts.notifyCritical("月度外部预算超限", "user="+user+"：所有外部请求已降级到内部 Qwen")
				if ts.auditWriter != nil {
					ts.auditWriter.Write(auditlog.Record{
						RequestID: "budget-exceeded",
						User:      user,
						Time:      time.Now(),
						Purpose:   "execute",
						Alerts:    []string{"[critical] budget_exceeded_global_monthly"},
					})
				}
			}
			// 泄露事故（信号 ②）：非 public 敏感级却出网 → 致命告警
			if d.Pool == modelrouter.KindExternal && d.Sensitivity != "" && d.Sensitivity != provider.SensitivityPublic {
				ts.notifyCritical("敏感内容出网（事故）", "request="+d.RequestID+" user="+user+" sensitivity="+string(d.Sensitivity))
			}
		},
		// 配额门禁 + 用户级禁外网：超限/被禁用 → 柔性降级内部池（用户无感，审计记 quota_exceeded）
		QuotaCheck: func() bool {
			if !userExternalAllowed {
				return false
			}
			return ts.quota == nil || ts.quota.Allow(user, architect)
		},
		// 闭环还原失败 → 致命告警 + 审计（假名化漏了/模型幻觉，泄露信号 ③）
		// 关联原请求 request_id（全链路 trace：可回溯到触发请求）
		OnRestoreFail: func(requestID, issue string) {
			ts.notifyCritical("闭环检测命中（疑似泄露）", "request="+requestID+" user="+user+" issue="+issue)
			if ts.auditWriter != nil {
				ts.auditWriter.Write(auditlog.Record{
					RequestID: requestID,
					User:      user,
					Time:      time.Now(),
					Purpose:   "execute",
					Alerts:    []string{"[critical] closed_loop_detected: " + issue},
				})
			}
		},
	}
}

// handleOfflineGet 返回仅本地模式状态（所有用户可读）。
func (ts *TeamixServer) handleOfflineGet(w http.ResponseWriter, _ *http.Request, _ *userSession) {
	writeJSON(w, map[string]any{"offline": ts.offline.Load()})
}

// handleOfflineSet 切换仅本地模式（仅 architect 可写）：手动"全走 Qwen"唯一入口。
// 切换后对下次 boot.Build 生效（新会话/切模型）；当前会话下次切模型时生效。
func (ts *TeamixServer) handleOfflineSet(w http.ResponseWriter, r *http.Request, u *userSession) {
	if !ts.isArchitect(u) {
		http.Error(w, "仅架构师可切换仅本地模式", http.StatusForbidden)
		return
	}
	var body struct {
		Offline bool `json:"offline"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad body: "+err.Error(), http.StatusBadRequest)
		return
	}
	ts.offline.Store(body.Offline)
	slog.Info("teamix: offline mode", "user", u.name, "offline", body.Offline)
	writeJSON(w, map[string]any{"offline": body.Offline})
}

// handleUserExternalToggle 切换用户的外部模型权限（仅 architect）：
// users.yaml 的 allow_external（false = 该用户全部请求走内部 Qwen）。
func (ts *TeamixServer) handleUserExternalToggle(w http.ResponseWriter, r *http.Request, u *userSession) {
	if !ts.isArchitect(u) {
		http.Error(w, "仅架构师可操作", http.StatusForbidden)
		return
	}
	var body struct {
		Name          string `json:"name"`
		AllowExternal bool   `json:"allow_external"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	uc := ts.globalCfg.Users
	target := uc.FindUser(body.Name)
	if target == nil {
		http.Error(w, "用户不存在", http.StatusNotFound)
		return
	}
	allow := body.AllowExternal
	target.AllowExternal = &allow
	if err := uc.SaveUsers(ts.workspaceRoot); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	slog.Info("teamix: user external permission", "by", u.name, "user", body.Name, "allow_external", allow)
	writeJSON(w, map[string]any{"ok": true, "name": body.Name, "allow_external": allow})
}

// notifyCritical 致命告警：slog.Error + 企微 webhook（best-effort，配置了才发）。
// 告警正文已脱敏（只含原因与用户，不含敏感数据——防"在报告路上泄露"）。
func (ts *TeamixServer) notifyCritical(title, body string) {
	slog.Error("teamix: "+title, "detail", body)
	if ts.alertWebhook == "" {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"msgtype": "text",
		"text":    map[string]string{"content": "【Teamix 安全告警】" + title + "\n" + body},
	})
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodPost, ts.alertWebhook, strings.NewReader(string(payload)))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		slog.Warn("teamix: alert webhook send failed", "err", err)
		return
	}
	_ = resp.Body.Close()
}

// auditRecord 把路由决策转成审计记录（操作流向：哪一步走哪个模型、是否出网）。
func auditRecord(user string, d modelrouter.Decision) auditlog.Record {
	rec := auditlog.Record{
		RequestID:   d.RequestID,
		User:        user,
		Time:        time.Now(),
		Purpose:     string(d.Purpose),
		Sensitivity: string(d.Sensitivity),
		Trace: []auditlog.Step{{
			Step:   "final_route",
			Model:  d.Model,
			Reason: d.Reason,
			Tokens: d.EstimatedTokens,
		}},
		Outbound: auditlog.Outbound{Sent: outboundFromDecision(d)},
	}
	// 泄露信号 ②：非 public 敏感级却出网 = 事故（本该拦截的内容出去了）→ 致命告警
	if rec.Outbound.Sent && d.Sensitivity != "" && d.Sensitivity != provider.SensitivityPublic {
		rec.Alerts = append(rec.Alerts, "[critical] sensitive_sent_external")
	}
	return rec
}

// outboundFromDecision 判断该决策是否把内容发到外部池。
func outboundFromDecision(d modelrouter.Decision) bool {
	return d.Pool == modelrouter.KindExternal
}
