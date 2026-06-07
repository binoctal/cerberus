package prompts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRegistry_LoadDefaults(t *testing.T) {
	r := NewRegistry("", zap.NewNop())

	// Should have all 14 prompts.
	assert.GreaterOrEqual(t, len(r.Keys()), 14)

	// Check specific prompts.
	assert.Contains(t, r.Get("agent_steer_system"), "test execution agent")
	assert.Contains(t, r.Get("examiner_judge_system"), "test verdict judge")
	assert.Contains(t, r.Get("scout_plan_system"), "test planning agent")
	assert.Contains(t, r.Get("agent_recover_output"), "Respond with JSON")
}

func TestRegistry_MustGet(t *testing.T) {
	r := NewRegistry("", zap.NewNop())
	assert.NotPanics(t, func() { r.MustGet("agent_steer_system") })
	assert.Panics(t, func() { r.MustGet("nonexistent_prompt") })
}

func TestRegistry_Set(t *testing.T) {
	r := NewRegistry("", zap.NewNop())
	r.Set("custom_prompt", "hello world")
	assert.Equal(t, "hello world", r.Get("custom_prompt"))
}

func TestRegistry_ProjectOverrides(t *testing.T) {
	// Create temp overrides directory.
	dir := t.TempDir()

	// Write an override file.
	override := "You are a CUSTOM steer agent.\nDo things differently."
	err := os.WriteFile(filepath.Join(dir, "agent_steer_system.txt"), []byte(override), 0644)
	require.NoError(t, err)

	r := NewRegistry(dir, zap.NewNop())

	// Should use the override.
	assert.Equal(t, override, r.Get("agent_steer_system"))

	// Non-overridden prompts should still have defaults.
	assert.Contains(t, r.Get("examiner_judge_system"), "test verdict judge")
}

func TestRegistry_NoOverridesDir(t *testing.T) {
	r := NewRegistry("/nonexistent/path", zap.NewNop())
	// Should still load defaults without error.
	assert.GreaterOrEqual(t, len(r.Keys()), 14)
}

func TestRegistry_OverrideOnlyTxtFiles(t *testing.T) {
	dir := t.TempDir()

	// Write a .yaml file — should be ignored.
	err := os.WriteFile(filepath.Join(dir, "agent_steer_system.yaml"), []byte("ignored"), 0644)
	require.NoError(t, err)

	r := NewRegistry(dir, zap.NewNop())
	// Should still have default, not the yaml content.
	assert.Contains(t, r.Get("agent_steer_system"), "test execution agent")
}
