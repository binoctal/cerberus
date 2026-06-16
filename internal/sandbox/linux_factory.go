//go:build linux

package sandbox

import "go.uber.org/zap"

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
