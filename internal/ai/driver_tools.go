package ai

import (
	"context"
	"fmt"

	"github.com/binoctal/cerberus/internal/llm"
)

// DecideWithTools sends a prompt with tool definitions and returns any tool calls
// made by the LLM. If no tools are called, returns the text content.
//
// Like Decide, the LLM call is wrapped in executeWithRetry so transient errors
// (429/5xx, rate-limit, timeout) are retried with exponential backoff. This
// parity matters because every Scout/Agent site migrated to tool-calling in
// S2/S3 funnels through this method — without retry, the first rate-limit
// derails the run. Caching is intentionally NOT applied here: tool responses
// are request-shape dependent (tools array, prompt) and a tool-call-aware
// cache path is deferred (spec §3).
func (d *Driver) DecideWithTools(ctx context.Context, prompt string, tools []llm.Tool) (*ToolCallResult, error) {
	// Check budget
	if err := checkBudget(d.budget); err != nil {
		return nil, err
	}

	// Execute with retry — executeWithRetry only returns tokens, so capture
	// the response via a closure variable (mirrors Decide's structure).
	var resp *llm.Response
	tokens, err := executeWithRetry(ctx, d.retry.MaxRetries, d.retry.BaseDelay, d.retry.MaxDelay,
		func(ctx context.Context) (int, error) {
			r, err := d.client.Complete(ctx, llm.Request{
				Messages: []llm.Message{
					{Role: "user", Content: prompt},
				},
				Tools: tools,
			})
			if err != nil {
				return 0, fmt.Errorf("llm call with tools: %w", err)
			}
			resp = r
			return r.Usage.TotalTokens, nil
		})
	if err != nil {
		return nil, err
	}

	d.budget.Record(tokens)

	return &ToolCallResult{
		ToolCalls: resp.ToolCalls,
		Content:   resp.Content,
		Usage:     TokenUsage{TotalTokens: tokens},
	}, nil
}
