package serve

import (
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"reasonix/internal/modelrouter"
	"reasonix/internal/teamixconfig"
)

// 配置并发访问收口：热加载 goroutine 会替换 globalCfg / sensitiveRules /
// alertWebhook，而每个请求都在读它们。所有读写走 cfgMu，避免数据竞争。
// 读方请用 GlobalCfg()/SensitiveRules()/AlertWebhook()，写方用 setGlobalCfg()。

// GlobalCfg 返回当前团队配置（cfgMu 读锁）。
func (ts *TeamixServer) GlobalCfg() *teamixconfig.GlobalConfig {
	ts.cfgMu.RLock()
	defer ts.cfgMu.RUnlock()
	return ts.globalCfg
}

// SensitiveRules 返回当前机密清单快照（cfgMu 读锁）。
func (ts *TeamixServer) SensitiveRules() *modelrouter.SensitiveRules {
	ts.cfgMu.RLock()
	defer ts.cfgMu.RUnlock()
	return ts.sensitiveRules
}

// AlertWebhook 返回当前致命告警 webhook（cfgMu 读锁）。
func (ts *TeamixServer) AlertWebhook() string {
	ts.cfgMu.RLock()
	defer ts.cfgMu.RUnlock()
	return ts.alertWebhook
}

// Nacos 返回 nacos 配置中心连接模板（cfgMu 读锁；未配置返回零值）。
func (ts *TeamixServer) Nacos() teamixconfig.NacosConfig {
	ts.cfgMu.RLock()
	defer ts.cfgMu.RUnlock()
	if ts.globalCfg != nil && ts.globalCfg.Config != nil {
		return ts.globalCfg.Config.Nacos
	}
	return teamixconfig.NacosConfig{}
}

// setGlobalCfg 原子替换配置快照并同步派生字段（cfgMu 写锁）。
func (ts *TeamixServer) setGlobalCfg(cfg *teamixconfig.GlobalConfig) {
	ts.cfgMu.Lock()
	defer ts.cfgMu.Unlock()
	ts.globalCfg = cfg
	if cfg != nil && cfg.Config != nil {
		ts.sensitiveRules = sensitiveRulesFromCfg(cfg.Config)
		ts.alertWebhook = cfg.Config.Alert.WebhookURL
	}
}

// watchConfigHotReload 轮询监听 .teamix/ 团队配置文件（sensitive.yaml /
// users.yaml / config.yaml），mtime 变化即重载 globalCfg —— 运行中的服务
// 配置即时生效，无需重启（此前机密清单只在构建新会话时重读、users/quota/
// alert 等要到重启才生效）。
//
// 用 5s 轻量轮询而非 fsnotify：零依赖、跨平台；配置文件极小，stat 开销可忽略。
func (ts *TeamixServer) watchConfigHotReload() {
	dir := filepath.Join(ts.workspaceRoot, ".teamix")
	files := []string{"sensitive.yaml", "users.yaml", "config.yaml"}
	mtimes := map[string]time.Time{}

	reload := func() {
		changed := false
		for _, f := range files {
			fi, err := os.Stat(filepath.Join(dir, f))
			if err != nil {
				continue
			}
			m := fi.ModTime()
			if prev, ok := mtimes[f]; ok && !m.Equal(prev) {
				changed = true
			}
			mtimes[f] = m
		}
		if !changed {
			return
		}
		cfg, err := teamixconfig.LoadAll(ts.workspaceRoot)
		if err != nil {
			slog.Warn("teamix: config hot reload failed", "err", err)
			return
		}
		ts.setGlobalCfg(cfg)
		slog.Info("teamix: config hot-reloaded", "files", files)
	}

	reload() // 初始基线
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		reload()
	}
}
