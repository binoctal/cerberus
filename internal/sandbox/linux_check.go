//go:build linux

package sandbox

import (
	"runtime"

	"github.com/criyle/go-sandbox/pkg/cgroup"
	"go.uber.org/zap"
)

// IsAvailable returns whether the sandbox can provide real isolation
func (s *LinuxSandbox) IsAvailable() bool {
	return s.available
}

// checkAvailability tests whether namespace isolation is available
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
