package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFromFile_NoEnv(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "project.yaml")
	baseYAML := `
project:
  name: my-app
services:
  - name: api
    url: "http://localhost:8080"
settings:
  confidence_threshold: 0.8
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(baseYAML), 0644))

	cfg, err := LoadFromFile(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "my-app", cfg.Project.Name)
	assert.Equal(t, "http://localhost:8080", cfg.Services[0].URL)
	assert.InDelta(t, 0.8, cfg.Settings.ConfidenceThreshold, 0.01)
}

func TestLoadFromFile_EnvOverlay(t *testing.T) {
	dir := t.TempDir()

	// Base config.
	cfgPath := filepath.Join(dir, "project.yaml")
	baseYAML := `
project:
  name: my-app
services:
  - name: api
    url: "http://localhost:8080"
settings:
  confidence_threshold: 0.8
  ai_budget:
    session_total_tokens: 200000
    model: claude-sonnet-4-6
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(baseYAML), 0644))

	// Staging overlay: only override what changes.
	overlayYAML := `
services:
  - name: api
    url: "https://staging.my-app.io"
settings:
  confidence_threshold: 0.9
  ai_budget:
    session_total_tokens: 400000
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "project.staging.yaml"), []byte(overlayYAML), 0644))

	t.Setenv("CERBERUS_ENV", "staging")

	cfg, err := LoadFromFile(cfgPath)
	require.NoError(t, err)

	// Overridden fields.
	assert.Equal(t, "https://staging.my-app.io", cfg.Services[0].URL)
	assert.InDelta(t, 0.9, cfg.Settings.ConfidenceThreshold, 0.01)
	assert.Equal(t, 400000, cfg.Settings.AIBudget.SessionTotalTokens)

	// Base fields preserved.
	assert.Equal(t, "my-app", cfg.Project.Name)
	assert.Equal(t, "claude-sonnet-4-6", cfg.Settings.AIBudget.Model)
}

func TestLoadFromFile_EnvOverlay_MissingFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "project.yaml")
	baseYAML := `
project:
  name: my-app
settings:
  confidence_threshold: 0.7
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(baseYAML), 0644))

	// CERBERUS_ENV set but no overlay file — should not error.
	t.Setenv("CERBERUS_ENV", "prod")

	cfg, err := LoadFromFile(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "my-app", cfg.Project.Name)
}

func TestLoadFromFile_EnvOverlay_EnvInterpolation(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "project.yaml")
	baseYAML := `
project:
  name: my-app
settings:
  confidence_threshold: 0.7
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(baseYAML), 0644))

	overlayYAML := `
services:
  - name: api
    url: "${API_URL}"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "project.staging.yaml"), []byte(overlayYAML), 0644))

	t.Setenv("CERBERUS_ENV", "staging")
	t.Setenv("API_URL", "https://staging.example.com")

	cfg, err := LoadFromFile(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "https://staging.example.com", cfg.Services[0].URL)
}
