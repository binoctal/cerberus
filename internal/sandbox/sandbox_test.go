package sandbox

import (
	"context"
	"testing"
)

func TestNoOpSandbox_Apply(t *testing.T) {
	sb := NoOpSandbox{}
	ctx := context.Background()
	policy := DefaultHTTPPolicy()

	newCtx, cleanup, err := sb.Apply(ctx, policy)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if newCtx == nil {
		t.Fatal("Apply returned nil context")
	}
	cleanup() // should not panic
}

func TestDefaultProcessPolicy(t *testing.T) {
	p := DefaultProcessPolicy("/tmp/work")
	if len(p.FS.Denied) == 0 {
		t.Error("expected denied paths")
	}
	if p.Network.AllowOutbound {
		t.Error("process policy should not allow outbound")
	}
	if p.Resources.MaxMemoryMB != 512 {
		t.Errorf("expected 512 MB, got %d", p.Resources.MaxMemoryMB)
	}
}

func TestDefaultFilePolicy(t *testing.T) {
	p := DefaultFilePolicy(".")
	if len(p.FS.ReadWrite) == 0 {
		t.Error("expected read-write paths")
	}
}

func TestDefaultHTTPPolicy(t *testing.T) {
	p := DefaultHTTPPolicy()
	if !p.Network.AllowOutbound {
		t.Error("HTTP policy should allow outbound")
	}
}

func TestDefaultMCPPolicy(t *testing.T) {
	p := DefaultMCPPolicy()
	if !p.Network.AllowOutbound {
		t.Error("MCP policy should allow outbound")
	}
}

func TestDefaultCodePolicy(t *testing.T) {
	p := DefaultCodePolicy(".")
	if len(p.FS.ReadOnly) == 0 {
		t.Error("expected read-only paths")
	}
}
