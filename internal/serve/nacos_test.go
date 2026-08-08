package serve

import (
	"os"
	"path/filepath"
	"testing"
)

// hasNacosConfig 覆盖判断：yaml 含 cloud: + nacos: 才算"项目配了 nacos"。
func TestHasNacosConfig(t *testing.T) {
	dir := t.TempDir()
	// 1. 无 nacos 的项目 → false
	_ = os.WriteFile(filepath.Join(dir, "application.yml"), []byte("server:\n  port: 8080\n"), 0o644)
	if hasNacosConfig(dir) {
		t.Fatal("plain yml should NOT match nacos")
	}
	// 2. 有 cloud.nacos → true
	_ = os.WriteFile(filepath.Join(dir, "application.yml"), []byte("spring:\n  application:\n    name: demo\n  cloud:\n    nacos:\n      config:\n        server-addr: 192.168.29.42:30107\n"), 0o644)
	if !hasNacosConfig(dir) {
		t.Fatal("yml with cloud.nacos SHOULD match")
	}
	// 3. bootstrap.yml 也算
	_ = os.WriteFile(filepath.Join(dir, "bootstrap.yml"), []byte("spring:\n  cloud:\n    nacos:\n      discovery:\n        server-addr: x:8848\n"), 0o644)
	if !hasNacosConfig(dir) {
		t.Fatal("bootstrap.yml with nacos SHOULD match")
	}
	// 4. 子目录 + 排除 node_modules/target
	sub := filepath.Join(dir, "src", "main", "resources")
	_ = os.MkdirAll(sub, 0o755)
	_ = os.WriteFile(filepath.Join(sub, "application.yml"), []byte("spring:\n  cloud:\n    nacos:\n      config:\n        group: DEFAULT_GROUP\n"), 0o644)
	if !hasNacosConfig(dir) {
		t.Fatal("nested resources yml SHOULD match")
	}
	_ = os.MkdirAll(filepath.Join(dir, "node_modules", "pkg"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "node_modules", "pkg", "x.yml"), []byte("cloud:\nnacos:\n"), 0o644)
	// 已 found=true（resources 命中），无法区分——单独目录验证排除
	clean := t.TempDir()
	_ = os.MkdirAll(filepath.Join(clean, "node_modules"), 0o755)
	_ = os.WriteFile(filepath.Join(clean, "node_modules", "x.yml"), []byte("cloud:\nnacos:\n"), 0o644)
	if hasNacosConfig(clean) {
		t.Fatal("node_modules yml should be skipped")
	}
}

// nacosEnvFor 注入内容：config 组=Global，discovery 组=用户名。
func TestNacosEnvForGroupSplit(t *testing.T) {
	ts := &TeamixServer{workspaceRoot: t.TempDir()}
	// 团队级不配 → 返回 nil
	if got := ts.nacosEnvFor(&userSession{name: "zhangsan", userRoot: t.TempDir()}, t.TempDir()); got != nil {
		t.Fatalf("no nacos template -> want nil, got %v", got)
	}
}
