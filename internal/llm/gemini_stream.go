package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Stream sends a streaming request to Gemini API
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
		_ = resp.Body.Close()
		return nil, fmt.Errorf("gemini stream error %d: %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan StreamEvent, 64)
	go func() {
		defer close(ch)
		defer func() { _ = resp.Body.Close() }()

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
