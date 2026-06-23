package discover

import "github.com/binoctal/cerberus/internal/project"

// MergeIntoConfig appends discovered services whose Name is not already
// present in cfg.Services. Existing entries are left untouched so hand-written
// overrides (domain, key-bearing headers, path_prefix) are preserved.
// Returns the names that were appended.
func MergeIntoConfig(cfg *project.Config, discovered []project.Service) []string {
	existing := make(map[string]bool, len(cfg.Services))
	for _, s := range cfg.Services {
		existing[s.Name] = true
	}
	var added []string
	for _, s := range discovered {
		if existing[s.Name] {
			continue
		}
		cfg.Services = append(cfg.Services, s)
		existing[s.Name] = true
		added = append(added, s.Name)
	}
	return added
}
