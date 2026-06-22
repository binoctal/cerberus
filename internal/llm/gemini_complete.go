package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Complete sends a non-streaming request to Gemini API
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
	if len(req.Tools) > 0 {
		funcDecls := make([]map[string]any, len(req.Tools))
		for i, t := range req.Tools {
			funcDecls[i] = map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.InputSchema,
			}
		}
		body["tools"] = []map[string]any{
			{"functionDeclarations": funcDecls},
		}
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
					Text         string `json:"text"`
					FunctionCall *struct {
						Name string          `json:"name"`
						Args json.RawMessage `json:"args"`
					} `json:"functionCall"`
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
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode gemini response: %w", err)
	}

	var content string
	var toolCalls []ToolCall
	if len(result.Candidates) > 0 {
		for _, part := range result.Candidates[0].Content.Parts {
			if part.Text != "" {
				content += part.Text
			}
			if part.FunctionCall != nil {
				var input map[string]any
				_ = json.Unmarshal(part.FunctionCall.Args, &input)
				toolCalls = append(toolCalls, ToolCall{
					ID:    part.FunctionCall.Name,
					Name:  part.FunctionCall.Name,
					Input: input,
				})
			}
		}
	}
	return &Response{
		Content: content,
		StopReason: func() string {
			if len(result.Candidates) > 0 {
				return result.Candidates[0].FinishReason
			}
			return ""
		}(),
		ToolCalls: toolCalls,
		Usage: TokenUsage{
			InputTokens:  result.UsageMetadata.PromptTokenCount,
			OutputTokens: result.UsageMetadata.CandidatesTokenCount,
			TotalTokens:  result.UsageMetadata.TotalTokenCount,
		},
	}, nil
}
