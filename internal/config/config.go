package config

import (
	"os"
	"strings"

	"github.com/binoctal/cerberus/internal/detect"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/runtime"
)

type Config struct {
	Port          string
	DBPath        string // SQLite file path (default: runtime path, use ":memory:" for tests)
	MigrationDir  string
	LogLevel      string
	LLMModel      string
	LLMAPIKey     string
	LLMBaseURL    string         // optional: overrides the provider's default API URL
	LLMProvider   string         // optional: "anthropic"|"openai"|"gemini"|"mock"; overrides model-based detection
	LLMAuthScheme llm.AuthScheme // auth header for Anthropic, derived from credential source
	CLIProfile    detect.Profile // resolved host CLI identity
	TierModels    TierModels     // head → model tier (empty when CLI unknown)
	TierContexts  TierContexts   // head → model context-window tokens (drives depth scaling)
	Paths         *runtime.Paths // runtime file paths (auto-detected)
}

func Load() *Config {
	settings := loadClaudeCodeEnv()
	profile := detect.Detect()

	// Provider priority: explicit env override, then the detected host CLI,
	// then empty (llm.NewClientWithConfig falls back to model-name detection).
	provider := os.Getenv("CERBERUS_LLM_PROVIDER")
	if provider == "" {
		provider = profile.Provider
	}

	// Get runtime paths (auto-detects development vs production).
	// Ensure() failures are tolerated here; the database open will surface
	// any real directory problem with a clear error.
	paths := runtime.GetPaths()
	_ = paths.Ensure()

	cfg := &Config{
		Port:         getEnv("CERBERUS_PORT", "8090"),
		DBPath:       getEnv("CERBERUS_DB_PATH", paths.DBPath),
		MigrationDir: getEnv("CERBERUS_MIGRATION_DIR", "migrations"),
		LogLevel:     getEnv("CERBERUS_LOG_LEVEL", "info"),
		LLMModel:     resolveModel(settings),
		LLMBaseURL:   resolveBaseURL(settings, profile),
		LLMProvider:  provider,
		CLIProfile:   profile,
		Paths:        paths,
	}

	// API key resolution: explicit CERBERUS key first, then CLI prefix, then
	// model-name inference (see resolveAPIKey).
	cfg.LLMAPIKey, cfg.LLMAuthScheme = resolveAPIKeyWithScheme(cfg.LLMModel, settings, profile)
	cfg.TierModels = resolveTierModels(profile.CLI, settings)
	cfg.TierContexts = resolveTierContexts(cfg.TierModels)

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

// resolveBaseURL picks the base URL for the LLM provider.
// Priority: CERBERUS_LLM_BASE_URL (explicit override) > target project
// settings.json > inherited env vars. Settings.json is authoritative for the
// target project; env vars are inherited from the host Claude Code session and
// may point at a different provider.
func resolveBaseURL(settings map[string]string, p detect.Profile) string {
	if v := os.Getenv("CERBERUS_LLM_BASE_URL"); v != "" {
		return v
	}
	// Settings from target project's .claude/settings.json win over inherited env.
	if p.EnvPrefix != "" {
		if v := settings[p.EnvPrefix+"_BASE_URL"]; v != "" {
			return v
		}
	}
	if v := settings["ANTHROPIC_BASE_URL"]; v != "" {
		return v
	}
	// Fallback: inherited environment (same project or no settings.json).
	if p.EnvPrefix != "" {
		if v := os.Getenv(p.EnvPrefix + "_BASE_URL"); v != "" {
			return v
		}
	}
	if v := os.Getenv("ANTHROPIC_BASE_URL"); v != "" {
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
	key, _ := resolveAPIKeyWithScheme(model, settings, p)
	return key
}

// resolveAPIKeyWithScheme resolves the credential and the auth scheme implied
// by its source. A credential from *_API_KEY (or CERBERUS_LLM_API_KEY) uses
// x-api-key; one from *_AUTH_TOKEN uses Authorization: Bearer. The scheme is
// decided at resolution time from whatever settings.json/env actually provides,
// so switching credential source flips the header with no further config.
func resolveAPIKeyWithScheme(model string, settings map[string]string, p detect.Profile) (string, llm.AuthScheme) {
	if key := os.Getenv("CERBERUS_LLM_API_KEY"); key != "" {
		return key, llm.AuthSchemeAPIKey
	}
	if p.EnvPrefix != "" {
		if key, scheme := providerKey(p.EnvPrefix, settings); key != "" {
			return key, scheme
		}
	}
	switch {
	case isModel(model, "gpt"):
		return os.Getenv("OPENAI_API_KEY"), llm.AuthSchemeAPIKey
	case isModel(model, "gemini"):
		return os.Getenv("GEMINI_API_KEY"), llm.AuthSchemeAPIKey
	default:
		return providerKey("ANTHROPIC", settings)
	}
}

// providerKey returns the first non-empty credential for an env prefix and the
// auth scheme its source implies (_API_KEY → x-api-key, _AUTH_TOKEN → Bearer).
// Settings.json (target project) takes precedence over inherited env vars.
func providerKey(prefix string, settings map[string]string) (string, llm.AuthScheme) {
	if key := settings[prefix+"_API_KEY"]; key != "" {
		return key, llm.AuthSchemeAPIKey
	}
	if key := settings[prefix+"_AUTH_TOKEN"]; key != "" {
		return key, llm.AuthSchemeBearer
	}
	if key := os.Getenv(prefix + "_API_KEY"); key != "" {
		return key, llm.AuthSchemeAPIKey
	}
	if key := os.Getenv(prefix + "_AUTH_TOKEN"); key != "" {
		return key, llm.AuthSchemeBearer
	}
	return "", llm.AuthSchemeAPIKey
}

// isModel reports whether model's name starts with prefix, case-insensitively.
// Vendors and users mix case (e.g. "GPT-5.5"), and the APIs treat model ids as
// case-equivalent.
func isModel(model, prefix string) bool {
	return len(model) >= len(prefix) && strings.EqualFold(model[:len(prefix)], prefix)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
