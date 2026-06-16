//go:build linux

package sandbox

import (
	"github.com/criyle/go-sandbox/pkg/mount"
	"go.uber.org/zap"
)

// buildMounts creates mount parameters from the filesystem policy
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
