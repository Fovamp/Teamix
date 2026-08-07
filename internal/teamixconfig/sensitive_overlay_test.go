package teamixconfig

import (
	"os"
	"path/filepath"
	"testing"
)

// Save/Load 独立敏感清单应完整往返 dirs/files/tools（含内置工具敏感级声明）。
func TestSensitiveOverlayToolsRoundTrip(t *testing.T) {
	root := t.TempDir()
	sc := SensitiveConfig{
		Dirs:  []string{"tenders/", "secrets/"},
		Files: []string{".env", "*.pem"},
		Tools: map[string]string{
			"doc_kb_search": "internal",
			"web_fetch":     "redact",
		},
	}
	if err := SaveSensitiveOverlay(root, sc); err != nil {
		t.Fatalf("SaveSensitiveOverlay: %v", err)
	}
	got, err := LoadSensitiveOverlay(root)
	if err != nil {
		t.Fatalf("LoadSensitiveOverlay: %v", err)
	}
	if got == nil {
		t.Fatal("overlay = nil")
	}
	if len(got.Dirs) != 2 || got.Dirs[0] != "tenders/" {
		t.Errorf("dirs = %v", got.Dirs)
	}
	if len(got.Files) != 2 || got.Files[1] != "*.pem" {
		t.Errorf("files = %v", got.Files)
	}
	if got.Tools["doc_kb_search"] != "internal" || got.Tools["web_fetch"] != "redact" {
		t.Errorf("tools = %v", got.Tools)
	}
	// 文件确实存在且路径正确
	if _, err := os.Stat(SensitiveOverlayPath(root)); err != nil {
		t.Fatalf("overlay file missing: %v", err)
	}
}

// Tools 为空时 overlay 仍可正常保存/读取（兼容旧配置）。
func TestSensitiveOverlayNoTools(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sub")
	sc := SensitiveConfig{Dirs: []string{"data/"}}
	if err := SaveSensitiveOverlay(root, sc); err != nil {
		t.Fatalf("SaveSensitiveOverlay: %v", err)
	}
	got, err := LoadSensitiveOverlay(root)
	if err != nil || got == nil {
		t.Fatalf("LoadSensitiveOverlay: %v err=%v", got, err)
	}
	if got.Tools != nil {
		t.Errorf("tools should stay nil, got %v", got.Tools)
	}
}
