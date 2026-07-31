package session

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
)

// TestSessionSummary_FailureHintsBreakdown: the summary derives a per-cause
// breakdown of correctable failures (non-none redispatch_hint on independent
// Fail units) so the report shows what the repair loop acted on. Recovered
// primaries still count (their hint is the cause that was repaired).
func TestSessionSummary_FailureHintsBreakdown(t *testing.T) {
	verdicts := []examiner.FinalVerdict{
		{Status: examiner.StatusFail, RedispatchHint: agent.HintEndpointDrift,
			StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "1"}}},
		{Status: examiner.StatusFail, RedispatchHint: agent.HintWsMatch,
			StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "2"}}},
		{Status: examiner.StatusFail, RedispatchHint: agent.HintWsShape,
			StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "3"}}},
		// none-hint fail + pass are NOT counted in the breakdown.
		{Status: examiner.StatusFail, RedispatchHint: agent.HintNone,
			StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "4"}}},
		{Status: examiner.StatusPass, RedispatchHint: agent.HintNone,
			StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "5"}}},
	}
	results := []agent.StepResult{}
	for _, v := range verdicts {
		results = append(results, v.StepResult)
	}

	s := FromResults("g", "", len(verdicts), results, verdicts, 0, 0, time.Duration(0))
	require.NotNil(t, s.FailureHints)
	assert.Equal(t, 1, s.FailureHints["endpoint_drift"])
	assert.Equal(t, 1, s.FailureHints["ws_match"])
	assert.Equal(t, 1, s.FailureHints["ws_shape"])
	assert.NotContains(t, s.FailureHints, "none", "none-hint failures are not a 'cause'")

	out := s.String()
	assert.Contains(t, out, "endpoint_drift")
	assert.Contains(t, out, "ws_match")
	assert.Contains(t, out, "ws_shape")
}

// TestSessionSummary_NoFailureHintsWhenClean: a passing session has no
// FailureHints (and the report omits the line).
func TestSessionSummary_NoFailureHintsWhenClean(t *testing.T) {
	verdicts := []examiner.FinalVerdict{
		{Status: examiner.StatusPass, StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "1"}}},
	}
	s := FromResults("g", "", 1, []agent.StepResult{verdicts[0].StepResult}, verdicts, 0, 0, 0)
	assert.Nil(t, s.FailureHints)
	assert.NotContains(t, s.String(), "Failure causes")
}

// TestSessionSummary_NonRepairableFailures: correctable failures on case types
// the repair mechanism cannot fix (process_exec, code_*, ...) are counted as
// NonRepairableFailures and surfaced in the report, so an operator understands
// why the repair loop did not act on them.
func TestSessionSummary_NonRepairableFailures(t *testing.T) {
	verdicts := []examiner.FinalVerdict{
		{Status: examiner.StatusFail, RedispatchHint: agent.HintShape,
			StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "p1", Action: "process_exec", Target: "go build ./..."}}},
		{Status: examiner.StatusFail, RedispatchHint: agent.HintEndpointDrift,
			StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "h1", Method: "GET", Target: "/u"}}},
		{Status: examiner.StatusFail, RedispatchHint: agent.HintShape,
			StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "c1", Action: "code_analyze"}}},
		// No hint and pass → not counted.
		{Status: examiner.StatusFail, RedispatchHint: agent.HintNone,
			StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "n1", Method: "GET", Target: "/x"}}},
		{Status: examiner.StatusPass,
			StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "ok", Method: "GET", Target: "/y"}}},
	}
	results := []agent.StepResult{}
	for _, v := range verdicts {
		results = append(results, v.StepResult)
	}
	s := FromResults("g", "", len(verdicts), results, verdicts, 0, 0, 0)
	assert.Equal(t, 2, s.NonRepairableFailures, "process_exec + code_analyze are non-repairable")
	out := s.String()
	assert.Contains(t, out, "Non-repairable by type: 2")
}
