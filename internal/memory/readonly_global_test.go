package memory

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReadOnlyGlobalForcesPrivateWrite ensures a read-only GlobalDir (Teamix
// team-global memory) redirects writes to Dir while reads still merge.
func TestReadOnlyGlobalForcesPrivateWrite(t *testing.T) {
	dir := t.TempDir()
	s := Store{
		Dir:            filepath.Join(dir, "private"),
		GlobalDir:      filepath.Join(dir, "global"),
		ReadOnlyGlobal: true,
	}
	// 写全局类型（TypeUser）→ 应落到 Dir（私有），不写 GlobalDir
	p, err := s.Save(Memory{Name: "pref", Type: TypeUser, Description: "d", Body: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(p) {
		t.Fatalf("path = %q", p)
	}
	if _, err := os.Stat(filepath.Join(s.GlobalDir, "pref.md")); !os.IsNotExist(err) {
		t.Fatalf("read-only global should not receive writes, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "pref.md")); err != nil {
		t.Fatalf("write should land in private Dir: %v", err)
	}
}

// TestReadOnlyGlobalStillReadsForIndex ensures Index() merges global even when read-only.
func TestReadOnlyGlobalStillReadsForIndex(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "global")
	if err := os.MkdirAll(global, 0o755); err != nil {
		t.Fatal(err)
	}
	// Index() 读 MEMORY.md 索引文件（合并全局+私有），行格式：- [name](file.md) — desc
	if err := os.WriteFile(filepath.Join(global, "MEMORY.md"), []byte("- [team-fact](team-fact.md) — 团队共享事实\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := Store{Dir: filepath.Join(dir, "private"), GlobalDir: global, ReadOnlyGlobal: true}
	idx := s.Index()
	if !contains(idx, "team-fact") {
		t.Fatalf("read-only global should still appear in Index: %q", idx)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
