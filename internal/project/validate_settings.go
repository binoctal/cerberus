package project

import (
	"fmt"
	"time"
)

// validateSettings checks settings configuration
func validateSettings(cfg *Config, ve *ValidationError) {
	// Confidence threshold
	if cfg.Settings.ConfidenceThreshold < 0 || cfg.Settings.ConfidenceThreshold > 1 {
		ve.add(fmt.Sprintf("settings.confidence_threshold: must be between 0 and 1, got %.2f", cfg.Settings.ConfidenceThreshold))
	}

	// Max duration
	if cfg.Settings.MaxDuration != "" {
		if _, err := time.ParseDuration(cfg.Settings.MaxDuration); err != nil {
			ve.add(fmt.Sprintf("settings.max_duration: invalid duration %q", cfg.Settings.MaxDuration))
		}
	}

	// Auto fix
	validAutoFix := map[string]bool{"": true, "off": true, "low_only": true, "aggressive": true}
	if !validAutoFix[cfg.Settings.AutoFix] {
		ve.add(fmt.Sprintf("settings.auto_fix: invalid value %q (use off, low_only, or aggressive)", cfg.Settings.AutoFix))
	}

	// AI budget
	validateAIBudget(cfg, ve)

	// Cost alerts
	validateCostAlerts(cfg, ve)
}

// validateAIBudget checks AI budget settings
func validateAIBudget(cfg *Config, ve *ValidationError) {
	if cfg.Settings.AIBudget.SessionTotalTokens < 0 {
		ve.add(fmt.Sprintf("settings.ai_budget.session_total_tokens: must be >= 0, got %d", cfg.Settings.AIBudget.SessionTotalTokens))
	}
	if cfg.Settings.AIBudget.PerCallLimit < 0 {
		ve.add(fmt.Sprintf("settings.ai_budget.per_call_limit: must be >= 0, got %d", cfg.Settings.AIBudget.PerCallLimit))
	}
}

// validateCostAlerts checks cost alert settings
func validateCostAlerts(cfg *Config, ve *ValidationError) {
	if cfg.Settings.CostAlerts.WarnAtPct < 0 || cfg.Settings.CostAlerts.WarnAtPct > 100 {
		ve.add(fmt.Sprintf("settings.cost_alerts.warn_at_pct: must be 0-100, got %d", cfg.Settings.CostAlerts.WarnAtPct))
	}
	if cfg.Settings.CostAlerts.StopAtPct < 0 || cfg.Settings.CostAlerts.StopAtPct > 100 {
		ve.add(fmt.Sprintf("settings.cost_alerts.stop_at_pct: must be 0-100, got %d", cfg.Settings.CostAlerts.StopAtPct))
	}
	if cfg.Settings.CostAlerts.WarnAtPct > cfg.Settings.CostAlerts.StopAtPct {
		ve.add("settings.cost_alerts: warn_at_pct should not exceed stop_at_pct")
	}
}
