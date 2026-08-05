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
