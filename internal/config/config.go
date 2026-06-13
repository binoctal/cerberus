package config

import (
	"os"

	"github.com/binoctal/cerberus/internal/detect"
)

type Config struct {
	Port         string
	DBPath       string // SQLite file path (default: "cerberus.db", use ":memory:" for tests)
	MigrationDir string
	LogLevel     string
	LLMModel     string
	LLMAPIKey    string
	LLMBaseURL   string         // optional: overrides the provider's default API URL
	LLMProvider  string         // optional: "anthropic"|"openai"|"gemini"|"mock"; overrides model-based detection
	CLIProfile   detect.Profile // resolved host CLI identity
	TierModels   TierModels     // head → model tier (empty when CLI unknown)
}

func Load() *Config {
	settings := loadClaudeCodeEnv()
	profile := detect.Detect()
	cfg := &Config{
		Port:         getEnv("CERBERUS_PORT", "8090"),
		DBPath:       getEnv("CERBERUS_DB_PATH", "cerberus.db"),
		MigrationDir: getEnv("CERBERUS_MIGRATION_DIR", "migrations"),
		LogLevel:     getEnv("CERBERUS_LOG_LEVEL", "info"),
		LLMModel:     resolveModel(settings),
		LLMBaseURL:   resolveBaseURL(settings),
		LLMProvider:  os.Getenv("CERBERUS_LLM_PROVIDER"),
		CLIProfile:   profile,
	}

	// API key resolution: explicit CERBERUS key first, then Claude Code config.
	cfg.LLMAPIKey = resolveAPIKey(cfg.LLMModel, settings)
	cfg.TierModels = resolveTierModels(profile.CLI, settings)

	return cfg
}

// resolveModel picks the LLM model: explicit env override, then the Claude Code
// default sonnet model, then the built-in default.
func resolveModel(settings map[string]string) string {
	if v := os.Getenv("CERBERUS_LLM_MODEL"); v != "" {
		return v
	}
	if v := settings["ANTHROPIC_DEFAULT_SONNET_MODEL"]; v != "" {
		return v
	}
	return "claude-sonnet-4-6"
}

// resolveBaseURL picks the base URL: explicit CERBERUS override, then the
// Claude Code base URL (env, then settings.json), then none.
func resolveBaseURL(settings map[string]string) string {
	if v := os.Getenv("CERBERUS_LLM_BASE_URL"); v != "" {
		return v
	}
	if v := os.Getenv("ANTHROPIC_BASE_URL"); v != "" {
		return v
	}
	if v := settings["ANTHROPIC_BASE_URL"]; v != "" {
		return v
	}
	return ""
}

// resolveAPIKey finds the API key for the configured LLM.
// Priority: CERBERUS_LLM_API_KEY > provider-native key. Non-Anthropic models
// (gpt, gemini) use their own keys exclusively; everything else (claude, glm,
// and Anthropic-compatible endpoints) shares Claude Code's ANTHROPIC_API_KEY
// or ANTHROPIC_AUTH_TOKEN, with environment taking precedence over settings.json.
func resolveAPIKey(model string, settings map[string]string) string {
	// Explicit override always wins
	if key := os.Getenv("CERBERUS_LLM_API_KEY"); key != "" {
		return key
	}

	switch {
	case isModel(model, "gpt"):
		return os.Getenv("OPENAI_API_KEY")
	case isModel(model, "gemini"):
		return os.Getenv("GEMINI_API_KEY")
	default:
		// Anthropic (claude, glm, Anthropic-compatible endpoints): reuse the
		// key configured for Claude Code. Environment beats settings.json.
		if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
			return key
		}
		if key := os.Getenv("ANTHROPIC_AUTH_TOKEN"); key != "" {
			return key
		}
		if key := settings["ANTHROPIC_API_KEY"]; key != "" {
			return key
		}
		if key := settings["ANTHROPIC_AUTH_TOKEN"]; key != "" {
			return key
		}
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
