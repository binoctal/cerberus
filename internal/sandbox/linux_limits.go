//go:build linux

package sandbox

import "github.com/criyle/go-sandbox/pkg/rlimit"

// buildRLimits creates resource limits from the resource policy
func (s *LinuxSandbox) buildRLimits(p Policy) []rlimit.RLimit {
	r := &rlimit.RLimits{
		CPU:          uint64(p.Resources.Timeout),
		AddressSpace: uint64(p.Resources.MaxMemoryMB) * 1024 * 1024,
		DisableCore:  true,
	}
	return r.PrepareRLimit()
}
