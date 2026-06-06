package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoad(t *testing.T) {
	os.Setenv("CERBERUS_PORT", "9090")
	defer os.Unsetenv("CERBERUS_PORT")

	cfg := Load()
	assert.Equal(t, "9090", cfg.Port)
	assert.Equal(t, "cerberus.db", cfg.DBPath)
}

func TestLoadDefaults(t *testing.T) {
	for _, key := range []string{
		"CERBERUS_PORT", "CERBERUS_DB_PATH",
		"CERBERUS_MIGRATION_DIR", "CERBERUS_LOG_LEVEL", "CERBERUS_LLM_MODEL",
		"CERBERUS_LLM_API_KEY", "ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY",
	} {
		os.Unsetenv(key)
	}

	cfg := Load()
	assert.Equal(t, "8090", cfg.Port)
	assert.Equal(t, "cerberus.db", cfg.DBPath)
	assert.Equal(t, "migrations", cfg.MigrationDir)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, "claude-sonnet-4-6", cfg.LLMModel)
	assert.Equal(t, "", cfg.LLMAPIKey, "no key when all env vars unset")
}

func TestDBPath(t *testing.T) {
	cfg := &Config{DBPath: "/tmp/test.db"}
	assert.Equal(t, "/tmp/test.db", cfg.DBPath)
}

func TestAPIKeyExplicit(t *testing.T) {
	for _, key := range []string{
		"CERBERUS_LLM_API_KEY", "ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY",
	} {
		os.Unsetenv(key)
	}

	os.Setenv("CERBERUS_LLM_API_KEY", "explicit-key")
	defer os.Unsetenv("CERBERUS_LLM_API_KEY")

	cfg := Load()
	assert.Equal(t, "explicit-key", cfg.LLMAPIKey, "CERBERUS_LLM_API_KEY takes priority")
}

func TestAPIKeyAutoDetect(t *testing.T) {
	for _, key := range []string{
		"CERBERUS_LLM_API_KEY", "ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY",
		"CERBERUS_LLM_MODEL",
	} {
		os.Unsetenv(key)
	}

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
			os.Unsetenv(tt.envKey)
			os.Setenv("CERBERUS_LLM_MODEL", tt.model)
			os.Setenv(tt.envKey, tt.envValue)
			defer func() {
				os.Unsetenv(tt.envKey)
				os.Unsetenv("CERBERUS_LLM_MODEL")
			}()

			cfg := Load()
			assert.Equal(t, tt.envValue, cfg.LLMAPIKey,
				"should auto-detect %s for model %s", tt.envKey, tt.model)
		})
	}
}
