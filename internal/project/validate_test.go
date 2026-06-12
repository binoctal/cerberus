package project

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate_ValidConfig(t *testing.T) {
	cfg := &Config{
		Services: []Service{
			{Name: "web", URL: "http://localhost:3000", Health: "/"},
		},
		Actors: []Actor{
			{Name: "admin", Credentials: CredentialRef{Email: "a@b.c", Password: "pw"}},
		},
		Invariants: []Invariant{
			{ID: "INV-001", Description: "test", Severity: "high", Check: "SELECT 1", Assertion: "true"},
		},
		Settings: Settings{
			MaxDuration:         "30m",
			ConfidenceThreshold: 0.7,
			AutoFix:             "low_only",
			AIBudget:            AIBudget{SessionTotalTokens: 200000, PerCallLimit: 10000},
			CostAlerts:          CostAlerts{WarnAtPct: 80, StopAtPct: 100},
		},
	}
	assert.NoError(t, cfg.Validate())
}

func TestValidate_DefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.NoError(t, cfg.Validate())
}

func TestValidate_EmptyConfig(t *testing.T) {
	cfg := &Config{}
	assert.NoError(t, cfg.Validate(), "empty config should be valid (all optional)")
}

func TestValidate_ServiceErrors(t *testing.T) {
	cfg := &Config{
		Services: []Service{
			{Name: "", URL: "http://ok.com"},
			{Name: "dup", URL: "http://ok.com"},
			{Name: "dup", URL: "http://ok.com"},
			{Name: "bad", URL: "ftp://invalid"},
			{Name: "good", URL: "https://valid.com"},
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	ve := err.(*ValidationError)
	assert.Contains(t, ve.Errors[0], "name is required")
	assert.Contains(t, ve.Errors[1], "duplicate service name")
	assert.Contains(t, ve.Errors[2], "invalid URL")
	assert.Len(t, ve.Errors, 3)
}

func TestValidate_ActorErrors(t *testing.T) {
	cfg := &Config{
		Actors: []Actor{
			{Name: ""},
			{Name: "same"},
			{Name: "same"},
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	ve := err.(*ValidationError)
	assert.Contains(t, ve.Errors[0], "name is required")
	assert.Contains(t, ve.Errors[1], "duplicate actor name")
}

func TestValidate_DatabaseErrors(t *testing.T) {
	cfg := &Config{
		Databases: []Database{
			{Name: ""},
			{Name: "dup"},
			{Name: "dup"},
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	ve := err.(*ValidationError)
	assert.Contains(t, ve.Errors[0], "name is required")
	assert.Contains(t, ve.Errors[1], "duplicate database name")
}

func TestValidate_InvariantErrors(t *testing.T) {
	cfg := &Config{
		Invariants: []Invariant{
			{ID: "", Check: "SELECT 1"},
			{ID: "DUP", Check: "SELECT 1"},
			{ID: "DUP", Check: "SELECT 1"},
			{ID: "OK", Severity: "invalid", Check: ""},
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	ve := err.(*ValidationError)
	// 0: empty ID, 2: dup DUP, 3: invalid severity, 3: missing check
	assert.Len(t, ve.Errors, 4)
}

func TestValidate_SettingsErrors(t *testing.T) {
	cfg := &Config{
		Settings: Settings{
			ConfidenceThreshold: 1.5,
			MaxDuration:         "not-a-duration",
			AutoFix:             "invalid",
			AIBudget:            AIBudget{SessionTotalTokens: -1, PerCallLimit: -1},
			CostAlerts:          CostAlerts{WarnAtPct: 150, StopAtPct: 200},
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	ve := err.(*ValidationError)
	// confidence, max_duration, auto_fix, tokens, per_call, warn_pct, stop_pct
	assert.GreaterOrEqual(t, len(ve.Errors), 6)
}

func TestValidate_CostAlertWarnExceedsStop(t *testing.T) {
	cfg := &Config{
		Settings: Settings{
			CostAlerts: CostAlerts{WarnAtPct: 90, StopAtPct: 80},
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	ve := err.(*ValidationError)
	found := false
	for _, e := range ve.Errors {
		if contains(e, "warn_at_pct") {
			found = true
			break
		}
	}
	assert.True(t, found, "should warn about warn_at_pct > stop_at_pct")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
