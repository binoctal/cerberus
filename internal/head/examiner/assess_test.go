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

func newAssessExaminer(t *testing.T, resp string) *Examiner {
	mock := llm.NewMockClient(map[string]string{"default": resp})
	driver := ai.NewDriver(mock, ai.NewTokenBudget(10000, 1000))
	return NewExaminer(driver, nil, setupExaminerStore(t), DefaultExaminerConfig(), zap.NewNop())
}

func stdContract() *contract.Contract {
	return &contract.Contract{
		Depth:        "standard",
		Scope:        []string{"internal/llm"},
		CoverageGate: contract.Gate{Module: "internal/llm", LineThreshold: 0.65},
	}
}

func TestAssessCoverage_BelowThresholdForcesNotReached(t *testing.T) {
	e := newAssessExaminer(t, `{"reached":true,"gaps":[],"coverage_pct":0.5,"reasoning":"ok"}`)
	res := []agent.StepResult{{TestCase: &agent.TestCase{ID: "tc-1", Target: "internal/llm"}}}

	a, err := e.AssessCoverage(context.Background(), stdContract(), res, contract.CoverageMeasurement{Pct: 0.50, Unit: "line", Known: true})
	require.NoError(t, err)
	assert.False(t, a.Reached, "objective gate overrides LLM reached=true")
	assert.Equal(t, 0.50, a.CoveragePct)
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
	e := newAssessExaminer(t, `{"reached":true,"gaps":[],"coverage_pct":0,"reasoning":"ok"}`)
	res := []agent.StepResult{{TestCase: &agent.TestCase{ID: "tc-1", Target: "internal/llm"}}}

	a, err := e.AssessCoverage(context.Background(), stdContract(), res, contract.CoverageMeasurement{Pct: 0, Unit: "line", Known: true})
	require.NoError(t, err)
	assert.False(t, a.Reached, "measured 0% is not unknown")
	assert.Equal(t, 0.0, a.CoveragePct)
}

func TestAssessCoverage_UnknownSkipsGate(t *testing.T) {
	e := newAssessExaminer(t, `{"reached":true,"gaps":[],"coverage_pct":0,"reasoning":"ok"}`)
	res := []agent.StepResult{{TestCase: &agent.TestCase{ID: "tc-1", Target: "internal/llm"}}}

	a, err := e.AssessCoverage(context.Background(), stdContract(), res, contract.CoverageMeasurement{Known: false})
	require.NoError(t, err)
	assert.True(t, a.Reached, "LLM judgment stands when coverage unmeasured")
	assert.Equal(t, 0.0, a.CoveragePct)
	for _, g := range a.Gaps {
		assert.NotEqual(t, "coverage", g.Kind, "no coverage gap appended when unknown")
	}
}

func TestAssessCoverage_FunctionUnitNotesMismatch(t *testing.T) {
	e := newAssessExaminer(t, `{"reached":true,"gaps":[],"coverage_pct":0.5,"reasoning":"ok"}`)
	res := []agent.StepResult{{TestCase: &agent.TestCase{ID: "tc-1", Target: "internal/llm"}}}

	a, err := e.AssessCoverage(context.Background(), stdContract(), res, contract.CoverageMeasurement{Pct: 0.50, Unit: "function", Known: true})
	require.NoError(t, err)
	assert.False(t, a.Reached)
	found := false
	for _, g := range a.Gaps {
		if g.Kind == "coverage" {
			found = true
			assert.Contains(t, g.Detail, "function")
		}
	}
	assert.True(t, found)
}

func TestAssessCoverage_BothAgreeReached(t *testing.T) {
	e := newAssessExaminer(t, `{"reached":true,"gaps":[],"coverage_pct":0.80,"reasoning":"ok"}`)
	res := []agent.StepResult{{TestCase: &agent.TestCase{ID: "tc-1", Target: "internal/llm"}}}

	a, err := e.AssessCoverage(context.Background(), stdContract(), res, contract.CoverageMeasurement{Pct: 0.80, Unit: "line", Known: true})
	require.NoError(t, err)
	assert.True(t, a.Reached)
	assert.Equal(t, 0.80, a.CoveragePct)
}
