package config

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/detect"
)

func TestLoad(t *testing.T) {
	t.Setenv("CERBERUS_PORT", "9090")
	t.Setenv("CERBERUS_DB_PATH", "cerberus.db") // Use explicit path for test

	cfg := Load()
	assert.Equal(t, "9090", cfg.Port)
	assert.Equal(t, "cerberus.db", cfg.DBPath)
	assert.NotNil(t, cfg.Paths, "Paths should be initialized")
}

func TestLoadDefaults(t *testing.T) {
	for _, key := range []string{
		"CERBERUS_PORT", "CERBERUS_DB_PATH",
		"CERBERUS_MIGRATION_DIR", "CERBERUS_LOG_LEVEL", "CERBERUS_LLM_MODEL",
		"CERBERUS_LLM_API_KEY", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "OPENAI_API_KEY", "GEMINI_API_KEY",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("CERBERUS_NO_CLAUDE_SETTINGS", "1") // ignore project .claude/settings.json

	cfg := Load()
	assert.Equal(t, "8090", cfg.Port)
	// DBPath now defaults to runtime path, not "cerberus.db"
	assert.Contains(t, cfg.DBPath, "cerberus.db", "DBPath should contain cerberus.db")
	assert.Contains(t, cfg.DBPath, ".local", "DBPath should be in user local directory")
	assert.Equal(t, "migrations", cfg.MigrationDir)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, "claude-sonnet-4-6", cfg.LLMModel)
	assert.Equal(t, "", cfg.LLMAPIKey, "no key when all env vars unset")
	assert.NotNil(t, cfg.Paths, "Paths should be initialized")
}

func TestDBPath(t *testing.T) {
	cfg := &Config{DBPath: "/tmp/test.db"}
	assert.Equal(t, "/tmp/test.db", cfg.DBPath)
}

func TestAPIKeyExplicit(t *testing.T) {
	for _, key := range []string{
		"CERBERUS_LLM_API_KEY", "ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY",
	} {
		t.Setenv(key, "")
	}

	t.Setenv("CERBERUS_LLM_API_KEY", "explicit-key")

	cfg := Load()
	assert.Equal(t, "explicit-key", cfg.LLMAPIKey, "CERBERUS_LLM_API_KEY takes priority")
}

func TestAPIKeyAutoDetect(t *testing.T) {
	for _, key := range []string{
		"CERBERUS_LLM_API_KEY", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "OPENAI_API_KEY", "GEMINI_API_KEY",
		"CERBERUS_LLM_MODEL",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("CLAUDECODE", "") // isolate model-name fallback from host-CLI detection

	tests := []struct {
		model    string
		envKey   string
		envValue string
	}{
		{"claude-sonnet-4-6", "ANTHROPIC_API_KEY", "sk-ant-123"},
		{"gpt-4.1-2025-04-14", "OPENAI_API_KEY", "sk-oai-456"},
		{"gemini-3-flash-preview", "GEMINI_API_KEY", "AIza-789"},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			t.Setenv(tt.envKey, tt.envValue)
			t.Setenv("CERBERUS_LLM_MODEL", tt.model)

			cfg := Load()
			assert.Equal(t, tt.envValue, cfg.LLMAPIKey,
				"should auto-detect %s for model %s", tt.envKey, tt.model)
		})
	}
}

func TestLoad_TierModelsUnderClaudeCode(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	// Minimal settings file is not required: tier envs come from settings.go's
	// map, but Load reads the real .claude/settings.json. For a deterministic
	// unit test, call resolveTierModels directly (covered in tier_test.go) and
	// assert Load populates CLIProfile under CLAUDECODE.
	cfg := Load()
	assert.Equal(t, detect.CLIClaudeCode, cfg.CLIProfile.CLI)
}

func TestLoad_UnknownCLI_NoTierModels(t *testing.T) {
	t.Setenv("CLAUDECODE", "")
	cfg := Load()
	assert.Equal(t, detect.CLIUnknown, cfg.CLIProfile.CLI)
	assert.Empty(t, cfg.TierModels)
}

func TestLoad_ProviderFromDetectedCLI(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CERBERUS_LLM_PROVIDER", "")
	t.Setenv("CERBERUS_NO_CLAUDE_SETTINGS", "1")
	cfg := Load()
	assert.Equal(t, "anthropic", cfg.LLMProvider, "detected Claude Code implies anthropic provider")
	assert.Equal(t, "claude-code", string(cfg.CLIProfile.CLI))
}

func TestLoad_ExplicitProviderBeatsDetection(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CERBERUS_LLM_PROVIDER", "openai")
	cfg := Load()
	assert.Equal(t, "openai", cfg.LLMProvider, "explicit CERBERUS_LLM_PROVIDER overrides detection")
}

func TestLoad_UnknownCLIProviderEmpty(t *testing.T) {
	t.Setenv("CLAUDECODE", "")
	t.Setenv("CERBERUS_LLM_PROVIDER", "")
	t.Setenv("CERBERUS_NO_CLAUDE_SETTINGS", "1")
	cfg := Load()
	assert.Equal(t, "", cfg.LLMProvider, "unknown CLI leaves provider empty so llm falls back to model detection")
	assert.Equal(t, "unknown", string(cfg.CLIProfile.CLI))
}
