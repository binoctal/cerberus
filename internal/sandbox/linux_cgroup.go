//go:build linux

package sandbox

import (
	"fmt"

	"github.com/criyle/go-sandbox/pkg/cgroup"
	"go.uber.org/zap"
)

// cgroupSync returns a SyncFunc that attaches the child process to a cgroup
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
		defer func() { _ = cg.Destroy() }()

		if p.Resources.MaxMemoryMB > 0 {
			if err := cg.SetMemoryLimit(uint64(p.Resources.MaxMemoryMB) * 1024 * 1024); err != nil {
				s.logger.Warn("failed to set memory limit", zap.Error(err))
			}
		}

		return cg.AddProc(pid)
	}
}
