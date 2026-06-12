// Package sandbox defines the execution isolation interface and policy types.
package sandbox

import "context"

// Sandbox provides execution isolation for actions.
type Sandbox interface {
	// Apply constrains the context with the given policy and returns a cleanup function.
	Apply(ctx context.Context, policy Policy) (context.Context, func(), error)
}

// Policy defines resource constraints for action execution.
type Policy struct {
	FS        FSPolicy
	Network   NetPolicy
	Resources ResPolicy
}

// FSPolicy controls filesystem access.
type FSPolicy struct {
	ReadOnly  []string
	ReadWrite []string
	Denied    []string
}

// NetPolicy controls network access.
type NetPolicy struct {
	AllowOutbound bool
	AllowHosts    []string
}

// ResPolicy controls resource limits.
type ResPolicy struct {
	MaxMemoryMB   int
	MaxCPUPercent int
	Timeout       int // seconds, 0 = no limit
}
