//go:build linux

package sandbox

import (
	"context"

	"go.uber.org/zap"
)

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
