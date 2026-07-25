package llm

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
)

type MockClient struct {
	responses     map[string]string
	toolResponses map[string][]ToolCall
}

func NewMockClient(responses map[string]string) *MockClient {
	return &MockClient{responses: responses}
}

// SetToolResponse presets a tool_use response for a given prompt key. When
// Complete matches this key it returns the tool calls (Content empty,
// StopReason "tool_use") instead of a text response.
func (m *MockClient) SetToolResponse(key string, calls []ToolCall) {
	if m.toolResponses == nil {
		m.toolResponses = map[string][]ToolCall{}
	}
	m.toolResponses[key] = calls
}

func (m *MockClient) Complete(ctx context.Context, req Request) (*Response, error) {
	key := m.matchKey(strings.Join(func() []string {
		var contents []string
		for _, msg := range req.Messages {
			contents = append(contents, msg.Content)
		}
		return contents
	}(), " "))

	if calls, ok := m.toolResponses[key]; ok {
		return &Response{
			Content:    "",
			ToolCalls:  calls,
			StopReason: "tool_use",
			Usage: TokenUsage{
				InputTokens:  len(req.Messages) * 10,
				OutputTokens: 0,
				TotalTokens:  len(req.Messages) * 10,
			},
		}, nil
	}

	content, ok := m.responses[key]
	if !ok {
		content = m.responses["default"]
	}

	inputTokens := len(req.Messages) * 10
	outputTokens := len(content) / 4
	return &Response{
		Content:    content,
		StopReason: "end_turn",
		Usage: TokenUsage{
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			TotalTokens:  inputTokens + outputTokens,
		},
	}, nil
}

func (m *MockClient) CompleteWithVision(ctx context.Context, prompt string, images [][]byte) (*Response, error) {
	resp, err := m.Complete(ctx, Request{
		Messages: []Message{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return nil, err
	}
	resp.Usage.InputTokens += len(images) * 100
	resp.Usage.TotalTokens = resp.Usage.InputTokens + resp.Usage.OutputTokens
	return resp, nil
}

func (m *MockClient) Stream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	// Use Complete to get the full response, then emit as delta + done.
	resp, err := m.Complete(ctx, req)
	if err != nil {
		ch := make(chan StreamEvent, 1)
		ch <- StreamEvent{Type: StreamError, Err: err}
		close(ch)
		return ch, nil
	}

	ch := make(chan StreamEvent, 2)
	ch <- StreamEvent{Type: StreamDelta, Content: resp.Content, Usage: &TokenUsage{
		InputTokens:  resp.Usage.InputTokens,
		OutputTokens: 0, // partial
		TotalTokens:  resp.Usage.InputTokens,
	}}
	ch <- StreamEvent{Type: StreamDone, Usage: &resp.Usage}
	close(ch)
	return ch, nil
}

func (m *MockClient) matchKey(input string) string {
	// Exact match takes priority — preserves TestMockClient_MatchKeyConsultsToolResponses.
	if _, ok := m.responses[input]; ok {
		return input
	}
	if _, ok := m.toolResponses[input]; ok {
		return input
	}
	// Substring fallback for tool responses only. Supports the common test
	// idiom of keying SetToolResponse on the goal string: the Scout planning
	// prompt embeds the goal as "Test Goal: <goal>", so the joined message
	// contents contain the goal as a substring. The longest matching key wins
	// to disambiguate when multiple substrings could match.
	var bestKey string
	for k := range m.toolResponses {
		if k == "" || len(k) > len(input) {
			continue
		}
		if strings.Contains(input, k) && len(k) > len(bestKey) {
			bestKey = k
		}
	}
	if bestKey != "" {
		return bestKey
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(input)))[:8]
}
