//go:build linux

package sandbox

import (
	"context"
	"testing"

	"github.com/criyle/go-sandbox/runner"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestTryNewLinuxSandbox_GracefulDegradation(t *testing.T) {
	// In an unprivileged test environment, TryNewLinuxSandbox may return nil.
	// This test verifies it doesn't panic and returns a usable result.
	sb := TryNewLinuxSandbox(zap.NewNop())
	if sb == nil {
		t.Log("Linux sandbox unavailable (expected in unprivileged CI), skipping")
		return
	}
	assert.True(t, sb.(*LinuxSandbox).IsAvailable())
}

func TestLinuxSandbox_ApplyNoPanic(t *testing.T) {
	sb := &LinuxSandbox{logger: zap.NewNop(), available: false}
	ctx := context.Background()
	policy := DefaultProcessPolicy(".")

	newCtx, cleanup, err := sb.Apply(ctx, policy)
	assert.NoError(t, err)
	assert.Equal(t, ctx, newCtx)
	cleanup()
}

func TestLinuxSandbox_ApplyWithPolicy(t *testing.T) {
	sb := &LinuxSandbox{logger: zap.NewNop(), available: true}
	ctx := context.Background()
	policy := DefaultProcessPolicy(".")

	newCtx, cleanup, err := sb.Apply(ctx, policy)
	assert.NoError(t, err)
	assert.NotNil(t, newCtx)
	cleanup()
}

func TestLinuxSandbox_RunNotAvailable(t *testing.T) {
	sb := &LinuxSandbox{logger: zap.NewNop(), available: false}
	result := sb.RunInSandbox(context.Background(), []string{"echo", "hi"}, nil, DefaultHTTPPolicy())
	assert.Equal(t, runner.StatusRunnerError, result.Status)
	assert.Contains(t, result.Error, "sandbox not available")
}

func TestLinuxSandbox_IsAvailable(t *testing.T) {
	sb := NewLinuxSandbox(zap.NewNop())
	// Just verify it doesn't panic; actual availability depends on privileges.
	_ = sb.IsAvailable()
}

func TestLinuxSandbox_BuildRLimits(t *testing.T) {
	sb := &LinuxSandbox{logger: zap.NewNop()}
	policy := Policy{
		Resources: ResPolicy{Timeout: 5, MaxMemoryMB: 100},
	}
	limits := sb.buildRLimits(policy)
	assert.NotEmpty(t, limits, "rlimits should be produced for nonzero resource policy")
}

func TestLinuxSandbox_BuildSeccompFilter_AllowOutbound(t *testing.T) {
	sb := &LinuxSandbox{logger: zap.NewNop()}
	// Outbound allowed → no syscall filtering needed.
	filter := sb.buildSeccompFilter(Policy{Network: NetPolicy{AllowOutbound: true}})
	assert.Nil(t, filter)
}

func TestLinuxSandbox_BuildSeccompFilter_DenyOutbound(t *testing.T) {
	sb := &LinuxSandbox{logger: zap.NewNop()}
	// Outbound denied → builder runs; result depends on libseccomp availability.
	// We only assert it does not panic.
	_ = sb.buildSeccompFilter(Policy{Network: NetPolicy{AllowOutbound: false}})
}

func TestLinuxSandbox_BuildMounts_Default(t *testing.T) {
	sb := &LinuxSandbox{logger: zap.NewNop()}
	// Empty FS policy → only default mounts are produced.
	mounts := sb.buildMounts(Policy{})
	assert.NotNil(t, mounts)
}

func TestLinuxSandbox_CgroupSync_NoLimits(t *testing.T) {
	sb := &LinuxSandbox{logger: zap.NewNop()}
	// No memory/CPU caps → sync callback is a no-op returning nil.
	sync := sb.cgroupSync(Policy{Resources: ResPolicy{}})
	assert.NoError(t, sync(1))
}
