package ai

import (
	"time"

	"github.com/binoctal/cerberus/internal/llm"
)

// RetryConfig controls LLM call retry behavior.
type RetryConfig struct {
	MaxRetries int           // Maximum retries for transient errors (default: 3)
	BaseDelay  time.Duration // Initial backoff delay (default: 1s)
	MaxDelay   time.Duration // Maximum backoff delay (default: 10s)
}

// DefaultRetryConfig returns sensible defaults.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries: 3,
		BaseDelay:  1 * time.Second,
		MaxDelay:   10 * time.Second,
	}
}

// Driver wraps an LLM client with budget tracking, retry logic, and caching.
type Driver struct {
	client llm.Client
	budget *TokenBudget
	retry  RetryConfig
	cache  *ResponseCache
}

// ToolCallResult holds the result of a DecideWithTools call.
type ToolCallResult struct {
	ToolCalls []llm.ToolCall
	Content   string
	Usage     TokenUsage
}
