package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/binoctal/cerberus/internal/llm"
)

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
