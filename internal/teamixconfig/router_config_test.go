package teamixconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadModelsSensitiveAudit(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".teamix")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	yml := `teamix:
  name: demo
models:
  internal:
    - ref: "qwen/qwen3.6-35b-a3b"
      roles: [subagent, guard, compress, classify, cheap, plan]
      max_input_tokens: 25000
      max_parallel: 2
  external:
    - ref: "deepseek/deepseek-v4-pro"
      roles: [execute, plan]
      max_input_tokens: 1000000
sensitive:
  dirs: ["tenders/", "data/", "secrets/"]
  files: [".env", "*.pem"]
audit:
  dir: ".teamix/logs/ai-audit"
  retention_days: 30
  roles_visible: [architect]
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(cfg.Models.Internal) != 1 {
		t.Fatalf("internal pools = %d, want 1", len(cfg.Models.Internal))
	}
	internal := cfg.Models.Internal[0]
	if internal.Ref != "qwen/qwen3.6-35b-a3b" {
		t.Errorf("internal ref = %q", internal.Ref)
	}
	if internal.MaxInputTokens != 25000 {
		t.Errorf("internal max_input_tokens = %d, want 25000", internal.MaxInputTokens)
	}
	if internal.MaxParallel != 2 {
		t.Errorf("internal max_parallel = %d, want 2", internal.MaxParallel)
	}
	if len(internal.Roles) != 6 {
		t.Errorf("internal roles = %v", internal.Roles)
	}

	if len(cfg.Models.External) != 1 || cfg.Models.External[0].Ref != "deepseek/deepseek-v4-pro" {
		t.Fatalf("external pools = %+v", cfg.Models.External)
	}

	if len(cfg.Sensitive.Dirs) != 3 || cfg.Sensitive.Dirs[0] != "tenders/" {
		t.Errorf("sensitive dirs = %v", cfg.Sensitive.Dirs)
	}
	if len(cfg.Sensitive.Files) != 2 || cfg.Sensitive.Files[1] != "*.pem" {
		t.Errorf("sensitive files = %v", cfg.Sensitive.Files)
	}

	if cfg.Audit.Dir != ".teamix/logs/ai-audit" {
		t.Errorf("audit dir = %q", cfg.Audit.Dir)
	}
	if cfg.Audit.RetentionDays != 30 {
		t.Errorf("audit retention = %d", cfg.Audit.RetentionDays)
	}
	if len(cfg.Audit.RolesVisible) != 1 || cfg.Audit.RolesVisible[0] != "architect" {
		t.Errorf("audit roles_visible = %v", cfg.Audit.RolesVisible)
	}
}

func TestDefaultAuditConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Audit.Dir != ".teamix/logs/ai-audit" || cfg.Audit.RetentionDays != 30 {
		t.Fatalf("default audit = %+v", cfg.Audit)
	}
	if len(cfg.Audit.RolesVisible) != 1 || cfg.Audit.RolesVisible[0] != "architect" {
		t.Fatalf("default roles_visible = %v", cfg.Audit.RolesVisible)
	}
}

func TestUserCanUseExternal(t *testing.T) {
	if (UserEntry{Name: "a"}).CanUseExternal() != true {
		t.Error("unset allow_external should default to allowed")
	}
	f := false
	if (UserEntry{Name: "a", AllowExternal: &f}).CanUseExternal() != false {
		t.Error("allow_external=false should block external")
	}
	t2 := true
	if (UserEntry{Name: "a", AllowExternal: &t2}).CanUseExternal() != true {
		t.Error("allow_external=true should allow")
	}
}
