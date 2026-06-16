package ai

import (
	"context"
	"fmt"

	"github.com/binoctal/cerberus/internal/llm"
)

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
