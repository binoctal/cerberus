package project

import (
	"fmt"
	"strings"
)

// validateInvariants checks invariants configuration
func validateInvariants(cfg *Config, ve *ValidationError) {
	validSeverities := map[string]bool{"": true, "low": true, "medium": true, "high": true, "critical": true}
	seenInv := make(map[string]bool)
	for i, inv := range cfg.Invariants {
		if inv.ID == "" {
			ve.add(fmt.Sprintf("invariants[%d]: id is required", i))
		} else if seenInv[inv.ID] {
			ve.add(fmt.Sprintf("invariants[%d]: duplicate invariant id %q", i, inv.ID))
		} else {
			seenInv[inv.ID] = true
		}
		if !validSeverities[strings.ToLower(inv.Severity)] {
			ve.add(fmt.Sprintf("invariants[%d]: invalid severity %q (use low, medium, high, or critical)", i, inv.Severity))
		}
		if inv.Check == "" {
			ve.add(fmt.Sprintf("invariants[%d]: check is required", i))
		}
	}
}
