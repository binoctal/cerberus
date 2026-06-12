package sandbox

import (
	"context"
	"testing"

	"go.uber.org/zap"
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
	if p.Network.AllowOutbound {
		t.Error("code policy should not allow outbound")
	}
}

func TestDefaultBrowserPolicy(t *testing.T) {
	p := DefaultBrowserPolicy()
	if !p.Network.AllowOutbound {
		t.Error("browser policy should allow outbound")
	}
	if p.Resources.Timeout != 60 {
		t.Errorf("expected timeout 60, got %d", p.Resources.Timeout)
	}
}

func TestDefaultDBPolicy(t *testing.T) {
	p := DefaultDBPolicy()
	if !p.Network.AllowOutbound {
		t.Error("DB policy should allow outbound")
	}
	if p.Resources.Timeout != 30 {
		t.Errorf("expected timeout 30, got %d", p.Resources.Timeout)
	}
}

func TestDefaultGraphQLPolicy(t *testing.T) {
	p := DefaultGraphQLPolicy()
	if !p.Network.AllowOutbound {
		t.Error("GraphQL policy should allow outbound")
	}
	if p.Resources.Timeout != 30 {
		t.Errorf("expected timeout 30, got %d", p.Resources.Timeout)
	}
}

func TestDefaultWSPolicy(t *testing.T) {
	p := DefaultWSPolicy()
	if !p.Network.AllowOutbound {
		t.Error("WS policy should allow outbound")
	}
	if p.Resources.Timeout != 60 {
		t.Errorf("expected timeout 60, got %d", p.Resources.Timeout)
	}
}

func TestDefaultProcessPolicy_DeniedPaths(t *testing.T) {
	p := DefaultProcessPolicy("/tmp/work")
	denied := []string{"/etc/shadow", "/root/.ssh", "/.env"}
	for _, d := range denied {
		found := false
		for _, dp := range p.FS.Denied {
			if dp == d {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %s in denied paths", d)
		}
	}
	if p.Resources.Timeout != 60 {
		t.Errorf("expected timeout 60, got %d", p.Resources.Timeout)
	}
}

func TestDefaultHTTPPolicy_Timeout(t *testing.T) {
	p := DefaultHTTPPolicy()
	if p.Resources.Timeout != 30 {
		t.Errorf("expected timeout 30, got %d", p.Resources.Timeout)
	}
}

func TestDefaultMCPPolicy_Timeout(t *testing.T) {
	p := DefaultMCPPolicy()
	if p.Resources.Timeout != 10 {
		t.Errorf("expected timeout 10, got %d", p.Resources.Timeout)
	}
}

func TestLinuxSandbox_Fallback(t *testing.T) {
	sb := NewLinuxSandbox(zap.NewNop())
	// On most dev machines, LinuxSandbox is unavailable (no root).
	// Verify the fallback paths work correctly.
	if !sb.IsAvailable() {
		// Fallback: Apply should be no-op
		ctx, cleanup, err := sb.Apply(context.Background(), DefaultHTTPPolicy())
		if err != nil {
			t.Fatalf("Apply returned error: %v", err)
		}
		if ctx == nil {
			t.Fatal("Apply returned nil context")
		}
		cleanup()

		// Fallback: ExecCommand should work via NoOp
		stdout, _, exitCode, err := sb.ExecCommand(
			context.Background(), "echo", []string{"fallback"}, nil, ".", Policy{},
		)
		if err != nil {
			t.Fatalf("ExecCommand returned error: %v", err)
		}
		if exitCode != 0 {
			t.Errorf("expected exit code 0, got %d", exitCode)
		}
		if stdout == "" {
			t.Error("expected stdout output")
		}
	}
}

func TestPolicy_Fields(t *testing.T) {
	p := Policy{
		FS: FSPolicy{
			ReadOnly:  []string{"/usr"},
			ReadWrite: []string{"/tmp"},
			Denied:    []string{"/etc/shadow"},
		},
		Network: NetPolicy{
			AllowOutbound: true,
			AllowHosts:    []string{"example.com"},
		},
		Resources: ResPolicy{
			MaxMemoryMB:   256,
			MaxCPUPercent: 50,
			Timeout:       30,
		},
	}
	if len(p.FS.ReadOnly) != 1 || p.FS.ReadOnly[0] != "/usr" {
		t.Error("FS.ReadOnly mismatch")
	}
	if len(p.Network.AllowHosts) != 1 || p.Network.AllowHosts[0] != "example.com" {
		t.Error("Network.AllowHosts mismatch")
	}
	if p.Resources.MaxMemoryMB != 256 {
		t.Errorf("expected MaxMemoryMB 256, got %d", p.Resources.MaxMemoryMB)
	}
}

func TestNoOpSandbox_ApplyWithNilPolicy(t *testing.T) {
	sb := NoOpSandbox{}
	ctx, cleanup, err := sb.Apply(context.Background(), Policy{})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if ctx == nil {
		t.Fatal("Apply returned nil context")
	}
	cleanup()
}

func TestNoOpSandbox_ExecCommand_WithDir(t *testing.T) {
	sb := NoOpSandbox{}
	stdout, _, exitCode, err := sb.ExecCommand(
		context.Background(), "pwd", nil, nil, "/tmp", Policy{},
	)
	if err != nil {
		t.Fatalf("ExecCommand returned error: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	// The working directory should be /tmp
	if stdout != "/tmp\n" {
		t.Errorf("expected /tmp, got %q", stdout)
	}
}
