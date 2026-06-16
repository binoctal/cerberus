package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
)

// CompleteWithVision sends a vision request with images to Gemini API
func (c *GeminiClient) CompleteWithVision(ctx context.Context, prompt string, images [][]byte) (*Response, error) {
	parts := []map[string]any{{"text": prompt}}
	for _, img := range images {
		parts = append(parts, map[string]any{
			"inline_data": map[string]any{
				"mime_type": "image/png",
				"data":      base64.StdEncoding.EncodeToString(img),
			},
		})
	}

	body := map[string]any{
		"contents": []map[string]any{{
			"role":  "user",
			"parts": parts,
		}},
	}
	b, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL(), bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", c.apiKey)

	resp, err := c.client().Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		UsageMetadata struct {
			TotalTokenCount int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)

	content := ""
	if len(result.Candidates) > 0 && len(result.Candidates[0].Content.Parts) > 0 {
		content = result.Candidates[0].Content.Parts[0].Text
	}
	return &Response{
		Content: content,
		Usage:   TokenUsage{TotalTokens: result.UsageMetadata.TotalTokenCount},
	}, nil
}
