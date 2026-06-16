package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Stream sends a streaming request to Claude API
func (c *ClaudeClient) Stream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	if req.Model == "" {
		req.Model = c.model
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

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL(), bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.client().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call anthropic stream: %w", err)
	}

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("anthropic stream error %d: %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan StreamEvent, 64)
	go func() {
		defer close(ch)
		defer func() { _ = resp.Body.Close() }()

		scanner := newSSEScanner(resp.Body)
		for scanner.Next() {
			eventType, data := scanner.Event()
			if eventType == "event" {
				continue // skip event type lines
			}

			// Parse the SSE data field.
			if data == "" || data == "[DONE]" {
				continue
			}

			var evt struct {
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
			}
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				continue
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
				// Final usage info.
			case "message_stop":
				usage := &TokenUsage{
					InputTokens:  evt.Message.Usage.InputTokens,
					OutputTokens: evt.Message.Usage.OutputTokens,
					TotalTokens:  evt.Message.Usage.InputTokens + evt.Message.Usage.OutputTokens,
				}
				ch <- StreamEvent{Type: StreamDone, Usage: usage}
				return
			}
		}

		// If we get here without message_stop, still send done.
		ch <- StreamEvent{Type: StreamDone}
	}()

	return ch, nil
}
