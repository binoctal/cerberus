package examiner

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPromptJudgeSystem_WSFailureModes: the judge prompt names the D2 WS
// correctable causes (handshake/ws_shape/ws_match) so the LLM can diagnose WS
// failures beyond the HTTP shape/none collapse. Negative: removing a mode from
// the prompt fails the assertion (RED).
func TestPromptJudgeSystem_WSFailureModes(t *testing.T) {
	for _, mode := range []string{"handshake", "ws_shape", "ws_match"} {
		assert.Contains(t, promptJudgeSystem, mode,
			"judge prompt must teach the WS failure mode %q", mode)
	}
}
