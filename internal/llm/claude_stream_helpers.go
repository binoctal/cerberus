package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// prepareStreamRequest builds the HTTP request for streaming.
func prepareStreamRequest(ctx context.Context, req Request, client *ClaudeClient) (*http.Request, error) {
	if req.Model == "" {
		req.Model = client.model
	}

	body := map[string]any{
		"model":      req.Model,
		"max_tokens": max(req.MaxTokens, 4096),
		"messages":   req.Messages,
		"stream":     true,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", client.baseURL(), bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	client.setAuthHeader(httpReq.Header)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	return httpReq, nil
}

// sendStreamRequest sends the HTTP request and validates response.
func sendStreamRequest(httpReq *http.Request, client *ClaudeClient) (*http.Response, error) {
	resp, err := client.client().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call anthropic stream: %w", err)
	}

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("anthropic stream error %d: %s", resp.StatusCode, string(respBody))
	}

	return resp, nil
}

// streamResponse represents a parsed SSE response event
type streamResponse struct {
	Type  string `json:"type"`
	Delta struct {
		Text string `json:"text"`
	} `json:"delta"`
	Message struct {
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
	// Usage is the top-level usage object carried by message_delta events
	// (output_tokens). message_start/message_stop do not populate this.
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// handleStreamEvent processes a single SSE event and sends to channel if needed.
// Returns true if streaming should stop (message_stop received).
func handleStreamEvent(data string, ch chan<- StreamEvent) bool {
	if data == "" || data == "[DONE]" {
		return false
	}

	var evt streamResponse
	if err := json.Unmarshal([]byte(data), &evt); err != nil {
		return false
	}

	switch evt.Type {
	case "content_block_delta":
		if evt.Delta.Text != "" {
			ch <- StreamEvent{Type: StreamDelta, Content: evt.Delta.Text}
		}
	case "message_start":
		// Initial message with input token usage.
		if evt.Message.Usage.InputTokens > 0 {
			ch <- StreamEvent{Type: StreamDelta, Usage: &TokenUsage{
				InputTokens: evt.Message.Usage.InputTokens,
				TotalTokens: evt.Message.Usage.InputTokens,
			}}
		}
	case "message_delta":
		// Real Claude reports accumulated output_tokens in a top-level "usage"
		// on this event (message.usage is empty here). Forward it so the
		// collector accumulates real usage instead of estimating.
		if evt.Usage.OutputTokens > 0 {
			ch <- StreamEvent{Type: StreamDelta, Usage: &TokenUsage{OutputTokens: evt.Usage.OutputTokens}}
		}
	case "message_stop":
		// message_stop carries no usage; final usage is assembled by the
		// collector from message_start (input) + message_delta (output).
		ch <- StreamEvent{Type: StreamDone}
		return true
	}

	return false
}

// parseSSEStream reads SSE events from response body and sends to channel.
func parseSSEStream(respBody io.ReadCloser, ch chan<- StreamEvent) {
	defer close(ch)
	defer func() { _ = respBody.Close() }()

	scanner := newSSEScanner(respBody)
	for scanner.Next() {
		eventType, data := scanner.Event()
		if eventType == "event" {
			continue // skip event type lines
		}

		if handleStreamEvent(data, ch) {
			return // message_stop received
		}
	}

	// If we get here without message_stop, still send done.
	ch <- StreamEvent{Type: StreamDone}
}
