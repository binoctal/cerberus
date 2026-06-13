package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type ClaudeClient struct {
	apiKey     string
	model      string
	httpClient *http.Client // nil defaults to http.DefaultClient
	serverURL  string       // overrides baseURL if set (for testing)
}

func NewClaudeClient(apiKey, model, baseURL string) *ClaudeClient {
	return &ClaudeClient{apiKey: apiKey, model: model, serverURL: baseURL}
}

func (c *ClaudeClient) baseURL() string {
	if c.serverURL != "" {
		return c.serverURL
	}
	return "https://api.anthropic.com/v1/messages"
}

func (c *ClaudeClient) client() *http.Client {
	if c.httpClient != nil {
		return c.httpClient
	}
	return http.DefaultClient
}

func (c *ClaudeClient) Complete(ctx context.Context, req Request) (*Response, error) {
	if req.Model == "" {
		req.Model = c.model
	}

	body := map[string]any{
		"model":      req.Model,
		"max_tokens": max(req.MaxTokens, 4096),
		"messages":   req.Messages,
	}
	if len(req.Tools) > 0 {
		claudeTools := make([]map[string]any, len(req.Tools))
		for i, t := range req.Tools {
			claudeTools[i] = map[string]any{
				"name":         t.Name,
				"description":  t.Description,
				"input_schema": t.InputSchema,
			}
		}
		body["tools"] = claudeTools
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
		return nil, fmt.Errorf("call anthropic: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic api error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		StopReason string `json:"stop_reason"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	var content string
	var toolCalls []ToolCall
	for _, block := range result.Content {
		switch block.Type {
		case "text", "":
			content += block.Text
		case "tool_use":
			var input map[string]any
			_ = json.Unmarshal(block.Input, &input)
			toolCalls = append(toolCalls, ToolCall{
				ID:    block.ID,
				Name:  block.Name,
				Input: input,
			})
		}
	}

	return &Response{
		Content:    content,
		StopReason: result.StopReason,
		ToolCalls:  toolCalls,
		Usage: TokenUsage{
			InputTokens:  result.Usage.InputTokens,
			OutputTokens: result.Usage.OutputTokens,
			TotalTokens:  result.Usage.InputTokens + result.Usage.OutputTokens,
		},
	}, nil
}

func (c *ClaudeClient) CompleteWithVision(ctx context.Context, prompt string, images [][]byte) (*Response, error) {
	content := []map[string]any{
		{"type": "text", "text": prompt},
	}
	for _, img := range images {
		content = append(content, map[string]any{
			"type": "image",
			"source": map[string]any{
				"type":       "base64",
				"media_type": "image/png",
				"data":       base64.StdEncoding.EncodeToString(img),
			},
		})
	}

	body := map[string]any{
		"model":      c.model,
		"max_tokens": 4096,
		"messages":   []map[string]any{{"role": "user", "content": content}},
	}
	b, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL(), bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.client().Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		StopReason string `json:"stop_reason"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)

	var text string
	if len(result.Content) > 0 {
		text = result.Content[0].Text
	}
	return &Response{
		Content:    text,
		StopReason: result.StopReason,
		Usage: TokenUsage{
			InputTokens:  result.Usage.InputTokens,
			OutputTokens: result.Usage.OutputTokens,
			TotalTokens:  result.Usage.InputTokens + result.Usage.OutputTokens,
		},
	}, nil
}

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
