package config

import "os"

type Config struct {
	Port         string
	DBPath       string // SQLite file path (default: "cerberus.db", use ":memory:" for tests)
	MigrationDir string
	LogLevel     string
	LLMModel     string
	LLMAPIKey    string
	LLMBaseURL   string // optional: overrides the provider's default API URL
}

func Load() *Config {
	cfg := &Config{
		Port:         getEnv("CERBERUS_PORT", "8090"),
		DBPath:       getEnv("CERBERUS_DB_PATH", "cerberus.db"),
		MigrationDir: getEnv("CERBERUS_MIGRATION_DIR", "migrations"),
		LogLevel:     getEnv("CERBERUS_LOG_LEVEL", "info"),
		LLMModel:     getEnv("CERBERUS_LLM_MODEL", "claude-sonnet-4-6"),
		LLMBaseURL:   os.Getenv("CERBERUS_LLM_BASE_URL"),
	}

	// API key resolution: explicit CERBERUS key first, then provider-native keys
	cfg.LLMAPIKey = resolveAPIKey(cfg.LLMModel)

	return cfg
}

// resolveAPIKey finds the API key for the configured LLM model.
// Priority: CERBERUS_LLM_API_KEY > provider-native env var.
func resolveAPIKey(model string) string {
	// Explicit override always wins
	if key := os.Getenv("CERBERUS_LLM_API_KEY"); key != "" {
		return key
	}

	// Auto-detect from provider-native env vars
	switch {
	case isModel(model, "claude"):
		return os.Getenv("ANTHROPIC_API_KEY")
	case isModel(model, "gpt"):
		return os.Getenv("OPENAI_API_KEY")
	case isModel(model, "gemini"):
		return os.Getenv("GEMINI_API_KEY")
	}
	return ""
}

func isModel(model, prefix string) bool {
	return len(model) >= len(prefix) && model[:len(prefix)] == prefix
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
