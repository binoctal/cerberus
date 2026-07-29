package session

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
)

func TestSessionSummary_FromResults(t *testing.T) {
	results := []agent.StepResult{
		{TestCase: &agent.TestCase{ID: "tc-001"}, Status: agent.StepPassed},
		{TestCase: &agent.TestCase{ID: "tc-002"}, Status: agent.StepPassed},
		{TestCase: &agent.TestCase{ID: "tc-003"}, Status: agent.StepFailed},
		{TestCase: &agent.TestCase{ID: "tc-004"}, Status: agent.StepSkipped},
	}

	verdicts := []examiner.FinalVerdict{
		{Status: examiner.StatusPass, StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "tc-001"}}},
		{Status: examiner.StatusPass, StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "tc-002"}}},
		{Status: examiner.StatusFail, StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "tc-003"}}},
		{Status: examiner.StatusSkip, StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "tc-004"}}},
	}

	summary := FromResults("test goal", "http://localhost:8080", 4, results, verdicts, 2, 50000, 2*time.Second)

	assert.Equal(t, "test goal", summary.Goal)
	assert.Equal(t, 4, summary.TotalCases)
	assert.Equal(t, 2, summary.Passed)
	assert.Equal(t, 1, summary.Failed)
	assert.Equal(t, 1, summary.Skipped)
	assert.Equal(t, 4, summary.Verdicts)
	assert.Equal(t, 2, summary.ReflectionsStored)
	assert.Equal(t, 50000, summary.TotalTokens)
	assert.InDelta(t, 50.0, summary.CoveragePct, 0.01) // 2 passed / 4 total * 100
}

func TestSessionSummary_FromResults_PrefersVerdictStatus(t *testing.T) {
	// Agent executed pass, but Examiner downgraded to uncertain (low
	// correctness). The summary must reflect the final verdict, not the raw
	// step status — otherwise reports undercount uncertain verdicts.
	results := []agent.StepResult{
		{TestCase: &agent.TestCase{ID: "tc-001"}, Status: agent.StepPassed},
	}
	verdicts := []examiner.FinalVerdict{
		{Status: examiner.StatusUncertain, StepResult: results[0]},
	}
	summary := FromResults("g", "", 1, results, verdicts, 0, 0, 0)
	assert.Equal(t, 0, summary.Passed, "verdict uncertain overrides step pass")
	assert.Equal(t, 1, summary.Uncertain)
}

func TestSessionSummary_String(t *testing.T) {
	summary := &SessionSummary{
		Passed:            10,
		Failed:            2,
		Skipped:           1,
		Uncertain:         1,
		PendingReview:     1,
		ReflectionsStored: 3,
		TotalTokens:       105000,
		Duration:          "2.5s",
	}

	s := summary.String()
	assert.Contains(t, s, "10 pass")
	assert.Contains(t, s, "2 fail")
	assert.Contains(t, s, "1 skip")
	assert.Contains(t, s, "1 uncertain")
	assert.Contains(t, s, "~105K")
}

func TestSessionSummary_ToJSON(t *testing.T) {
	summary := &SessionSummary{
		Goal:   "test",
		Passed: 5, Failed: 1, TotalTokens: 50000,
	}
	j := summary.ToJSON()
	assert.Contains(t, j, `"passed": 5`)
	assert.Contains(t, j, `"goal": "test"`)
}

func TestSessionSummary_CoveragePct_EdgeCases(t *testing.T) {
	t.Run("zero cases", func(t *testing.T) {
		summary := FromResults("goal", "", 0, nil, nil, 0, 0, 0)
		assert.InDelta(t, 0.0, summary.CoveragePct, 0.01)
	})

	t.Run("all pass", func(t *testing.T) {
		results := []agent.StepResult{
			{Status: agent.StepPassed},
			{Status: agent.StepPassed},
		}
		summary := FromResults("goal", "", 2, results, nil, 0, 0, 0)
		assert.InDelta(t, 100.0, summary.CoveragePct, 0.01)
	})

	t.Run("all fail", func(t *testing.T) {
		results := []agent.StepResult{
			{Status: agent.StepFailed},
			{Status: agent.StepFailed},
		}
		summary := FromResults("goal", "", 2, results, nil, 0, 0, 0)
		assert.InDelta(t, 0.0, summary.CoveragePct, 0.01)
	})
}

// TestFromResults_RecoveredPairing is the golden case from the design:
// roles A, B, C; A has fallback A' (recovered), B has fallback B' (not
// recovered), C standalone. A reclassifies to Recovered (not Failed); the
// fallback results are not independent units; coverage counts Recovered.
func TestFromResults_RecoveredPairing(t *testing.T) {
	results := []agent.StepResult{
		{TestCase: &agent.TestCase{ID: "A"}, Status: agent.StepFailed},
		{TestCase: &agent.TestCase{ID: "B"}, Status: agent.StepFailed},
		{TestCase: &agent.TestCase{ID: "C"}, Status: agent.StepPassed},
		{TestCase: &agent.TestCase{ID: "A'", FallbackFor: "A"}, Status: agent.StepPassed, Recovered: true},
		{TestCase: &agent.TestCase{ID: "B'", FallbackFor: "B"}, Status: agent.StepFailed},
	}
	verdicts := []examiner.FinalVerdict{
		{Status: examiner.StatusFail, StepResult: results[0]},
		{Status: examiner.StatusFail, StepResult: results[1]},
		{Status: examiner.StatusPass, StepResult: results[2]},
		{Status: examiner.StatusPass, StepResult: results[3]},
		{Status: examiner.StatusFail, StepResult: results[4]},
	}

	summary := FromResults("g", "", 5, results, verdicts, 0, 0, 0)

	assert.Equal(t, 3, summary.TotalCases, "fallback results excluded from total")
	assert.Equal(t, 1, summary.Passed, "only C passed")
	assert.Equal(t, 1, summary.Failed, "only B failed; A reclassified out of Failed")
	assert.Equal(t, 1, summary.Recovered, "A recovered")
	assert.InDelta(t, 66.67, summary.CoveragePct, 0.01, "(Passed+Recovered)/Total")
}

// TestFromResults_AllRecovered: a recovered role does not surface as Failed.
func TestFromResults_AllRecovered(t *testing.T) {
	results := []agent.StepResult{
		{TestCase: &agent.TestCase{ID: "A"}, Status: agent.StepFailed},
		{TestCase: &agent.TestCase{ID: "A'", FallbackFor: "A"}, Status: agent.StepPassed, Recovered: true},
	}
	verdicts := []examiner.FinalVerdict{
		{Status: examiner.StatusFail, StepResult: results[0]},
		{Status: examiner.StatusPass, StepResult: results[1]},
	}
	summary := FromResults("g", "", 2, results, verdicts, 0, 0, 0)
	assert.Equal(t, 0, summary.Failed, "recovered primary is not Failed")
	assert.Equal(t, 1, summary.Recovered)
	assert.Equal(t, 1, summary.TotalCases)
	assert.InDelta(t, 100.0, summary.CoveragePct, 0.01)
}

// TestFromResults_RecoveredRawResults: the raw-results branch (no verdicts)
// also honors Recovered/FallbackFor.
func TestFromResults_RecoveredRawResults(t *testing.T) {
	results := []agent.StepResult{
		{TestCase: &agent.TestCase{ID: "A"}, Status: agent.StepFailed},
		{TestCase: &agent.TestCase{ID: "A'", FallbackFor: "A"}, Status: agent.StepPassed, Recovered: true},
	}
	summary := FromResults("g", "", 2, results, nil, 0, 0, 0)
	assert.Equal(t, 0, summary.Failed)
	assert.Equal(t, 1, summary.Recovered)
}

func TestSessionSummary_StringIncludesRecovered(t *testing.T) {
	s := &SessionSummary{Passed: 1, Failed: 1, Skipped: 0, Uncertain: 0, Recovered: 1,
		PendingReview: 0, ReflectionsStored: 0, TotalTokens: 0, Duration: "1s"}
	assert.Contains(t, s.String(), "1 recovered")
}
