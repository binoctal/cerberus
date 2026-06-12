//go:build !linux

package sandbox

import (
	"context"

	"go.uber.org/zap"
)

// LinuxSandbox is a stub for non-Linux platforms.
type LinuxSandbox struct{}

// NewLinuxSandbox returns a LinuxSandbox stub that is never available on non-Linux.
func NewLinuxSandbox(logger *zap.Logger) *LinuxSandbox {
	logger.Warn("linux sandbox not available: not running on Linux")
	return &LinuxSandbox{}
}

// IsAvailable always returns false on non-Linux platforms.
func (s *LinuxSandbox) IsAvailable() bool {
	return false
}

// Apply returns the context unchanged (no-op on non-Linux).
func (s *LinuxSandbox) Apply(ctx context.Context, _ Policy) (context.Context, func(), error) {
	return ctx, func() {}, nil
}
