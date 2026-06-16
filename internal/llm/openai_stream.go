package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Stream sends a streaming request to OpenAI API
func (c *OpenAIClient) Stream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	if req.Model == "" {
		req.Model = c.model
	}

	body := map[string]any{
		"model":       req.Model,
		"messages":    req.Messages,
		"max_tokens":  max(req.MaxTokens, 4096),
		"temperature": 0.1,
		"stream":      true,
	}
	b, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL(), bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai stream: %w", err)
	}

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("openai stream error %d: %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan StreamEvent, 64)
	go func() {
		defer close(ch)
		defer func() { _ = resp.Body.Close() }()

		scanner := newSSEScanner(resp.Body)
		for scanner.Next() {
			_, data := scanner.Event()
			if data == "" || data == "[DONE]" {
				ch <- StreamEvent{Type: StreamDone}
				return
			}

			var evt struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
					FinishReason *string `json:"finish_reason"`
				} `json:"choices"`
				Usage *struct {
					PromptTokens     int `json:"prompt_tokens"`
					CompletionTokens int `json:"completion_tokens"`
					TotalTokens      int `json:"total_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				continue
			}

			if len(evt.Choices) > 0 {
				content := evt.Choices[0].Delta.Content
				if content != "" {
					ch <- StreamEvent{Type: StreamDelta, Content: content}
				}
				if evt.Choices[0].FinishReason != nil {
					usage := &TokenUsage{}
					if evt.Usage != nil {
						usage = &TokenUsage{
							InputTokens:  evt.Usage.PromptTokens,
							OutputTokens: evt.Usage.CompletionTokens,
							TotalTokens:  evt.Usage.TotalTokens,
						}
					}
					ch <- StreamEvent{Type: StreamDone, Usage: usage}
					return
				}
			}
		}

		ch <- StreamEvent{Type: StreamDone}
	}()

	return ch, nil
}
