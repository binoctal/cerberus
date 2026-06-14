package llm

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContextWindow_ExactMatch(t *testing.T) {
	resetRegistry()
	assert.Equal(t, 1_000_000, ContextWindow("glm-5.2[1m]"))
	assert.Equal(t, 1_000_000, ContextWindow("claude-sonnet-4-6"))
	assert.Equal(t, 1_000_000, ContextWindow("claude-opus-4-8"))
	assert.Equal(t, 200_000, ContextWindow("claude-haiku-4-5"))
	assert.Equal(t, 1_047_576, ContextWindow("gpt-4.1"))
	assert.Equal(t, 128_000, ContextWindow("gpt-4o"))
	assert.Equal(t, 1_048_576, ContextWindow("gemini-2.5-pro"))
}

func TestContextWindow_PrefixMatch(t *testing.T) {
	resetRegistry()
	// Dated/variant ids resolve to the base family via longest-prefix match.
	assert.Equal(t, 1_000_000, ContextWindow("claude-sonnet-4-6-20260205"))
	assert.Equal(t, 1_047_576, ContextWindow("gpt-4.1-2025-04-14"))
	assert.Equal(t, 1_000_000, ContextWindow("claude-opus-4-8-20260101"))
}

func TestContextWindow_PrefixLongestWins(t *testing.T) {
	resetRegistry()
	// "gpt-4o" must not be shadowed by a shorter entry.
	assert.Equal(t, 128_000, ContextWindow("gpt-4o-2024-11-20"))
}

func TestContextWindow_Unknown_Default(t *testing.T) {
	resetRegistry()
	assert.Equal(t, defaultContextWindow, ContextWindow("some-unknown-model"))
	assert.Equal(t, defaultContextWindow, ContextWindow("glm-9-preview"))
}

func TestContextWindow_CaseInsensitive(t *testing.T) {
	resetRegistry()
	// Vendors/users mix case ("GLM-4.5-Air"); registry keys are lowercase and
	// the APIs treat model ids as case-equivalent.
	assert.Equal(t, 128_000, ContextWindow("GLM-4.5-Air"))
	assert.Equal(t, 200_000, ContextWindow("GLM-4.7"))
	assert.Equal(t, 1_050_000, ContextWindow("GPT-5.5"))
	assert.Equal(t, 1_000_000, ContextWindow("CLAUDE-OPUS-4-8"))
	assert.Equal(t, 1_000_000, ContextWindow("glm-5.2[1m]")) // lowercase still works
}

func TestMaxOutput(t *testing.T) {
	resetRegistry()
	assert.Equal(t, 128_000, MaxOutput("claude-opus-4-8"))
	assert.Equal(t, 64_000, MaxOutput("claude-sonnet-4-6"))
	assert.Equal(t, 32_768, MaxOutput("gpt-4.1"))
	assert.Equal(t, 65_535, MaxOutput("gemini-2.5-pro"))
}

func TestMaxOutput_Unknown_Default(t *testing.T) {
	resetRegistry()
	assert.Equal(t, defaultMaxOutput, MaxOutput("unknown-model"))
}

// TestContextWindow_ExternalOverride proves the CERBERUS_MODELS_JSON env var
// can add/override model caps at runtime without recompiling.
func TestContextWindow_ExternalOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "override.json")
	// Add a brand-new model AND override an existing one.
	over := `{
		"defaults": {"input": 64000, "output": 2048},
		"models": {
			"custom-model": {"input": 999999, "output": 12345},
			"gpt-4o": {"input": 200000, "output": 8000}
		}
	}`
	assert.NoError(t, os.WriteFile(path, []byte(over), 0o600))

	t.Setenv("CERBERUS_MODELS_JSON", path)
	resetRegistry()
	t.Cleanup(resetRegistry) // other tests reload from embed

	// New model only in the override file.
	assert.Equal(t, 999999, ContextWindow("custom-model"))
	assert.Equal(t, 12345, MaxOutput("custom-model"))
	// Existing model overridden.
	assert.Equal(t, 200000, ContextWindow("gpt-4o"))
	// Default also overridable.
	assert.Equal(t, 64000, ContextWindow("never-seen-model"))
}
