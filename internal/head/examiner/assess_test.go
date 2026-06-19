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

func TestAssessCoverage(t *testing.T) {
	mock := llm.NewMockClient(map[string]string{
		"default": `{"reached":false,"gaps":[{"kind":"scope","detail":"internal/session not covered"}],"coverage_pct":0.42,"reasoning":"scope incomplete"}`,
	})
	driver := ai.NewDriver(mock, ai.NewTokenBudget(10000, 1000))
	e := NewExaminer(driver, nil, setupExaminerStore(t), DefaultExaminerConfig(), zap.NewNop())

	c := &contract.Contract{Depth: "standard", Scope: []string{"internal/llm", "internal/session"}, CoverageGate: contract.Gate{Module: "internal/llm", LineThreshold: 0.65}}
	res := []agent.StepResult{{TestCase: &agent.TestCase{ID: "tc-1", Target: "internal/llm"}}}

	a, err := e.AssessCoverage(context.Background(), c, res, 0.42)
	require.NoError(t, err)
	assert.False(t, a.Reached)
	assert.NotEmpty(t, a.Gaps)
}

func TestAssessCoverage_ObjectiveGateOverride(t *testing.T) {
	// LLM says reached, but coverage is below threshold → override to false.
	mock := llm.NewMockClient(map[string]string{
		"default": `{"reached":true,"gaps":[],"coverage_pct":0.5,"reasoning":"all scope covered"}`,
	})
	driver := ai.NewDriver(mock, ai.NewTokenBudget(10000, 1000))
	e := NewExaminer(driver, nil, setupExaminerStore(t), DefaultExaminerConfig(), zap.NewNop())

	c := &contract.Contract{Depth: "standard", Scope: []string{"internal/llm", "internal/session"}, CoverageGate: contract.Gate{Module: "internal/llm", LineThreshold: 0.65}}
	res := []agent.StepResult{{TestCase: &agent.TestCase{ID: "tc-1", Target: "internal/llm"}}}

	// Coverage 50% < 65% gate → should override LLM's "reached=true"
	a, err := e.AssessCoverage(context.Background(), c, res, 0.50)
	require.NoError(t, err)
	assert.False(t, a.Reached, "objective gate should override LLM judgment")
	assert.Equal(t, 0.50, a.CoveragePct)

	// Should have the coverage gap from override
	foundCoverageGap := false
	for _, gap := range a.Gaps {
		if gap.Kind == "coverage" {
			foundCoverageGap = true
			assert.Contains(t, gap.Detail, "50% < 65%")
		}
	}
	assert.True(t, foundCoverageGap, "should have coverage gap from objective gate override")
}

func TestAssessCoverage_BothAgreeReached(t *testing.T) {
	// LLM says reached AND coverage passes threshold → reached.
	mock := llm.NewMockClient(map[string]string{
		"default": `{"reached":true,"gaps":[],"coverage_pct":0.80,"reasoning":"all good"}`,
	})
	driver := ai.NewDriver(mock, ai.NewTokenBudget(10000, 1000))
	e := NewExaminer(driver, nil, setupExaminerStore(t), DefaultExaminerConfig(), zap.NewNop())

	c := &contract.Contract{Depth: "standard", Scope: []string{"internal/llm"}, CoverageGate: contract.Gate{Module: "internal/llm", LineThreshold: 0.65}}
	res := []agent.StepResult{{TestCase: &agent.TestCase{ID: "tc-1", Target: "internal/llm"}}}

	a, err := e.AssessCoverage(context.Background(), c, res, 0.80)
	require.NoError(t, err)
	assert.True(t, a.Reached, "both LLM and objective gate agree")
	assert.Equal(t, 0.80, a.CoveragePct)
}
