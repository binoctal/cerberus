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
	StreamDelta  StreamEventType = "delta"  // Incremental content chunk
	StreamDone   StreamEventType = "done"   // Stream completed successfully
	StreamError  StreamEventType = "error"  // Stream encountered an error
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
	Model   string
	APIKey  string
	BaseURL string // optional: overrides the provider's default API URL
}

// NewClient creates an LLM client with model and API key (shorthand).
func NewClient(model, apiKey string) (Client, error) {
	return NewClientWithConfig(ClientConfig{Model: model, APIKey: apiKey})
}

// NewClientWithConfig creates an LLM client with full configuration.
func NewClientWithConfig(cfg ClientConfig) (Client, error) {
	provider := detectProvider(cfg.Model)
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

func detectProvider(model string) string {
	if model == "mock" {
		return "mock"
	}
	if strings.HasPrefix(model, "claude") {
		return "anthropic"
	}
	if strings.HasPrefix(model, "gpt") {
		return "openai"
	}
	if strings.HasPrefix(model, "gemini") {
		return "gemini"
	}
	return ""
}
