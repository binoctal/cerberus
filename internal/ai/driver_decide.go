package ai

import (
	"context"
	"fmt"

	"github.com/binoctal/cerberus/internal/llm"
)

// Decide sends a prompt to the LLM and parses the response into the schema.
// It handles caching, budget checking, and retry logic automatically.
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
