//go:build linux

package sandbox

import (
	"github.com/criyle/go-sandbox/pkg/seccomp"
	"github.com/criyle/go-sandbox/pkg/seccomp/libseccomp"
	"go.uber.org/zap"
)

// buildSeccompFilter creates a seccomp BPF filter
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
