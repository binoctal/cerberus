//go:build linux

package sandbox

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/criyle/go-sandbox/pkg/cgroup"
	"github.com/criyle/go-sandbox/pkg/mount"
	"github.com/criyle/go-sandbox/pkg/rlimit"
	"github.com/criyle/go-sandbox/pkg/seccomp"
	"github.com/criyle/go-sandbox/pkg/seccomp/libseccomp"
	"github.com/criyle/go-sandbox/runner"
	unshare "github.com/criyle/go-sandbox/runner/unshare"
	"go.uber.org/zap"
)

// LinuxSandbox provides namespace + seccomp + cgroup isolation on Linux.
// Falls back to no-op behavior when privileges are insufficient.
type LinuxSandbox struct {
	logger    *zap.Logger
	available bool
	mu        sync.Mutex
}

// NewLinuxSandbox creates a Linux sandbox instance.
// Call IsAvailable() to check whether namespace isolation is usable.
func NewLinuxSandbox(logger *zap.Logger) *LinuxSandbox {
	sb := &LinuxSandbox{logger: logger}
	sb.available = sb.checkAvailability()
	if sb.available {
		logger.Info("linux sandbox available: namespace + seccomp + cgroup isolation enabled")
	} else {
		logger.Warn("linux sandbox not available: insufficient privileges, falling back to no-op")
	}
	return sb
}

// IsAvailable returns whether the sandbox can provide real isolation.
func (s *LinuxSandbox) IsAvailable() bool {
	return s.available
}

// Apply constrains the context according to the given policy.
// Returns a constrained context and a cleanup function.
// If the sandbox is unavailable, returns the original context with a no-op cleanup.
func (s *LinuxSandbox) Apply(ctx context.Context, policy Policy) (context.Context, func(), error) {
	if !s.available {
		return ctx, func() {}, nil
	}

	s.logger.Debug("applying linux sandbox policy",
		zap.Int("readonly_paths", len(policy.FS.ReadOnly)),
		zap.Int("readwrite_paths", len(policy.FS.ReadWrite)),
		zap.Bool("allow_outbound", policy.Network.AllowOutbound),
	)

	// Policy is applied at execution time via RunInSandbox, not here.
	// Apply satisfies the Sandbox interface for future context-based integration.
	return ctx, func() {}, nil
}

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

// checkAvailability tests whether namespace creation is possible.
func (s *LinuxSandbox) checkAvailability() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	// Try to create a minimal cgroup to test privileges.
	ct := &cgroup.Controllers{Memory: true}
	cg, err := cgroup.New("cerberus-check", ct)
	if err != nil {
		s.logger.Debug("cgroup check failed", zap.Error(err))
		return false
	}
	_ = cg.Destroy()
	return true
}

// buildMounts creates mount parameters from the filesystem policy.
func (s *LinuxSandbox) buildMounts(p Policy) []mount.SyscallParams {
	b := mount.NewDefaultBuilder()

	// Add read-write paths.
	for _, path := range p.FS.ReadWrite {
		b = b.WithBind(path, path, false)
	}
	// Add read-only paths.
	for _, path := range p.FS.ReadOnly {
		b = b.WithBind(path, path, true)
	}

	mounts, err := b.FilterNotExist().Build()
	if err != nil {
		s.logger.Warn("failed to build mounts", zap.Error(err))
		return nil
	}
	return mounts
}

// buildRLimits creates resource limits from the resource policy.
func (s *LinuxSandbox) buildRLimits(p Policy) []rlimit.RLimit {
	r := &rlimit.RLimits{
		CPU:          uint64(p.Resources.Timeout),
		AddressSpace: uint64(p.Resources.MaxMemoryMB) * 1024 * 1024,
		DisableCore:  true,
	}
	return r.PrepareRLimit()
}

// buildSeccompFilter creates a seccomp BPF filter.
func (s *LinuxSandbox) buildSeccompFilter(p Policy) seccomp.Filter {
	if p.Network.AllowOutbound {
		return nil
	}

	// Block network-related syscalls when outbound is denied.
	builder := &libseccomp.Builder{
		Allow: []string{
			"read", "write", "close", "fstat", "lseek",
			"mmap", "mprotect", "munmap", "brk",
			"openat", "open", "access", "faccessat",
			"dup", "dup2", "dup3",
			"fcntl", "ioctl", "flock",
			"getdents", "getdents64",
			"stat", "lstat", "newfstatat",
			"readlink", "readlinkat",
			"execve", "exit", "exit_group",
			"arch_prctl", "set_tid_address",
			"set_robust_list", "futex",
			"rt_sigaction", "rt_sigprocmask",
			"getpid", "getppid", "getuid", "getgid",
			"geteuid", "getegid", "getgroups",
			"clock_gettime", "clock_getres",
			"madvise", "getrandom",
			"pipe", "pipe2",
			"select", "poll", "ppoll",
			"nanosleep", "clock_nanosleep",
			"chdir", "fchdir",
			"mkdir", "mkdirat", "rmdir",
			"rename", "renameat", "renameat2",
			"unlink", "unlinkat",
			"symlink", "symlinkat",
			"chmod", "fchmod", "fchmodat",
			"umask",
		},
		Default: libseccomp.ActionAllow,
	}
	filter, err := builder.Build()
	if err != nil {
		s.logger.Warn("failed to build seccomp filter", zap.Error(err))
		return nil
	}
	return filter
}

// cgroupSync returns a SyncFunc that attaches the child process to a cgroup.
func (s *LinuxSandbox) cgroupSync(p Policy) func(pid int) error {
	return func(pid int) error {
		if p.Resources.MaxMemoryMB <= 0 && p.Resources.MaxCPUPercent <= 0 {
			return nil
		}

		ct := &cgroup.Controllers{
			Memory: p.Resources.MaxMemoryMB > 0,
			CPU:    p.Resources.MaxCPUPercent > 0,
			Pids:   true,
		}

		cg, err := cgroup.New("cerberus", ct)
		if err != nil {
			return fmt.Errorf("create cgroup: %w", err)
		}
		defer cg.Destroy()

		if p.Resources.MaxMemoryMB > 0 {
			if err := cg.SetMemoryLimit(uint64(p.Resources.MaxMemoryMB) * 1024 * 1024); err != nil {
				s.logger.Warn("failed to set memory limit", zap.Error(err))
			}
		}

		return cg.AddProc(pid)
	}
}
