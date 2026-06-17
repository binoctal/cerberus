package ai

import (
	"context"
)

// DecideStreamCollect uses streaming to collect the full response, then
// parses it into the schema. Useful for progress feedback via the callback.
func (d *Driver) DecideStreamCollect(ctx context.Context, prompt string, schema any) error {
	// Phase 1: Check budget
	if err := checkBudget(d.budget); err != nil {
		return err
	}

	// Phase 2: Try cache
	if _, ok, err := tryGetCachedResponse(d.cache, prompt, schema); ok {
		return err
	}

	// Phase 3: Stream response and collect content
	collector, err := collectStreamEvents(ctx, d.client, prompt)
	if err != nil {
		return err
	}

	// Phase 4: Record token usage
	recordTokenUsage(d.budget, prompt, collector)

	// Phase 5: Parse structured output
	fullContent := collector.content.String()
	if err := parseAndValidate(fullContent, schema); err != nil {
		return err
	}

	// Phase 6: Cache response
	cacheResponse(d.cache, prompt, fullContent, collector.usage.TotalTokens)

	return nil
}
