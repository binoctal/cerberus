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

type OpenAIClient struct {
	apiKey     string
	model      string
	httpClient *http.Client // nil defaults to http.DefaultClient
	serverURL  string       // overrides baseURL if set (for testing)
}

func NewOpenAIClient(apiKey, model string) *OpenAIClient {
	return &OpenAIClient{apiKey: apiKey, model: model}
}

func (c *OpenAIClient) baseURL() string {
	if c.serverURL != "" {
		return c.serverURL
	}
	return "https://api.openai.com/v1/chat/completions"
}

func (c *OpenAIClient) client() *http.Client {
	if c.httpClient != nil {
		return c.httpClient
	}
	return http.DefaultClient
}

func (c *OpenAIClient) Complete(ctx context.Context, req Request) (*Response, error) {
	if req.Model == "" {
		req.Model = c.model
	}

	body := map[string]any{
		"model":       req.Model,
		"messages":    req.Messages,
		"max_tokens":  max(req.MaxTokens, 4096),
		"temperature": 0.1,
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
		return nil, fmt.Errorf("call openai: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai api error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)

	var content string
	if len(result.Choices) > 0 {
		content = result.Choices[0].Message.Content
	}
	return &Response{
		Content:    content,
		StopReason: func() string { if len(result.Choices) > 0 { return result.Choices[0].FinishReason }; return "" }(),
		Usage: TokenUsage{
			InputTokens:  result.Usage.PromptTokens,
			OutputTokens: result.Usage.CompletionTokens,
			TotalTokens:  result.Usage.TotalTokens,
		},
	}, nil
}

func (c *OpenAIClient) CompleteWithVision(ctx context.Context, prompt string, images [][]byte) (*Response, error) {
	content := []map[string]any{
		{"type": "text", "text": prompt},
	}
	for _, img := range images {
		content = append(content, map[string]any{
			"type": "image_url",
			"image_url": map[string]any{
				"url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(img),
			},
		})
	}

	body := map[string]any{
		"model": c.model,
		"messages": []map[string]any{{
			"role":    "user",
			"content": content,
		}},
		"max_tokens": 4096,
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
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)

	content2 := ""
	if len(result.Choices) > 0 {
		content2 = result.Choices[0].Message.Content
	}
	return &Response{Content: content2, Usage: TokenUsage{TotalTokens: result.Usage.TotalTokens}}, nil
}
