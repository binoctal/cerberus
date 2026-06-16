//go:build linux

package sandbox

import (
	"context"
	"fmt"
	"time"

	"github.com/criyle/go-sandbox/runner"
	unshare "github.com/criyle/go-sandbox/runner/unshare"
)

// ExecCommand runs a command with sandbox isolation when available,
// falling back to direct os/exec when the sandbox is unavailable.
func (s *LinuxSandbox) ExecCommand(ctx context.Context, cmd string, args []string, env []string, dir string, policy Policy) (string, string, int, error) {
	if !s.available {
		return NoOpSandbox{}.ExecCommand(ctx, cmd, args, env, dir, policy)
	}

	// Prepend the command to args for runner format.
	fullArgs := append([]string{cmd}, args...)
	result := s.RunInSandbox(ctx, fullArgs, env, policy)

	exitCode := 0
	switch result.Status {
	case runner.StatusRunnerError:
		return "", "", -1, fmt.Errorf("sandbox runner error: %s", result.Error)
	case runner.StatusTimeLimitExceeded:
		exitCode = -1
	case runner.StatusMemoryLimitExceeded:
		exitCode = -1
	case runner.StatusSignalled:
		exitCode = -1
	}

	return "", "", exitCode, nil
}

// RunInSandbox executes a command inside the sandbox with the given policy.
// This is the primary entry point for sandboxed execution.
func (s *LinuxSandbox) RunInSandbox(ctx context.Context, args []string, env []string, policy Policy) runner.Result {
	if !s.available {
		return runner.Result{Status: runner.StatusRunnerError, Error: "sandbox not available"}
	}

	mounts := s.buildMounts(policy)
	limits := s.buildRLimits(policy)
	filter := s.buildSeccompFilter(policy)

	memBytes := uint64(policy.Resources.MaxMemoryMB) * 1024 * 1024
	r := &unshare.Runner{
		Args:    args,
		Env:     env,
		Files:   []uintptr{0, 1, 2},
		RLimits: limits,
		Limit: runner.Limit{
			TimeLimit:   time.Duration(policy.Resources.Timeout) * time.Second,
			MemoryLimit: runner.Size(memBytes),
		},
		Seccomp:  filter,
		Mounts:   mounts,
		SyncFunc: s.cgroupSync(policy),
	}

	return r.Run(ctx)
}
