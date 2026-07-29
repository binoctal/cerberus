package examiner

import (
	"testing"

	"github.com/binoctal/cerberus/internal/head/agent"
)

// TestNewFinalVerdict_PropagatesRedispatchHint: the hint parsed by the judge
// flows onto the FinalVerdict so the repair loop (Task 4) can read it.
func TestNewFinalVerdict_PropagatesRedispatchHint(t *testing.T) {
	for _, hint := range []agent.RedispatchHint{
		agent.HintNone, agent.HintEndpointDrift, agent.HintAuth, agent.HintShape,
	} {
		jr := &JudgeResult{Status: StatusFail, RedispatchHint: hint}
		v := newFinalVerdict(jr, agent.StepResult{})
		if v.RedispatchHint != hint {
			t.Fatalf("hint %q not propagated to FinalVerdict (got %q)", hint, v.RedispatchHint)
		}
	}
}

// TestFallbackVerdict_HintNone: a degraded/fallback verdict must not accidentally
// trigger replanning — it defaults to HintNone.
func TestFallbackVerdict_HintNone(t *testing.T) {
	v := fallbackVerdict(agent.StepResult{Status: agent.StepFailed}, 0.5, "examiner unavailable")
	if v.RedispatchHint != agent.HintNone {
		t.Fatalf("fallback verdict must default to HintNone, got %q", v.RedispatchHint)
	}
}
