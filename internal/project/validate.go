package project

import (
	"net/url"
	"strings"
)

// ValidationError collects all config problems found during validation.
type ValidationError struct {
	Errors []string
}

func (ve *ValidationError) Error() string {
	return "project config validation failed: " + strings.Join(ve.Errors, "; ")
}

func (ve *ValidationError) add(msg string) {
	ve.Errors = append(ve.Errors, msg)
}

// Validate checks the config for semantic correctness and returns a
// ValidationError containing all problems found, or nil if valid.
func (cfg *Config) Validate() error {
	var ve ValidationError

	// Phase 1: Validate services
	validateServices(cfg, &ve)

	// Phase 2: Validate actors
	validateActors(cfg, &ve)

	// Phase 3: Validate databases
	validateDatabases(cfg, &ve)

	// Phase 4: Validate invariants
	validateInvariants(cfg, &ve)

	// Phase 5: Validate settings
	validateSettings(cfg, &ve)

	if len(ve.Errors) == 0 {
		return nil
	}
	return &ve
}

func isValidURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}
