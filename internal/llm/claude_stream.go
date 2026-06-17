package llm

import (
	"context"
)

// Stream sends a streaming request to Claude API
func (c *ClaudeClient) Stream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	// Phase 1: Prepare HTTP request
	httpReq, err := prepareStreamRequest(ctx, req, c)
	if err != nil {
		return nil, err
	}

	// Phase 2: Send request and validate response
	resp, err := sendStreamRequest(httpReq, c)
	if err != nil {
		return nil, err
	}

	// Phase 3: Start SSE stream parser
	ch := make(chan StreamEvent, 64)
	go parseSSEStream(resp.Body, ch)

	return ch, nil
}
