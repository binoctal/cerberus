package ai

import (
	"context"
	"fmt"

	"github.com/binoctal/cerberus/internal/llm"
)

type Driver struct {
	client llm.Client
	budget *TokenBudget
}

func NewDriver(client llm.Client, budget *TokenBudget) *Driver {
	return &Driver{client: client, budget: budget}
}

func (d *Driver) Decide(ctx context.Context, prompt string, schema any) error {
	if d.budget.Exhausted() {
		return fmt.Errorf("token budget exhausted")
	}

	if !d.budget.CanSpend(d.budget.PerCallLimit) {
		return fmt.Errorf("insufficient budget: remaining %d, need up to %d",
			d.budget.Remaining(), d.budget.PerCallLimit)
	}

	resp, err := d.client.Complete(ctx, llm.Request{
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return fmt.Errorf("llm call: %w", err)
	}

	d.budget.Record(resp.Usage.TotalTokens)

	if err := ParseStructuredOutput(resp.Content, schema); err != nil {
		return fmt.Errorf("parse output: %w\nraw: %s", err, resp.Content)
	}

	return nil
}

func (d *Driver) DecideWithVision(ctx context.Context, prompt string, images [][]byte, schema any) error {
	if d.budget.Exhausted() {
		return fmt.Errorf("token budget exhausted")
	}

	if !d.budget.CanSpend(d.budget.PerCallLimit) {
		return fmt.Errorf("insufficient budget: remaining %d, need up to %d",
			d.budget.Remaining(), d.budget.PerCallLimit)
	}

	resp, err := d.client.CompleteWithVision(ctx, prompt, images)
	if err != nil {
		return fmt.Errorf("llm vision call: %w", err)
	}

	d.budget.Record(resp.Usage.TotalTokens)

	if err := ParseStructuredOutput(resp.Content, schema); err != nil {
		return fmt.Errorf("parse output: %w\nraw: %s", err, resp.Content)
	}

	return nil
}

func (d *Driver) Budget() *TokenBudget {
	return d.budget
}
