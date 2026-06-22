package llm

import "testing"

// Real Claude sends output_tokens in a top-level "usage" on message_delta
// (not under message.usage, which is empty for this event). The handler must
// capture it instead of silently dropping it.
func TestHandleStreamEventMessageDeltaCapturesOutputTokens(t *testing.T) {
	data := `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":42}}`
	ch := make(chan StreamEvent, 2)
	handleStreamEvent(data, ch)
	close(ch)
	var got int
	for ev := range ch {
		if ev.Usage != nil {
			got = ev.Usage.OutputTokens
		}
	}
	if got != 42 {
		t.Fatalf("message_delta output_tokens not captured: got %d, want 42", got)
	}
}
