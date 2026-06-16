package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
)

// CompleteWithVision sends a vision request to Claude API with images
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
