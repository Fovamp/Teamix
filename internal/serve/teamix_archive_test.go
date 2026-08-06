package serve

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMoveSessionGroup 验证归档整组移动：jsonl + sidecar + ckpt 目录全部移走，源目录无残留。
func TestMoveSessionGroup(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "archive", "sessions", "proj")
	// 构造一个会话的文件组
	stem := "0812-140000-deepseek"
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(src, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(stem+".jsonl", "{\"role\":\"user\",\"content\":\"hi\"}\n")
	write(stem+".events.jsonl", "evt\n")
	write(stem+".event-index.json", "idx\n")
	write(stem+".jsonl.meta", "meta\n")
	write("other-session.jsonl", "{\"role\":\"user\",\"content\":\"other\"}\n") // 无关会话
	if err := os.MkdirAll(filepath.Join(src, stem+".ckpt"), 0o755); err != nil {
		t.Fatal(err)
	}

	ts := &TeamixServer{}
	if err := ts.moveSessionGroup(src, dst, stem); err != nil {
		t.Fatalf("moveSessionGroup: %v", err)
	}
	// 目标目录应包含整组
	for _, want := range []string{
		stem + ".jsonl", stem + ".events.jsonl", stem + ".event-index.json", stem + ".jsonl.meta", stem + ".ckpt",
	} {
		if _, err := os.Stat(filepath.Join(dst, want)); err != nil {
			t.Errorf("archived missing %s: %v", want, err)
		}
	}
	// 源目录应无残留（本会话的文件组）
	for _, gone := range []string{
		stem + ".jsonl", stem + ".events.jsonl", stem + ".event-index.json", stem + ".jsonl.meta", stem + ".ckpt",
	} {
		if _, err := os.Stat(filepath.Join(src, gone)); !os.IsNotExist(err) {
			t.Errorf("source still has %s", gone)
		}
	}
	// 无关会话不受影响
	if _, err := os.Stat(filepath.Join(src, "other-session.jsonl")); err != nil {
		t.Errorf("unrelated session disturbed: %v", err)
	}
}

// TestArchiveRoundTrip 归档后再恢复：文件组原样回到活跃目录。
func TestArchiveRoundTrip(t *testing.T) {
	active := t.TempDir()
	arch := filepath.Join(t.TempDir(), "archive", "sessions", "proj")
	stem := "0812-140000-deepseek"
	content := "{\"role\":\"user\",\"content\":\"你好\"}\n"
	if err := os.WriteFile(filepath.Join(active, stem+".jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := &TeamixServer{}
	if err := ts.moveSessionGroup(active, arch, stem); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if err := ts.moveSessionGroup(arch, active, stem); err != nil {
		t.Fatalf("restore: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(active, stem+".jsonl"))
	if err != nil {
		t.Fatalf("restored jsonl missing: %v", err)
	}
	if string(b) != content {
		t.Errorf("restored content = %q, want %q", b, content)
	}
}
