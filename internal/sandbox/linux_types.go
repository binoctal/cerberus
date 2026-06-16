//go:build linux

package sandbox

import "go.uber.org/zap"

// LinuxSandbox provides namespace + seccomp + cgroup isolation on Linux.
// Falls back to no-op behavior when privileges are insufficient.
type LinuxSandbox struct {
	logger    *zap.Logger
	available bool
}
