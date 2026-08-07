package examiner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/contract"
	"github.com/binoctal/cerberus/internal/llm"
)

func newAssessExaminer(t *testing.T) (*Examiner, *llm.MockClient) {
	mock := llm.NewMockClient(nil)
	driver := ai.NewDriver(mock, ai.NewTokenBudget(10000, 1000))
	return NewExaminer(driver, nil, setupExaminerStore(t), DefaultExaminerConfig(), zap.NewNop()), mock
}

func stdContract() *contract.Contract {
	return &contract.Contract{
		Depth:        "standard",
		Scope:        []string{"internal/llm"},
		CoverageGate: contract.Gate{Module: "internal/llm", LineThreshold: 0.65},
	}
}

// assessCoverageCall builds an assess_coverage tool call fixture matching the
// assessTools() schema. coverage_pct is intentionally absent — the schema
// omits it (always overwritten by the objective measure in assess.go).
func assessCoverageCall(reached bool, gaps []contract.Gap, reasoning string) llm.ToolCall {
	rawGaps := make([]any, len(gaps))
	for i, g := range gaps {
		rawGaps[i] = map[string]any{"kind": g.Kind, "detail": g.Detail}
	}
	return llm.ToolCall{
		Name: "assess_coverage",
		Input: map[string]any{
			"reached":   reached,
			"gaps":      rawGaps,
			"reasoning": reasoning,
		},
	}
}

// TestAssessCoverage_ToolCallAssembles is the S4 RED→GREEN gate: preset
// assess_coverage{reached:false, gaps:[{kind,detail}]} and assert the assembled
// Assessment carries the same {reached, gaps, reasoning}. Pre-migration this
// fixture was a JSON string; post-migration it is a tool call that
// assembleAssessment walks. coverage_pct is NOT in the fixture — the schema
// omits it (overwritten by the objective measure downstream).
func TestAssessCoverage_ToolCallAssembles(t *testing.T) {
	// Measured (Known:true, 0.80 ≥ 0.65 gate) so the LLM IS consulted and its
	// {reached, gaps, reasoning} round-trip is exercised — without the objective
	// gate forcing a value. The gate does not append a coverage gap (0.80 ≥ 0.65).
	e, mock := newAssessExaminer(t)
	mock.SetToolResponse("default", []llm.ToolCall{
		assessCoverageCall(false, []contract.Gap{
			{Kind: "scope", Detail: "no /admin"},
			{Kind: "boundary", Detail: "no zero"},
		}, "two gaps remain"),
	})
	res := []agent.StepResult{{TestCase: &agent.TestCase{ID: "tc-1", Target: "internal/llm"}}}

	a, err := e.AssessCoverage(context.Background(), stdContract(), res, contract.CoverageMeasurement{Pct: 0.80, Unit: "line", Known: true})
	require.NoError(t, err)
	require.NotNil(t, a)
	assert.True(t, a.Measured, "measured path sets Measured=true")
	assert.False(t, a.Reached, "LLM emitted reached=false")
	assert.Equal(t, 0.80, a.CoveragePct, "measurement is the sole source of CoveragePct")
	assert.Equal(t, "two gaps remain", a.Reasoning)
	require.Len(t, a.Gaps, 2)
	assert.Equal(t, contract.Gap{Kind: "scope", Detail: "no /admin"}, a.Gaps[0])
	assert.Equal(t, contract.Gap{Kind: "boundary", Detail: "no zero"}, a.Gaps[1])
}

func TestAssessCoverage_BelowThresholdForcesNotReached(t *testing.T) {
	// The schema no longer permits an LLM-side coverage_pct (assessTools omits
	// it); the measurement is the sole source of CoveragePct. reached=true is
	// still overridden by the objective gate (0.50 < 0.65).
	e, mock := newAssessExaminer(t)
	mock.SetToolResponse("default", []llm.ToolCall{
		assessCoverageCall(true, nil, "ok"),
	})
	res := []agent.StepResult{{TestCase: &agent.TestCase{ID: "tc-1", Target: "internal/llm"}}}

	a, err := e.AssessCoverage(context.Background(), stdContract(), res, contract.CoverageMeasurement{Pct: 0.50, Unit: "line", Known: true})
	require.NoError(t, err)
	assert.False(t, a.Reached, "objective gate overrides LLM reached=true")
	assert.Equal(t, 0.50, a.CoveragePct, "measurement is the sole source of CoveragePct")
	found := false
	for _, g := range a.Gaps {
		if g.Kind == "coverage" {
			found = true
			assert.Contains(t, g.Detail, "50% < 65%")
		}
	}
	assert.True(t, found)
}

func TestAssessCoverage_MeasuredZeroForcesNotReached(t *testing.T) {
	e, mock := newAssessExaminer(t)
	mock.SetToolResponse("default", []llm.ToolCall{
		assessCoverageCall(true, nil, "ok"),
	})
	res := []agent.StepResult{{TestCase: &agent.TestCase{ID: "tc-1", Target: "internal/llm"}}}

	a, err := e.AssessCoverage(context.Background(), stdContract(), res, contract.CoverageMeasurement{Pct: 0, Unit: "line", Known: true})
	require.NoError(t, err)
	assert.False(t, a.Reached, "measured 0% is not unknown")
	assert.Equal(t, 0.0, a.CoveragePct, "measurement is the sole source of CoveragePct")
	found := false
	for _, g := range a.Gaps {
		if g.Kind == "coverage" {
			found = true
			assert.Contains(t, g.Detail, "0% < 65%")
		}
	}
	assert.True(t, found, "coverage gap appended at measured 0%")
}

// TestAssessCoverage_UnmeasuredIsNotApplicable verifies Phase 1: a SaaS session
// with no measurable local SUT gets an honest "not applicable" ({Measured:false})
// assessment instead of the prior hallucinated 0% / reached=false / invented
// gaps. The LLM is NOT consulted — a queued assess_coverage response exists, and
// we assert the result does NOT reflect it (the response was never consumed).
func TestAssessCoverage_UnmeasuredIsNotApplicable(t *testing.T) {
	e, mock := newAssessExaminer(t)
	// Booby-trap: queue a response so that if the short-circuit were missing,
	// the LLM would be called and the assertions below would fail.
	mock.SetToolResponse("default", []llm.ToolCall{
		assessCoverageCall(true, nil, "SHOULD NOT BE USED"),
	})
	res := []agent.StepResult{{TestCase: &agent.TestCase{ID: "tc-1", Target: "internal/llm"}}}

	a, err := e.AssessCoverage(context.Background(), stdContract(), res, contract.CoverageMeasurement{Known: false})
	require.NoError(t, err)
	require.NotNil(t, a)
	assert.False(t, a.Measured, "unmeasured assessment reports Measured=false")
	assert.False(t, a.Reached, "unmeasured assessment must not assert Reached=true (misleading)")
	assert.Equal(t, 0.0, a.CoveragePct, "unmeasured CoveragePct is the zero value (no value)")
	assert.Empty(t, a.Reasoning, "queued LLM response was never consumed")
	for _, g := range a.Gaps {
		assert.NotEqual(t, "coverage", g.Kind, "unmeasured assessment must not emit a coverage gap")
	}
}

func TestAssessCoverage_FunctionUnitNotesMismatch(t *testing.T) {
	e, mock := newAssessExaminer(t)
	mock.SetToolResponse("default", []llm.ToolCall{
		assessCoverageCall(true, nil, "ok"),
	})
	res := []agent.StepResult{{TestCase: &agent.TestCase{ID: "tc-1", Target: "internal/llm"}}}

	a, err := e.AssessCoverage(context.Background(), stdContract(), res, contract.CoverageMeasurement{Pct: 0.50, Unit: "function", Known: true})
	require.NoError(t, err)
	assert.False(t, a.Reached)
	found := false
	for _, g := range a.Gaps {
		if g.Kind == "coverage" {
			found = true
			assert.Contains(t, g.Detail, " (measured as function coverage)")
		}
	}
	assert.True(t, found)
}

func TestAssessCoverage_BothAgreeReached(t *testing.T) {
	e, mock := newAssessExaminer(t)
	mock.SetToolResponse("default", []llm.ToolCall{
		assessCoverageCall(true, nil, "ok"),
	})
	res := []agent.StepResult{{TestCase: &agent.TestCase{ID: "tc-1", Target: "internal/llm"}}}

	a, err := e.AssessCoverage(context.Background(), stdContract(), res, contract.CoverageMeasurement{Pct: 0.80, Unit: "line", Known: true})
	require.NoError(t, err)
	assert.True(t, a.Reached)
	assert.Equal(t, 0.80, a.CoveragePct, "measurement is the sole source of CoveragePct")
}

// TestAssessCoverage_ZeroToolCalls_Errors verifies the assess error policy:
// zero tool calls (drift/quality) PROPAGATE as "assess coverage: ..." — NOT a
// silent degrade. assess is the ONLY Examiner site that propagates (judge,
// critic, autofix, learner all degrade gracefully). assess feeds the contract
// gate, so drift must surface as an error, not look like "not reached".
func TestAssessCoverage_ZeroToolCalls_Errors(t *testing.T) {
	e, mock := newAssessExaminer(t)
	mock.SetToolResponse("default", nil) // zero tool calls
	res := []agent.StepResult{{TestCase: &agent.TestCase{ID: "tc-1", Target: "internal/llm"}}}

	_, err := e.AssessCoverage(context.Background(), stdContract(), res, contract.CoverageMeasurement{Pct: 0.50, Unit: "line", Known: true})
	require.Error(t, err, "zero tool calls must propagate as an error")
	assert.Contains(t, err.Error(), "assess coverage")
	assert.Contains(t, err.Error(), "zero tool calls")
}
