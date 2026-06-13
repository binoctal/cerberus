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

func NewOpenAIClient(apiKey, model, baseURL string) *OpenAIClient {
	return &OpenAIClient{apiKey: apiKey, model: model, serverURL: baseURL}
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
					ID   string `json:"id"`
					Type string `json:"type"`
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
	_ = json.NewDecoder(resp.Body).Decode(&result)

	var content string
	var toolCalls []ToolCall
	if len(result.Choices) > 0 {
		content = result.Choices[0].Message.Content
		for _, tc := range result.Choices[0].Message.ToolCalls {
			var input map[string]any
			_ = json.Unmarshal(tc.Function.Arguments, &input)
			toolCalls = append(toolCalls, ToolCall{
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: input,
			})
		}
	}
	return &Response{
		Content:    content,
		StopReason: func() string { if len(result.Choices) > 0 { return result.Choices[0].FinishReason }; return "" }(),
		ToolCalls:  toolCalls,
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

func (c *OpenAIClient) Stream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	if req.Model == "" {
		req.Model = c.model
	}

	body := map[string]any{
		"model":       req.Model,
		"messages":    req.Messages,
		"max_tokens":  max(req.MaxTokens, 4096),
		"temperature": 0.1,
		"stream":      true,
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
		return nil, fmt.Errorf("openai stream: %w", err)
	}

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("openai stream error %d: %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan StreamEvent, 64)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := newSSEScanner(resp.Body)
		for scanner.Next() {
			_, data := scanner.Event()
			if data == "" || data == "[DONE]" {
				ch <- StreamEvent{Type: StreamDone}
				return
			}

			var evt struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
					FinishReason *string `json:"finish_reason"`
				} `json:"choices"`
				Usage *struct {
					PromptTokens     int `json:"prompt_tokens"`
					CompletionTokens int `json:"completion_tokens"`
					TotalTokens      int `json:"total_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				continue
			}

			if len(evt.Choices) > 0 {
				content := evt.Choices[0].Delta.Content
				if content != "" {
					ch <- StreamEvent{Type: StreamDelta, Content: content}
				}
				if evt.Choices[0].FinishReason != nil {
					usage := &TokenUsage{}
					if evt.Usage != nil {
						usage = &TokenUsage{
							InputTokens:  evt.Usage.PromptTokens,
							OutputTokens: evt.Usage.CompletionTokens,
							TotalTokens:  evt.Usage.TotalTokens,
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
