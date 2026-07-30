package session

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
)

// TestEligibleFailures_WSCaseEligible: a Fail verdict on a WebSocket case
// (Steps present) with a WS hint is collected as a RepairInput carrying the WS
// case and hint — the repair loop is hint-agnostic, so WS cases flow through
// exactly like HTTP cases. Negative: hint==none → not eligible (RED).
func TestEligibleFailures_WSCaseEligible(t *testing.T) {
	rp, cleanup := newTestRunPhase(t)
	defer cleanup()

	rp.verdicts = []examiner.FinalVerdict{
		{
			Status:         examiner.StatusFail,
			RedispatchHint: agent.HintWsMatch,
			StepResult: agent.StepResult{
				TestCase: &agent.TestCase{
					ID: "ws-1", Action: "ws_flow", Target: "wss://x", Service: "ws",
					Steps: []agent.TestStep{
						{Action: "ws_connect", ConnectionID: "web"},
						{Action: "ws_receive", ConnectionID: "web", Type: "hello"},
					},
				},
			},
		},
	}
	out := rp.eligibleFailures(map[string]bool{})
	require.Len(t, out, 1, "WS Fail with a WS hint is eligible")
	assert.Equal(t, "ws-1", out[0].Case.ID)
	assert.Equal(t, agent.HintWsMatch, out[0].Hint)
	assert.NotEmpty(t, out[0].Case.Steps, "the WS flow (Steps) is carried into the RepairInput")
}

// TestEligibleFailures_WSCaseNoneHintNotEligible: a WS Fail with hint==none is
// NOT eligible (non-correctable). Negative reference.
func TestEligibleFailures_WSCaseNoneHintNotEligible(t *testing.T) {
	rp, cleanup := newTestRunPhase(t)
	defer cleanup()
	rp.verdicts = []examiner.FinalVerdict{
		{Status: examiner.StatusFail, RedispatchHint: agent.HintNone,
			StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "ws-2", Action: "ws_flow"}}},
	}
	out := rp.eligibleFailures(map[string]bool{})
	assert.Empty(t, out, "none-hint WS failure is not eligible")
}
