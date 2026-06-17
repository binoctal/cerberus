package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/binoctal/cerberus/internal/llm"
)

// streamCollector collects streaming response into content and usage
type streamCollector struct {
	content strings.Builder
	usage   llm.TokenUsage
}

// collectStreamEvents processes stream events and collects content
func collectStreamEvents(ctx context.Context, client llm.Client, prompt string) (*streamCollector, error) {
	events, err := client.Stream(ctx, llm.Request{
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("stream: %w", err)
	}

	collector := &streamCollector{}
	for evt := range events {
		switch evt.Type {
		case llm.StreamDelta:
			collector.content.WriteString(evt.Content)
			if evt.Usage != nil {
				collector.usage = *evt.Usage
			}
		case llm.StreamDone:
			if evt.Usage != nil {
				collector.usage = *evt.Usage
			}
		case llm.StreamError:
			return nil, fmt.Errorf("stream error: %w", evt.Err)
		}
	}

	return collector, nil
}

// recordTokenUsage records the token usage from streaming
func recordTokenUsage(budget *TokenBudget, prompt string, collector *streamCollector) {
	if collector.usage.TotalTokens > 0 {
		budget.Record(collector.usage.TotalTokens)
	} else {
		// Estimate if provider didn't report usage
		budget.Record(len(prompt)/4 + collector.content.Len()/4)
	}
}

// parseAndValidate parses structured output and validates it
func parseAndValidate(fullContent string, schema any) error {
	if err := ParseStructuredOutput(fullContent, schema); err != nil {
		return fmt.Errorf("parse output: %w\nraw: %s", err, fullContent)
	}
	return nil
}
