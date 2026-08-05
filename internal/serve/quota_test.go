package serve

import (
	"testing"
)

func TestQuotaTrackerPerUserDay(t *testing.T) {
	q := NewQuotaTracker(2, 0) // 每人每日 2 次出网
	if !q.Allow("zhangsan", false) {
		t.Fatal("first should be allowed")
	}
	q.Record("zhangsan")
	if !q.Allow("zhangsan", false) {
		t.Fatal("second should be allowed")
	}
	q.Record("zhangsan")
	if q.Allow("zhangsan", false) {
		t.Fatal("third should be blocked (per-user day quota)")
	}
	// 其他用户不受影响
	if !q.Allow("lisi", false) {
		t.Fatal("lisi should be allowed")
	}
}

func TestQuotaTrackerGlobalMonth(t *testing.T) {
	q := NewQuotaTracker(0, 3) // 全局每月 3 次
	for i := 0; i < 3; i++ {
		if !q.Allow("u", false) {
			t.Fatalf("call %d should be allowed", i+1)
		}
		q.Record("u")
	}
	if q.Allow("u", false) {
		t.Fatal("global month quota exceeded should block")
	}
}

func TestQuotaTrackerArchitectExempt(t *testing.T) {
	q := NewQuotaTracker(1, 0)
	q.Record("arch")
	if !q.Allow("arch", true) {
		t.Fatal("architect should be exempt from quota")
	}
}

func TestQuotaTrackerNilAllowed(t *testing.T) {
	var q *QuotaTracker
	if !q.Allow("u", false) {
		t.Fatal("nil tracker = unlimited")
	}
}
