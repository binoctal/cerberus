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

type GeminiClient struct {
	apiKey     string
	model      string
	httpClient *http.Client // nil defaults to http.DefaultClient
	serverURL  string       // overrides baseURL if set (for testing)
}

func NewGeminiClient(apiKey, model, baseURL string) *GeminiClient {
	return &GeminiClient{apiKey: apiKey, model: model, serverURL: baseURL}
}

func (c *GeminiClient) baseURL() string {
	if c.serverURL != "" {
		return c.serverURL
	}
	return fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", c.model)
}

func (c *GeminiClient) client() *http.Client {
	if c.httpClient != nil {
		return c.httpClient
	}
	return http.DefaultClient
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
	httpReq.Header.Set("x-goog-api-key", c.apiKey)

	resp, err := c.client().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call gemini: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

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
	_ = json.NewDecoder(resp.Body).Decode(&result)

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
	defer func() { _ = resp.Body.Close() }()

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

func (c *GeminiClient) streamURL() string {
	if c.serverURL != "" {
		return c.serverURL
	}
	return fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:streamGenerateContent?alt=sse", c.model)
}

func (c *GeminiClient) Stream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
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

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.streamURL(), bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", c.apiKey)

	resp, err := c.client().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini stream: %w", err)
	}

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("gemini stream error %d: %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan StreamEvent, 64)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := newSSEScanner(resp.Body)
		for scanner.Next() {
			_, data := scanner.Event()
			if data == "" {
				continue
			}

			var evt struct {
				Candidates []struct {
					Content struct {
						Parts []struct {
							Text string `json:"text"`
						} `json:"parts"`
					} `json:"content"`
					FinishReason string `json:"finishReason"`
				} `json:"candidates"`
				UsageMetadata *struct {
					PromptTokenCount     int `json:"promptTokenCount"`
					CandidatesTokenCount int `json:"candidatesTokenCount"`
					TotalTokenCount      int `json:"totalTokenCount"`
				} `json:"usageMetadata"`
			}
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				continue
			}

			if len(evt.Candidates) > 0 {
				cand := evt.Candidates[0]
				if len(cand.Content.Parts) > 0 && cand.Content.Parts[0].Text != "" {
					ch <- StreamEvent{Type: StreamDelta, Content: cand.Content.Parts[0].Text}
				}
				if cand.FinishReason == "STOP" {
					usage := &TokenUsage{}
					if evt.UsageMetadata != nil {
						usage = &TokenUsage{
							InputTokens:  evt.UsageMetadata.PromptTokenCount,
							OutputTokens: evt.UsageMetadata.CandidatesTokenCount,
							TotalTokens:  evt.UsageMetadata.TotalTokenCount,
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
