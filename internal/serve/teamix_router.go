package serve

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	"reasonix/internal/auditlog"
	"reasonix/internal/modelrouter"
	"reasonix/internal/provider"
	"reasonix/internal/teamixconfig"
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

// routerCfg 构造一次 boot.Build 的路由集成配置：内部池 + 敏感规则 + 审计。
// 每次 Build 调用独立构造，使 Audit 回调能带上当前用户（per-user 日志）。
func (ts *TeamixServer) routerCfg(user string) *modelrouter.BootConfig {
	return &modelrouter.BootConfig{
		Internal:  ts.internalProvider,
		Sensitive: ts.sensitiveRules,
		Offline:   ts.offline.Load(),
		Audit: func(d modelrouter.Decision) {
			if ts.auditWriter != nil {
				ts.auditWriter.Write(auditRecord(user, d))
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
	// 泄露信号 ②：非 public 敏感级却出网 = 事故（本该拦截的内容出去了）
	if rec.Outbound.Sent && d.Sensitivity != "" && d.Sensitivity != provider.SensitivityPublic {
		rec.Alerts = append(rec.Alerts, "sensitive_sent_external")
	}
	return rec
}

// outboundFromDecision 判断该决策是否把内容发到外部池。
func outboundFromDecision(d modelrouter.Decision) bool {
	return d.Pool == modelrouter.KindExternal
}
