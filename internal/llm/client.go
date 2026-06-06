package llm

import (
	"context"
	"fmt"
	"strings"
)

type Client interface {
	Complete(ctx context.Context, req Request) (*Response, error)
	CompleteWithVision(ctx context.Context, prompt string, images [][]byte) (*Response, error)
}

type Request struct {
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	MaxTokens int       `json:"max_tokens,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Response struct {
	Content    string     `json:"content"`
	Usage      TokenUsage `json:"usage"`
	StopReason string     `json:"stop_reason,omitempty"`
}

type TokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

func NewClient(model, apiKey string) (Client, error) {
	provider := detectProvider(model)
	switch provider {
	case "anthropic":
		return NewClaudeClient(apiKey, model), nil
	case "openai":
		return NewOpenAIClient(apiKey, model), nil
	case "gemini":
		return NewGeminiClient(apiKey, model), nil
	case "mock":
		return NewMockClient(map[string]string{
			"default": `{"status":"pass","confidence":0.9,"reasoning":"mock response"}`,
		}), nil
	default:
		return nil, fmt.Errorf("unsupported model: %s", model)
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
