package policy

import (
	"testing"

	"github.com/binoctal/cerberus/internal/types"
)

func TestDefaultActionPolicy_ProcessAllowed(t *testing.T) {
	p := NewDefaultActionPolicy(".")
	err := p.Validate(types.ProcessExecAction{Command: "go", Args: []string{"test", "./..."}})
	if err != nil {
		t.Fatalf("expected allowed, got: %v", err)
	}
}

func TestDefaultActionPolicy_ProcessDenied(t *testing.T) {
	p := NewDefaultActionPolicy(".")
	err := p.Validate(types.ProcessExecAction{Command: "rm", Args: []string{"-rf", "/"}})
	if err == nil {
		t.Fatal("expected denial for 'rm' command")
	}
}

func TestDefaultActionPolicy_ProcessShellMetachars(t *testing.T) {
	p := NewDefaultActionPolicy(".")
	err := p.Validate(types.ProcessExecAction{Command: "go", Args: []string{"; rm -rf /"}})
	if err == nil {
		t.Fatal("expected denial for shell metacharacters")
	}
}

func TestDefaultActionPolicy_FileWriteDeniedPath(t *testing.T) {
	p := NewDefaultActionPolicy(".")
	err := p.Validate(types.FileWriteAction{Path: "/etc/shadow", Content: "hack"})
	if err == nil {
		t.Fatal("expected denial for denied path")
	}
}

func TestDefaultActionPolicy_MCPAllowed(t *testing.T) {
	p := NewDefaultActionPolicy(".")
	err := p.Validate(types.MCPCallAction{Method: "tools/call", Server: "test"})
	if err != nil {
		t.Fatalf("expected allowed, got: %v", err)
	}
}

func TestDefaultActionPolicy_MCPDenied(t *testing.T) {
	p := NewDefaultActionPolicy(".")
	err := p.Validate(types.MCPCallAction{Method: "admin/delete", Server: "test"})
	if err == nil {
		t.Fatal("expected denial for unknown MCP method")
	}
}

func TestDefaultActionPolicy_HTTPAlwaysAllowed(t *testing.T) {
	p := NewDefaultActionPolicy(".")
	err := p.Validate(types.HTTPAction{Method: "DELETE", URL: "http://example.com/api/1"})
	if err != nil {
		t.Fatalf("HTTP actions should always pass policy, got: %v", err)
	}
}

func TestAnomalyDetector_SensitiveContent(t *testing.T) {
	d := NewDefaultAnomalyDetector()
	result := types.HTTPResult{OK: true, Body: `{"password": "secret123"}`}
	if !d.Check(result) {
		t.Error("expected anomaly for sensitive content")
	}
}

func TestAnomalyDetector_CleanContent(t *testing.T) {
	d := NewDefaultAnomalyDetector()
	result := types.HTTPResult{OK: true, Body: `{"status": "ok"}`}
	if d.Check(result) {
		t.Error("expected no anomaly for clean content")
	}
}
