package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type GeminiClient struct {
	apiKey string
	model  string
}

func NewGeminiClient(apiKey, model string) *GeminiClient {
	return &GeminiClient{apiKey: apiKey, model: model}
}

func (c *GeminiClient) baseURL() string {
	return fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		c.model, c.apiKey)
}

func (c *GeminiClient) Complete(ctx context.Context, req Request) (*Response, error) {
	msgs := make([]map[string]any, len(req.Messages))
	for i, m := range req.Messages {
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		msgs[i] = map[string]any{
			"role": role,
			"parts": []map[string]any{
				{"text": m.Content},
			},
		}
	}

	body := map[string]any{
		"contents": msgs,
		"generationConfig": map[string]any{
			"temperature":     1.0,
			"maxOutputTokens": max(req.MaxTokens, 4096),
		},
	}
	b, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL(), bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call gemini: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini api error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
			TotalTokenCount      int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	var content string
	if len(result.Candidates) > 0 && len(result.Candidates[0].Content.Parts) > 0 {
		content = result.Candidates[0].Content.Parts[0].Text
	}
	return &Response{
		Content:    content,
		StopReason: func() string { if len(result.Candidates) > 0 { return result.Candidates[0].FinishReason }; return "" }(),
		Usage: TokenUsage{
			InputTokens:  result.UsageMetadata.PromptTokenCount,
			OutputTokens: result.UsageMetadata.CandidatesTokenCount,
			TotalTokens:  result.UsageMetadata.TotalTokenCount,
		},
	}, nil
}

func (c *GeminiClient) CompleteWithVision(ctx context.Context, prompt string, images [][]byte) (*Response, error) {
	parts := []map[string]any{{"text": prompt}}
	for _, img := range images {
		parts = append(parts, map[string]any{
			"inline_data": map[string]any{
				"mime_type": "image/png",
				"data":      fmt.Sprintf("%x", img),
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

	resp, err := http.DefaultClient.Do(httpReq)
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
	json.NewDecoder(resp.Body).Decode(&result)

	content := ""
	if len(result.Candidates) > 0 && len(result.Candidates[0].Content.Parts) > 0 {
		content = result.Candidates[0].Content.Parts[0].Text
	}
	return &Response{
		Content: content,
		Usage:   TokenUsage{TotalTokens: result.UsageMetadata.TotalTokenCount},
	}, nil
}
