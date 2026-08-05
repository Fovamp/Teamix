package agent

import (
	"encoding/json"
	"testing"

	"reasonix/internal/modelrouter"
	"reasonix/internal/provider"
)

func TestMarkToolSensitivityConfidential(t *testing.T) {
	a := &Agent{
		sensitiveRules: &modelrouter.SensitiveRules{
			Dirs:  []string{"tenders/", "data/"},
			Files: []string{"*.pem"},
		},
	}
	a.markToolSensitivity(nil, json.RawMessage(`{"path": "tenders/项目A.docx"}`))
	if a.sensitive != provider.SensitivityConfidential {
		t.Fatalf("sensitive = %q, want confidential (data residency pinning)", a.sensitive)
	}
}

func TestMarkToolSensitivityPublicPath(t *testing.T) {
	a := &Agent{
		sensitiveRules: &modelrouter.SensitiveRules{
			Dirs: []string{"tenders/"},
		},
	}
	a.markToolSensitivity(nil, json.RawMessage(`{"path": "internal/risk/analyzer.go"}`))
	if a.sensitive != "" {
		t.Fatalf("sensitive = %q, want empty (code is not confidential)", a.sensitive)
	}
}

func TestMarkToolSensitivityGlobFile(t *testing.T) {
	a := &Agent{
		sensitiveRules: &modelrouter.SensitiveRules{
			Files: []string{"*.pem"},
		},
	}
	a.markToolSensitivity(nil, json.RawMessage(`{"file": "deploy/keys.pem"}`))
	if a.sensitive != provider.SensitivityConfidential {
		t.Fatalf("sensitive = %q, want confidential for *.pem", a.sensitive)
	}
}

func TestMarkToolSensitivityBadArgs(t *testing.T) {
	a := &Agent{
		sensitiveRules: &modelrouter.SensitiveRules{Dirs: []string{"tenders/"}},
	}
	a.markToolSensitivity(nil, json.RawMessage(`not-json`))
	if a.sensitive != "" {
		t.Fatalf("bad args should not mark sensitive: %q", a.sensitive)
	}
}
