package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// prepareCompleteRequest prepares the HTTP request for Claude Complete API
func prepareCompleteRequest(ctx context.Context, req Request, client *ClaudeClient) (*http.Request, error) {
	if req.Model == "" {
		req.Model = client.model
	}

	body := map[string]any{
		"model":      req.Model,
		"max_tokens": max(req.MaxTokens, 4096),
		"messages":   req.Messages,
	}

	if len(req.Tools) > 0 {
		claudeTools := make([]map[string]any, len(req.Tools))
		for i, t := range req.Tools {
			claudeTools[i] = map[string]any{
				"name":         t.Name,
				"description":  t.Description,
				"input_schema": t.InputSchema,
			}
		}
		body["tools"] = claudeTools
	}

	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", client.baseURL(), bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	client.setAuthHeader(httpReq.Header)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	return httpReq, nil
}

// sendCompleteRequest sends the HTTP request and validates response
func sendCompleteRequest(httpReq *http.Request, client *ClaudeClient) (*http.Response, error) {
	resp, err := client.client().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call anthropic: %w", err)
	}

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("anthropic api error %d: %s", resp.StatusCode, string(respBody))
	}

	return resp, nil
}

// completeResponse represents the parsed Claude Complete API response
type completeResponse struct {
	Content []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	StopReason string `json:"stop_reason"`
}

// parseCompleteResponse parses the Complete API response
func parseCompleteResponse(resp *http.Response) (*completeResponse, error) {
	var result completeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// buildCompleteResult creates Response from completeResponse
func buildCompleteResult(result *completeResponse) Response {
	var content string
	var toolCalls []ToolCall

	for _, block := range result.Content {
		switch block.Type {
		case "text", "":
			content += block.Text
		case "tool_use":
			var input map[string]any
			_ = json.Unmarshal(block.Input, &input)
			toolCalls = append(toolCalls, ToolCall{
				ID:    block.ID,
				Name:  block.Name,
				Input: input,
			})
		}
	}

	return Response{
		Content:    content,
		StopReason: result.StopReason,
		ToolCalls:  toolCalls,
		Usage: TokenUsage{
			InputTokens:  result.Usage.InputTokens,
			OutputTokens: result.Usage.OutputTokens,
			TotalTokens:  result.Usage.InputTokens + result.Usage.OutputTokens,
		},
	}
}
