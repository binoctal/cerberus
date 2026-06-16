package ai

import (
	"context"
	"fmt"
)

// DecideWithVision sends a prompt with images to the LLM and parses the response.
// Vision calls are not cached due to image payload variability.
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
