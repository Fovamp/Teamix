package auditlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWritePerUserFile(t *testing.T) {
	dir := t.TempDir()
	w := New(dir, 0)

	w.Write(Record{
		RequestID:   "r-1",
		User:        "zhangsan",
		Time:        time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC),
		Purpose:     "execute",
		Sensitivity: "public",
		Trace: []Step{{
			Step:   "final_route",
			Model:  "deepseek",
			Reason: "rule_execute_external",
		}},
		Outbound: Outbound{Sent: true},
	})
	w.Write(Record{
		RequestID:   "r-2",
		User:        "lisi",
		Time:        time.Date(2026, 8, 5, 12, 1, 0, 0, time.UTC),
		Purpose:     "execute",
		Sensitivity: "confidential",
		Outbound:    Outbound{Sent: false},
	})
	// Close flush 全部缓冲并关句柄后再读
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	zhang, err := os.ReadFile(filepath.Join(dir, "zhangsan", "2026-08-05.jsonl"))
	if err != nil {
		t.Fatalf("read zhangsan log: %v", err)
	}
	if !strings.Contains(string(zhang), `"request_id":"r-1"`) {
		t.Errorf("zhangsan log = %s", zhang)
	}
	var rec Record
	if err := json.Unmarshal(zhang, &rec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !rec.Outbound.Sent {
		t.Error("outbound.sent should be true for r-1")
	}
	if rec.Sensitivity != "public" || rec.Purpose != "execute" {
		t.Errorf("rec = %+v", rec)
	}
	if len(rec.Trace) != 1 || rec.Trace[0].Reason != "rule_execute_external" {
		t.Errorf("trace = %+v", rec.Trace)
	}

	// lisi 的用户文件独立
	if _, err := os.Stat(filepath.Join(dir, "lisi", "2026-08-05.jsonl")); err != nil {
		t.Errorf("lisi log missing: %v", err)
	}
}

func TestRecordHasLeakSignals(t *testing.T) {
	// 泄露三信号字段必须可序列化，供查询层标红
	incident := Record{
		RequestID:   "r-incident",
		Sensitivity: "internal", // 本该拦截
		Outbound:    Outbound{Sent: true},
		Alerts:      []string{"closed_loop_detected"},
	}
	b, err := json.Marshal(incident)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"sensitivity":"internal"`) || !strings.Contains(s, `"sent":true`) {
		t.Errorf("incident record missing signal: %s", s)
	}
}

func TestCleanupOldFiles(t *testing.T) {
	dir := t.TempDir()
	oldDir := filepath.Join(dir, "zhangsan")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldFile := filepath.Join(oldDir, "2026-01-01.jsonl")
	if err := os.WriteFile(oldFile, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 把 mtime 改成 40 天前
	old := time.Now().AddDate(0, 0, -40)
	os.Chtimes(oldFile, old, old)

	w := New(dir, 30) // 30 天保留期，启动清理
	defer w.Close()

	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Errorf("old file should be cleaned, err=%v", err)
	}
}
