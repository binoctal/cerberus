package llm

import (
	"fmt"
	"strings"
)

// NewClient creates an LLM client with model and API key (shorthand).
//
// This is a convenience wrapper around NewClientWithConfig for the common case
// where you only need to specify the model and API key.
func NewClient(model, apiKey string) (Client, error) {
	return NewClientWithConfig(ClientConfig{Model: model, APIKey: apiKey})
}

// NewClientWithConfig creates an LLM client with full configuration.
//
// Provider is determined by the Provider field if set; otherwise it's auto-detected
// from the model name. Supported providers: "anthropic", "openai", "gemini", "mock".
// BaseURL can override the provider's default API endpoint (for deep integrations).
func NewClientWithConfig(cfg ClientConfig) (Client, error) {
	provider := cfg.Provider
	if provider == "" {
		provider = detectProvider(cfg.Model)
	}
	// Normalize model: many Anthropic-compatible endpoints (e.g. zhipu/bigmodel)
	// are case-sensitive and reject mixed-case names like "GLM-4.7". Strip the
	// Claude Code "[Nm]" thinking-budget suffix which is not part of the model id.
	cfg.Model = normalizeModelID(cfg.Model)
	switch provider {
	case "anthropic":
		return NewClaudeClient(cfg.APIKey, cfg.Model, cfg.BaseURL), nil
	case "openai":
		return NewOpenAIClient(cfg.APIKey, cfg.Model, cfg.BaseURL), nil
	case "gemini":
		return NewGeminiClient(cfg.APIKey, cfg.Model, cfg.BaseURL), nil
	case "mock":
		return NewMockClient(map[string]string{
			"default": `{"status":"pass","confidence":0.9,"reasoning":"mock response"}`,
		}), nil
	default:
		return nil, fmt.Errorf("unsupported model: %s", cfg.Model)
	}
}

// detectProvider determines the LLM provider from a model name.
//
// Returns "openai" for models starting with "gpt", "gemini" for Gemini models,
// "mock" for the literal string "mock", and defaults to "anthropic" for all other
// cases (since cerberus deep-integrates with Claude Code).
func detectProvider(model string) string {
	if model == "mock" {
		return "mock"
	}
	// Match case-insensitively: vendors/users mix case ("GPT-5.5", "Gemini-3.5").
	m := strings.ToLower(model)
	if strings.HasPrefix(m, "gpt") {
		return "openai"
	}
	if strings.HasPrefix(m, "gemini") {
		return "gemini"
	}
	// Default to anthropic: cerberus deep-integrates with Claude Code, so
	// Anthropic-compatible endpoints (api.anthropic.com, GLM, etc.) are the norm.
	return "anthropic"
}
