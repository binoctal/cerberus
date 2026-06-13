package llm

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
)

type MockClient struct {
	responses map[string]string
}

func NewMockClient(responses map[string]string) *MockClient {
	return &MockClient{responses: responses}
}

func (m *MockClient) Complete(ctx context.Context, req Request) (*Response, error) {
	key := m.matchKey(strings.Join(func() []string {
		var contents []string
		for _, msg := range req.Messages {
			contents = append(contents, msg.Content)
		}
		return contents
	}(), " "))

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
	if _, ok := m.responses[input]; ok {
		return input
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(input)))[:8]
}
