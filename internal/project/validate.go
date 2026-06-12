package project

import (
	"fmt"
	"net/url"
	"strings"
	"time"
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

	// Services: unique names, valid URLs.
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

	// Actors: unique names.
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

	// Databases: unique names.
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

	// Invariants: unique IDs, valid severity.
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

	// Settings.
	if cfg.Settings.ConfidenceThreshold < 0 || cfg.Settings.ConfidenceThreshold > 1 {
		ve.add(fmt.Sprintf("settings.confidence_threshold: must be between 0 and 1, got %.2f", cfg.Settings.ConfidenceThreshold))
	}
	if cfg.Settings.MaxDuration != "" {
		if _, err := time.ParseDuration(cfg.Settings.MaxDuration); err != nil {
			ve.add(fmt.Sprintf("settings.max_duration: invalid duration %q", cfg.Settings.MaxDuration))
		}
	}
	validAutoFix := map[string]bool{"": true, "off": true, "low_only": true, "aggressive": true}
	if !validAutoFix[cfg.Settings.AutoFix] {
		ve.add(fmt.Sprintf("settings.auto_fix: invalid value %q (use off, low_only, or aggressive)", cfg.Settings.AutoFix))
	}
	if cfg.Settings.AIBudget.SessionTotalTokens < 0 {
		ve.add(fmt.Sprintf("settings.ai_budget.session_total_tokens: must be >= 0, got %d", cfg.Settings.AIBudget.SessionTotalTokens))
	}
	if cfg.Settings.AIBudget.PerCallLimit < 0 {
		ve.add(fmt.Sprintf("settings.ai_budget.per_call_limit: must be >= 0, got %d", cfg.Settings.AIBudget.PerCallLimit))
	}
	if cfg.Settings.CostAlerts.WarnAtPct < 0 || cfg.Settings.CostAlerts.WarnAtPct > 100 {
		ve.add(fmt.Sprintf("settings.cost_alerts.warn_at_pct: must be 0-100, got %d", cfg.Settings.CostAlerts.WarnAtPct))
	}
	if cfg.Settings.CostAlerts.StopAtPct < 0 || cfg.Settings.CostAlerts.StopAtPct > 100 {
		ve.add(fmt.Sprintf("settings.cost_alerts.stop_at_pct: must be 0-100, got %d", cfg.Settings.CostAlerts.StopAtPct))
	}
	if cfg.Settings.CostAlerts.WarnAtPct > cfg.Settings.CostAlerts.StopAtPct {
		ve.add("settings.cost_alerts: warn_at_pct should not exceed stop_at_pct")
	}

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
