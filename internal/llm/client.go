package llm

import (
	"context"
	"fmt"
	"strings"
)

type Client interface {
	Complete(ctx context.Context, req Request) (*Response, error)
	CompleteWithVision(ctx context.Context, prompt string, images [][]byte) (*Response, error)
	Stream(ctx context.Context, req Request) (<-chan StreamEvent, error)
}

// StreamEventType identifies the type of a streaming event.
type StreamEventType string

const (
	StreamDelta StreamEventType = "delta" // Incremental content chunk
	StreamDone  StreamEventType = "done"  // Stream completed successfully
	StreamError StreamEventType = "error" // Stream encountered an error
)

// StreamEvent represents a single event from a streaming LLM response.
type StreamEvent struct {
	Type    StreamEventType
	Content string
	Usage   *TokenUsage
	Err     error
}

type Request struct {
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	MaxTokens int       `json:"max_tokens,omitempty"`
	Tools     []Tool    `json:"tools,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Response struct {
	Content    string     `json:"content"`
	Usage      TokenUsage `json:"usage"`
	StopReason string     `json:"stop_reason,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// Tool describes a function the LLM can call.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
}

// ToolCall represents a single function call requested by the LLM.
type ToolCall struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

type TokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// ClientConfig holds options for creating an LLM client.
type ClientConfig struct {
	Model    string
	APIKey   string
	BaseURL  string // optional: overrides the provider's default API URL
	Provider string // optional: "anthropic"|"openai"|"gemini"|"mock"; overrides model-based detection
}

// NewClient creates an LLM client with model and API key (shorthand).
func NewClient(model, apiKey string) (Client, error) {
	return NewClientWithConfig(ClientConfig{Model: model, APIKey: apiKey})
}

// NewClientWithConfig creates an LLM client with full configuration.
func NewClientWithConfig(cfg ClientConfig) (Client, error) {
	provider := cfg.Provider
	if provider == "" {
		provider = detectProvider(cfg.Model)
	}
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

// joinBaseURL appends path to base unless base already ends with path.
// Claude Code (and the Anthropic SDK) configure a base URL prefix such as
// ANTHROPIC_BASE_URL="https://host/api/anthropic"; the endpoint is that prefix
// plus "/v1/messages". Treating a bare prefix as the full endpoint silently
// breaks deep-integration (some providers return HTTP 200 with an error body).
func joinBaseURL(base, path string) string {
	if base == "" {
		return ""
	}
	trimmed := strings.TrimRight(base, "/")
	if strings.HasSuffix(trimmed, path) {
		return trimmed
	}
	return trimmed + path
}

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
