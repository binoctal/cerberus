package llm

import (
	"context"
)

// Complete sends a synchronous request to Claude API
func (c *ClaudeClient) Complete(ctx context.Context, req Request) (*Response, error) {
	// Phase 1: Prepare HTTP request
	httpReq, err := prepareCompleteRequest(ctx, req, c)
	if err != nil {
		return nil, err
	}

	// Phase 2: Send request and validate response
	resp, err := sendCompleteRequest(httpReq, c)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	// Phase 3: Parse response
	result, err := parseCompleteResponse(resp)
	if err != nil {
		return nil, err
	}

	// Phase 4: Build result
	response := buildCompleteResult(result)
	return &response, nil
}
