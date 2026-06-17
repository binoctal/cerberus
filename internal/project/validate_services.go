package project

import (
	"fmt"
)

// validateServices checks service configuration
func validateServices(cfg *Config, ve *ValidationError) {
	seenService := make(map[string]bool)
	for i, s := range cfg.Services {
		if s.Name == "" {
			ve.add(fmt.Sprintf("services[%d]: name is required", i))
		} else if seenService[s.Name] {
			ve.add(fmt.Sprintf("services[%d]: duplicate service name %q", i, s.Name))
		} else {
			seenService[s.Name] = true
		}
		if s.URL != "" && !isValidURL(s.URL) {
			ve.add(fmt.Sprintf("services[%d]: invalid URL %q (must start with http:// or https://)", i, s.URL))
		}
	}
}
