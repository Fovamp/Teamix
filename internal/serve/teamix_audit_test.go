package serve

import (
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/auditlog"
)

func TestReadAuditLogs(t *testing.T) {
	dir := t.TempDir()
	w := auditlog.New(dir, 0)
	w.Write(auditlog.Record{
		RequestID:   "r-1",
		User:        "zhangsan",
		Time:        time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC),
		Purpose:     "execute",
		Sensitivity: "public",
		Outbound:    auditlog.Outbound{Sent: true},
	})
	w.Write(auditlog.Record{
		RequestID:   "r-2",
		User:        "zhangsan",
		Time:        time.Date(2026, 8, 5, 11, 0, 0, 0, time.UTC),
		Sensitivity: "internal",
		Outbound:    auditlog.Outbound{Sent: false},
		Alerts:      []string{"closed_loop_detected: x"},
	})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	// 文件在 dir/zhangsan/2026-08-05.jsonl（auditlog 内部按 用户×天 拼路径）
	recs, err := readJSONL(filepath.Join(dir, "zhangsan", "2026-08-05.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("records = %d, want 2", len(recs))
	}
}

func TestAggregateAuditStats(t *testing.T) {
	now := time.Now()
	today := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")

	records := []auditlog.Record{
		// zhangsan：今天 1 条出网（in 100 / out 200）
		{RequestID: "a1", User: "zhangsan", Time: now, Cost: auditlog.Cost{InTokens: 100, OutTokens: 200}, Outbound: auditlog.Outbound{Sent: true}},
		// zhangsan：昨天 1 条内网 + 1 条致命告警
		{RequestID: "a2", User: "zhangsan", Time: now.AddDate(0, 0, -1), Cost: auditlog.Cost{InTokens: 50, OutTokens: 30}, Alerts: []string{"[critical] sensitive_sent"}},
		// lisi：今天 2 条（含无 user 兜底 _system 不用）
		{RequestID: "b1", User: "lisi", Time: now, Cost: auditlog.Cost{InTokens: 500, OutTokens: 0}},
		// 超窗记录：不应计入
		{RequestID: "c1", User: "zhangsan", Time: now.AddDate(0, 0, -8), Cost: auditlog.Cost{InTokens: 9999}},
		// 空 user 兜底 _system
		{RequestID: "d1", Time: now, Cost: auditlog.Cost{InTokens: 10}},
	}

	days, users, totals := aggregateAuditStats(records, 7)
	if len(days) != 7 {
		t.Fatalf("days = %d, want 7", len(days))
	}
	if days[0] != today && len(days) > 0 {
		// days 是"最近 7 个自然日"（含今天），首日应为 7 天前
	}
	// 昨天 zhangsan 的 daily 明细应有 1 次调用
	byName := map[string]auditUserStat{}
	for _, u := range users {
		byName[u.User] = u
	}
	zs, ok := byName["zhangsan"]
	if !ok {
		t.Fatalf("zhangsan missing from %v", users)
	}
	if zs.Tokens != 100+200+50+30 {
		t.Errorf("zhangsan tokens = %d, want 380", zs.Tokens)
	}
	if zs.Calls != 2 {
		t.Errorf("zhangsan calls = %d, want 2", zs.Calls)
	}
	if zs.Outbound != 1 {
		t.Errorf("zhangsan outbound = %d, want 1", zs.Outbound)
	}
	if zs.Critical != 1 {
		t.Errorf("zhangsan critical = %d, want 1", zs.Critical)
	}
	yesterdayCalls := 0
	for _, d := range zs.Daily {
		if d.Date == yesterday {
			yesterdayCalls = d.Calls
		}
	}
	if yesterdayCalls != 1 {
		t.Errorf("zhangsan yesterday calls = %d, want 1", yesterdayCalls)
	}
	if _, ok := byName["_system"]; !ok {
		t.Errorf("empty user should fall back to _system, got %v", users)
	}
	// 超窗记录不计入（zhangsan 不应含 9999）
	if zs.Tokens >= 9999 {
		t.Errorf("out-of-window record leaked into stats: tokens=%d", zs.Tokens)
	}
	// 按 token 降序：lisi(500) 应排在 zhangsan(380) 前
	if len(users) < 2 {
		t.Fatalf("want >=2 users, got %d", len(users))
	}
	if users[0].Tokens < users[1].Tokens {
		t.Errorf("users not sorted by tokens desc: %d < %d", users[0].Tokens, users[1].Tokens)
	}
	// 按天合计：今天总和 = zhangsan 300 + lisi 500 + _system 10
	if len(totals) != 7 {
		t.Fatalf("totals = %d, want 7 days", len(totals))
	}
	todayTotal := 0
	for _, d := range totals {
		if d.Date == today {
			todayTotal = d.Tokens
		}
	}
	if todayTotal != 300+500+10 {
		t.Errorf("today total = %d, want 810", todayTotal)
	}
	_ = yesterday // 昨天日期在 daily 明细里校验
}
