package llm

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockClient_Stream(t *testing.T) {
	mock := NewMockClient(map[string]string{
		"default": `{"status":"pass","confidence":0.9}`,
	})

	events, err := mock.Stream(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "test"}},
	})
	require.NoError(t, err)

	var collected strings.Builder
	var gotDone bool
	for evt := range events {
		switch evt.Type {
		case StreamDelta:
			collected.WriteString(evt.Content)
		case StreamDone:
			gotDone = true
			assert.NotNil(t, evt.Usage)
		case StreamError:
			t.Fatalf("unexpected error: %v", evt.Err)
		}
	}

	assert.True(t, gotDone, "should receive done event")
	assert.Equal(t, `{"status":"pass","confidence":0.9}`, collected.String())
}

func TestMockClient_Stream_DefaultResponse(t *testing.T) {
	mock := NewMockClient(nil)

	events, err := mock.Stream(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "test"}},
	})
	require.NoError(t, err)

	var gotDone bool
	for evt := range events {
		if evt.Type == StreamDone {
			gotDone = true
		}
	}
	assert.True(t, gotDone)
}

func TestMockClient_SetToolResponse(t *testing.T) {
	mock := NewMockClient(nil)
	mock.SetToolResponse("plan a relay", []ToolCall{
		{ID: "call_1", Name: "ws_relay", Input: map[string]any{"roles": []any{"web", "bridge"}}},
	})
	resp, err := mock.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "plan a relay"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "" {
		t.Errorf("Content: got %q, want empty (pure tool_use turn)", resp.Content)
	}
	if resp.StopReason != "tool_use" {
		t.Errorf("StopReason: got %q, want tool_use", resp.StopReason)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "ws_relay" {
		t.Fatalf("ToolCalls: got %+v, want one ws_relay call", resp.ToolCalls)
	}
}

// TestMockClient_ToolResponseSequence verifies that successive Complete calls
// for the same key rotate through a preset sequence and then hold on the last
// element. This is how N-sample voting tests represent run-to-run variance:
// the mock must return DIFFERENT drafts across calls with an identical prompt.
func TestMockClient_ToolResponseSequence(t *testing.T) {
	mock := NewMockClient(nil)
	mock.SetToolResponseSequence("default", [][]ToolCall{
		{{Name: "protocol_draft", Input: map[string]any{"found": false}}},        // false negative
		{{Name: "protocol_draft", Input: map[string]any{"found": true, "v": 1}}}, // good draft
		{{Name: "protocol_draft", Input: map[string]any{"found": true, "v": 2}}}, // good draft
	})

	call := func() *Response {
		resp, err := mock.Complete(context.Background(), Request{
			Messages: []Message{{Role: "user", Content: "anything"}},
		})
		if err != nil {
			t.Fatalf("Complete: %v", err)
		}
		return resp
	}

	r1 := call()
	if found, _ := r1.ToolCalls[0].Input["found"].(bool); found {
		t.Fatalf("call 1: want first sequence element (found=false), got %+v", r1.ToolCalls)
	}
	r2 := call()
	if v, _ := r2.ToolCalls[0].Input["v"].(int); v != 1 {
		t.Fatalf("call 2: want v=1, got %v", r2.ToolCalls[0].Input["v"])
	}
	r3 := call()
	if v, _ := r3.ToolCalls[0].Input["v"].(int); v != 2 {
		t.Fatalf("call 3: want v=2, got %v", r3.ToolCalls[0].Input["v"])
	}
	// Exhausted sequence holds on the last element.
	r4 := call()
	if v, _ := r4.ToolCalls[0].Input["v"].(int); v != 2 {
		t.Fatalf("call 4: want held last (v=2), got %v", r4.ToolCalls[0].Input["v"])
	}
}

// matchKey must consult toolResponses too — a key present only there must NOT
// hash to sha256[:8] (which would make SetToolResponse silently never match).
func TestMockClient_MatchKeyConsultsToolResponses(t *testing.T) {
	mock := NewMockClient(nil)
	mock.SetToolResponse("only-in-tool-responses", []ToolCall{{Name: "f"}})
	resp, err := mock.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "only-in-tool-responses"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("toolResponses-only key must match directly (no hash); got ToolCalls=%+v", resp.ToolCalls)
	}
}
