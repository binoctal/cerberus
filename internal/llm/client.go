package llm

import "context"

// Client defines the interface for LLM provider clients.
//
// Each provider implementation (Claude, OpenAI, Gemini, mock) must implement
// these three methods, exposing a uniform API for completing requests,
// handling vision inputs, and streaming responses.
type Client interface {
	// Complete sends a request to the LLM and returns the complete response.
	Complete(ctx context.Context, req Request) (*Response, error)

	// CompleteWithVision sends a text prompt with image data to vision-capable models.
	CompleteWithVision(ctx context.Context, prompt string, images [][]byte) (*Response, error)

	// Stream sends a request and returns a channel of streaming events.
	Stream(ctx context.Context, req Request) (<-chan StreamEvent, error)
}
