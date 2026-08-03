package teamixconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureGlobalWorkspace(t *testing.T) {
	root := t.TempDir()

	// 首次初始化：创建目录 + 三个模板文件
	if err := EnsureGlobalWorkspace(root); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	for _, rel := range []string{".teamix/config.yaml", ".teamix/users.yaml", ".teamix/projects.yaml", ".teamix/notifications", ".teamix/workflows", ".reasonix"} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("missing after init: %s (%v)", rel, err)
		}
	}

	// 幂等：再跑一次不报错、不覆盖已有内容
	custom := "# 架构师自定义\nusers:\n  - name: boss\n    role: architect\n"
	if err := os.WriteFile(filepath.Join(root, ".teamix", "users.yaml"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureGlobalWorkspace(root); err != nil {
		t.Fatalf("re-ensure: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".teamix", "users.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != custom {
		t.Errorf("existing file was overwritten: %q", data)
	}

	// 空 root：no-op
	if err := EnsureGlobalWorkspace(""); err != nil {
		t.Errorf("empty root should be no-op, got %v", err)
	}
}
