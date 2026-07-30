package examiner

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestJudgeTools_RedispatchHintEnum: the judge_result redispatch_hint enum
// carries the HTTP hints plus the D2 WS hints (handshake/ws_shape/ws_match), in
// lockstep with the parser and prompt. Negative: dropping any value fails (RED).
func TestJudgeTools_RedispatchHintEnum(t *testing.T) {
	tools := judgeTools()
	require.Len(t, tools, 1)
	props := tools[0].InputSchema["properties"].(map[string]any)
	hint := props["redispatch_hint"].(map[string]any)
	enum, ok := hint["enum"].([]any)
	require.True(t, ok)

	want := map[string]bool{
		"none": true, "endpoint_drift": true, "auth": true, "shape": true,
		"handshake": true, "ws_shape": true, "ws_match": true,
	}
	got := map[string]bool{}
	for _, v := range enum {
		got[v.(string)] = true
	}
	for w := range want {
		assert.True(t, got[w], "redispatch_hint enum must include %q", w)
	}
}
