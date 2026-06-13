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
		LLMBaseURL:   resolveBaseURL(settings, detect.Profile{}),
		LLMProvider:  os.Getenv("CERBERUS_LLM_PROVIDER"),
		CLIProfile:   profile,
	}

	// API key resolution: explicit CERBERUS key first, then Claude Code config.
	cfg.LLMAPIKey = resolveAPIKey(cfg.LLMModel, settings, detect.Profile{})
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
// detected host CLI's credential prefix, then the historical ANTHROPIC default
// (so unknown CLIs are unchanged). Environment beats settings.json at each tier.
func resolveBaseURL(settings map[string]string, p detect.Profile) string {
	if v := os.Getenv("CERBERUS_LLM_BASE_URL"); v != "" {
		return v
	}
	if p.EnvPrefix != "" {
		if v := os.Getenv(p.EnvPrefix + "_BASE_URL"); v != "" {
			return v
		}
		if v := settings[p.EnvPrefix+"_BASE_URL"]; v != "" {
			return v
		}
	}
	// Graceful fallback: historical anthropic default for unknown CLIs.
	if v := os.Getenv("ANTHROPIC_BASE_URL"); v != "" {
		return v
	}
	if v := settings["ANTHROPIC_BASE_URL"]; v != "" {
		return v
	}
	return ""
}

// resolveAPIKey finds the API key for the configured LLM.
// Priority: CERBERUS_LLM_API_KEY > detected CLI's credential prefix
// (env, then settings.json) > model-name inference (env only). Model-name
// inference is retained as a graceful fallback so unknown CLIs — and models
// whose provider differs from the host CLI's — keep working.
func resolveAPIKey(model string, settings map[string]string, p detect.Profile) string {
	if key := os.Getenv("CERBERUS_LLM_API_KEY"); key != "" {
		return key
	}
	if p.EnvPrefix != "" {
		if key := providerKey(p.EnvPrefix, settings); key != "" {
			return key
		}
	}
	switch {
	case isModel(model, "gpt"):
		return os.Getenv("OPENAI_API_KEY")
	case isModel(model, "gemini"):
		return os.Getenv("GEMINI_API_KEY")
	default:
		return providerKey("ANTHROPIC", settings)
	}
}

// providerKey returns the first non-empty credential for an env prefix,
// checking environment then settings.json. AUTH_TOKEN is Anthropic-specific but
// harmless to check for other prefixes (always unset, skipped).
func providerKey(prefix string, settings map[string]string) string {
	if key := os.Getenv(prefix + "_API_KEY"); key != "" {
		return key
	}
	if key := os.Getenv(prefix + "_AUTH_TOKEN"); key != "" {
		return key
	}
	if key := settings[prefix+"_API_KEY"]; key != "" {
		return key
	}
	if key := settings[prefix+"_AUTH_TOKEN"]; key != "" {
		return key
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
