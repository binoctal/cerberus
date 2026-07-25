package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Complete sends a non-streaming request to OpenAI API
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
	if len(req.Tools) > 0 {
		oaiTools := make([]map[string]any, len(req.Tools))
		for i, t := range req.Tools {
			oaiTools[i] = map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        t.Name,
					"description": t.Description,
					"parameters":  t.InputSchema,
				},
			}
		}
		body["tools"] = oaiTools
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
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode openai response: %w", err)
	}

	var content string
	var toolCalls []ToolCall
	if len(result.Choices) > 0 {
		content = result.Choices[0].Message.Content
		for _, tc := range result.Choices[0].Message.ToolCalls {
			input := decodeToolArguments(tc.Function.Arguments)
			toolCalls = append(toolCalls, ToolCall{
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: input,
			})
		}
	}
	return &Response{
		Content: content,
		StopReason: func() string {
			if len(result.Choices) > 0 {
				return result.Choices[0].FinishReason
			}
			return ""
		}(),
		ToolCalls: toolCalls,
		Usage: TokenUsage{
			InputTokens:  result.Usage.PromptTokens,
			OutputTokens: result.Usage.CompletionTokens,
			TotalTokens:  result.Usage.TotalTokens,
		},
	}, nil
}

// decodeToolArguments decodes a tool-call arguments payload into a map. OpenAI
// (and OpenAI-compatible APIs) send arguments as a JSON-encoded string — a
// stringified object such as `"{\"location\":\"Tokyo\"}"` — which a direct
// json.Unmarshal into a map fails on (the payload is a JSON string, not an
// object). Handle both shapes: decode directly when the payload is a raw
// object; otherwise unwrap the string and decode its contents.
func decodeToolArguments(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var input map[string]any
	if err := json.Unmarshal(raw, &input); err == nil {
		return input
	}
	// String-wrapped JSON (OpenAI's real shape): unwrap, then decode the inner JSON.
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil
	}
	var inner map[string]any
	_ = json.Unmarshal([]byte(s), &inner)
	return inner
}
