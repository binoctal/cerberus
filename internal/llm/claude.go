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
			Text string `json:"text"`
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
	if len(result.Content) > 0 {
		content = result.Content[0].Text
	}

	return &Response{
		Content:    content,
		StopReason: result.StopReason,
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

