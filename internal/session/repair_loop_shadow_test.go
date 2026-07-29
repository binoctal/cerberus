package session

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
)

// TestEligibleFailures_ShadowsReplacedOriginal: on a hint-CHANGE (drift->auth),
// computeStuck deliberately does NOT mark the target stuck — that is progress.
// Without shadowing, eligibleFailures would emit BOTH the original (tc-1, fail
// drift) and its replacement (repair-tc-1, fail auth), causing Scout to mint a
// colliding repair-tc-1 ID and duplicate Agent work. Spec §5.3: only the latest
// replacement in the Replaces chain is eligible; the original is shadowed.
func TestEligibleFailures_ShadowsReplacedOriginal(t *testing.T) {
	verdicts := []examiner.FinalVerdict{
		{
			Status:         examiner.StatusFail,
			RedispatchHint: agent.HintEndpointDrift,
			StepResult: agent.StepResult{
				TestCase: &agent.TestCase{ID: "tc-1", Target: "/users", Method: "GET"},
			},
		},
		{
			Status:         examiner.StatusFail,
			RedispatchHint: agent.HintAuth, // hint changed (drift->auth) -> progress, NOT stuck
			StepResult: agent.StepResult{
				TestCase: &agent.TestCase{ID: "repair-tc-1", Target: "/users", Method: "GET", Replaces: "tc-1"},
			},
		},
	}

	rp := &runPhase{verdicts: verdicts}
	eligible := rp.eligibleFailures(computeStuck(verdicts))

	assert.Len(t, eligible, 1, "exactly one eligible input (the replacement; original is shadowed)")
	if len(eligible) == 1 {
		assert.Equal(t, "repair-tc-1", eligible[0].Case.ID, "shadowed original tc-1 must not be re-emitted")
		assert.Equal(t, agent.HintAuth, eligible[0].Hint)
	}
}
