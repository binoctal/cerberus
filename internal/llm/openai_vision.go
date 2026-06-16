package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
)

// CompleteWithVision sends a vision request with images to OpenAI API
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
	defer resp.Body.Close()

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
