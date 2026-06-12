package sandbox

import "context"

// NoOpSandbox is a no-op sandbox that imposes no restrictions.
type NoOpSandbox struct{}

// Apply returns the context unchanged with a no-op cleanup.
func (NoOpSandbox) Apply(ctx context.Context, _ Policy) (context.Context, func(), error) {
	return ctx, func() {}, nil
}
