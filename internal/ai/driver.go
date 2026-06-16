package ai

import (
	"context"
	"fmt"
	"strings"
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

type Driver struct {
	client llm.Client
	budget *TokenBudget
	retry  RetryConfig
	cache  *ResponseCache
}

func NewDriver(client llm.Client, budget *TokenBudget) *Driver {
	return &Driver{
		client: client,
		budget: budget,
		retry:  DefaultRetryConfig(),
		cache:  NewResponseCache(5 * time.Minute),
	}
}

// NewDriverWithRetry creates a driver with custom retry config.
func NewDriverWithRetry(client llm.Client, budget *TokenBudget, retry RetryConfig) *Driver {
	return &Driver{
		client: client,
		budget: budget,
		retry:  retry,
		cache:  NewResponseCache(5 * time.Minute),
	}
}

// SetCache replaces the default cache. Pass nil to disable caching.
func (d *Driver) SetCache(c *ResponseCache) {
	d.cache = c
}

func (d *Driver) Decide(ctx context.Context, prompt string, schema any) error {
	// Check budget
	if err := checkBudget(d.budget); err != nil {
		return err
	}

	// Try cache
	if _, ok, err := tryGetCachedResponse(d.cache, prompt, schema); ok {
		return err
	}

	// Execute with retry
	tokens, err := executeWithRetry(ctx, d.retry.MaxRetries, d.retry.BaseDelay, d.retry.MaxDelay,
		func(ctx context.Context) (int, error) {
			resp, err := d.client.Complete(ctx, llm.Request{
				Messages: []llm.Message{
					{Role: "user", Content: prompt},
				},
			})
			if err != nil {
				return 0, fmt.Errorf("llm call: %w", err)
			}

			// Parse response
			if err := ParseStructuredOutput(resp.Content, schema); err != nil {
				return 0, fmt.Errorf("parse output: %w\nraw: %s", err, resp.Content)
			}

			tokens := resp.Usage.TotalTokens
			// Cache successful response
			cacheResponse(d.cache, prompt, resp.Content, tokens)
			return tokens, nil
		})

	if err != nil {
		return err
	}

	d.budget.Record(tokens)
	return nil
}

func (d *Driver) DecideWithVision(ctx context.Context, prompt string, images [][]byte, schema any) error {
	// Check budget
	if err := checkBudget(d.budget); err != nil {
		return err
	}

	// Execute with retry (no caching for vision calls)
	tokens, err := executeWithRetry(ctx, d.retry.MaxRetries, d.retry.BaseDelay, d.retry.MaxDelay,
		func(ctx context.Context) (int, error) {
			resp, err := d.client.CompleteWithVision(ctx, prompt, images)
			if err != nil {
				return 0, fmt.Errorf("llm vision call: %w", err)
			}

			// Parse response
			if err := ParseStructuredOutput(resp.Content, schema); err != nil {
				return 0, fmt.Errorf("parse output: %w\nraw: %s", err, resp.Content)
			}

			return resp.Usage.TotalTokens, nil
		})

	if err != nil {
		return err
	}

	d.budget.Record(tokens)
	return nil
}

func (d *Driver) Budget() *TokenBudget {
	return d.budget
}

// Client returns the underlying LLM client for direct access (e.g., raw completion fallback).
func (d *Driver) Client() llm.Client {
	return d.client
}

// isRetryable determines if an LLM error is transient and worth retrying.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()

	// 5xx server errors.
	if containsAny(msg, "500", "502", "503", "504", "server error", "internal server error") {
		return true
	}

	// Rate limiting.
	if containsAny(msg, "429", "rate limit", "rate_limit", "too many requests") {
		return true
	}

	// Network / timeout errors.
	if containsAny(msg, "timeout", "connection refused", "connection reset", "temporary failure", "deadline exceeded") {
		return true
	}

	// API overloaded.
	if containsAny(msg, "overloaded", "capacity") {
		return true
	}

	return false
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if len(s) >= len(sub) {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

// DecideStreamCollect uses streaming to collect the full response, then
// parses it into the schema. Useful for progress feedback via the callback.
func (d *Driver) DecideStreamCollect(ctx context.Context, prompt string, schema any) error {
	// Check budget
	if err := checkBudget(d.budget); err != nil {
		return err
	}

	// Try cache
	if _, ok, err := tryGetCachedResponse(d.cache, prompt, schema); ok {
		return err
	}

	// Stream response
	events, err := d.client.Stream(ctx, llm.Request{
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return fmt.Errorf("stream: %w", err)
	}

	var content strings.Builder
	var usage llm.TokenUsage

	for evt := range events {
		switch evt.Type {
		case llm.StreamDelta:
			content.WriteString(evt.Content)
			if evt.Usage != nil {
				usage = *evt.Usage
			}
		case llm.StreamDone:
			if evt.Usage != nil {
				usage = *evt.Usage
			}
		case llm.StreamError:
			return fmt.Errorf("stream error: %w", evt.Err)
		}
	}

	// Record token usage
	if usage.TotalTokens > 0 {
		d.budget.Record(usage.TotalTokens)
	} else {
		// Estimate if provider didn't report usage
		d.budget.Record(len(prompt)/4 + content.Len()/4)
	}

	fullContent := content.String()
	if err := ParseStructuredOutput(fullContent, schema); err != nil {
		return fmt.Errorf("parse output: %w\nraw: %s", err, fullContent)
	}

	// Cache response
	cacheResponse(d.cache, prompt, fullContent, usage.TotalTokens)

	return nil
}

// ToolCallResult holds the result of a DecideWithTools call.
type ToolCallResult struct {
	ToolCalls []llm.ToolCall
	Content   string
	Usage     TokenUsage
}

// DecideWithTools sends a prompt with tool definitions and returns any tool calls
// made by the LLM. If no tools are called, returns the text content.
func (d *Driver) DecideWithTools(ctx context.Context, prompt string, tools []llm.Tool) (*ToolCallResult, error) {
	// Check budget
	if err := checkBudget(d.budget); err != nil {
		return nil, err
	}

	resp, err := d.client.Complete(ctx, llm.Request{
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
		Tools: tools,
	})
	if err != nil {
		return nil, fmt.Errorf("llm call with tools: %w", err)
	}

	d.budget.Record(resp.Usage.TotalTokens)

	return &ToolCallResult{
		ToolCalls: resp.ToolCalls,
		Content:   resp.Content,
		Usage:     TokenUsage{TotalTokens: resp.Usage.TotalTokens},
	}, nil
}
