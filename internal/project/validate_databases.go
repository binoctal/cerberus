package project

import (
	"fmt"
)

// validateDatabases checks database configuration
func validateDatabases(cfg *Config, ve *ValidationError) {
	seenDB := make(map[string]bool)
	for i, d := range cfg.Databases {
		if d.Name == "" {
			ve.add(fmt.Sprintf("databases[%d]: name is required", i))
		} else if seenDB[d.Name] {
			ve.add(fmt.Sprintf("databases[%d]: duplicate database name %q", i, d.Name))
		} else {
			seenDB[d.Name] = true
		}
	}
}
