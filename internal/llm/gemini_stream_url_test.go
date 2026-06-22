package llm

import (
	"strings"
	"testing"
)

// Real Gemini requires ?alt=sse on streamGenerateContent to return a
// text/event-stream; without it the endpoint returns a JSON array and the SSE
// scanner yields nothing for real servers.
func TestGeminiStreamURLHasSSEFlag(t *testing.T) {
	c := NewGeminiClient("k", "gemini-pro", "")
	if got := c.streamURL(); !strings.Contains(got, "alt=sse") {
		t.Fatalf("streamURL must request SSE (alt=sse); got %q", got)
	}
}
