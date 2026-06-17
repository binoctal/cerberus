package project

import (
	"fmt"
)

// validateActors checks actor configuration
func validateActors(cfg *Config, ve *ValidationError) {
	seenActor := make(map[string]bool)
	for i, a := range cfg.Actors {
		if a.Name == "" {
			ve.add(fmt.Sprintf("actors[%d]: name is required", i))
		} else if seenActor[a.Name] {
			ve.add(fmt.Sprintf("actors[%d]: duplicate actor name %q", i, a.Name))
		} else {
			seenActor[a.Name] = true
		}
	}
}
