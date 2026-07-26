package examiner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/llm"
)

// reportReflectionCall builds a report_reflection tool call fixture matching
// the learnerTools() schema.
func reportReflectionCall(r Reflection) llm.ToolCall {
	return llm.ToolCall{
		Name: "report_reflection",
		Input: map[string]any{
			"type":              r.Type,
			"diagnosis":         r.Diagnosis,
			"strategy":          r.Strategy,
			"condition_pattern": r.ConditionPattern,
			"category":          r.Category,
		},
	}
}

// TestLearner_ToolCallAssembles is the S4 RED→GREEN gate: preset 2×
// report_reflection tool calls and assert both flow through Learn() into
// storage. Pre-migration this fixture was a JSON array string; post-migration
// it is N tool calls that assembleReflections walks.
func TestLearner_ToolCallAssembles(t *testing.T) {
	s := setupExaminerStore(t)
	mockClient := llm.NewMockClient(nil)
	mockClient.SetToolResponse("default", []llm.ToolCall{
		reportReflectionCall(Reflection{
			Type:             "failure",
			Diagnosis:        "Missing auth header",
			Strategy:         "Add Authorization: Bearer <token>",
			ConditionPattern: "GET /api/users → 401",
			Category:         "auth_failure",
		}),
		reportReflectionCall(Reflection{
			Type:             "success",
			Diagnosis:        "Happy path stable",
			Strategy:         "Reuse the same retry policy",
			ConditionPattern: "GET /api/health → 200",
			Category:         "general_failure",
		}),
	})
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))
	learner := NewLearner(driver, s, zap.NewNop(), nil)

	results := []agent.StepResult{
		makeStepResult("tc-1", "Get users", "/api/users", "returns 200", agent.StepFailed, 401, ""),
	}

	stored, err := learner.Learn(context.Background(), LearnInput{
		SessionID: "s1",
		Project:   "test-project",
		Results:   results,
	})
	require.NoError(t, err)
	assert.Equal(t, 2, stored, "both reflections pass the quality gate and are stored")
}

// TestLearner_ZeroToolCalls_NoPropagate verifies the learner error policy:
// zero tool calls (drift/quality) degrade to empty reflections (NOT
// propagate). This differs from assess (propagates) but mirrors autofix's
// graceful degrade — Reflexion is non-fatal background learning.
func TestLearner_ZeroToolCalls_NoPropagate(t *testing.T) {
	s := setupExaminerStore(t)
	mockClient := llm.NewMockClient(nil)
	mockClient.SetToolResponse("default", nil) // zero tool calls
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))
	learner := NewLearner(driver, s, zap.NewNop(), nil)

	results := []agent.StepResult{
		makeStepResult("tc-1", "Get users", "/api/users", "returns 200", agent.StepFailed, 401, ""),
	}

	stored, err := learner.Learn(context.Background(), LearnInput{
		SessionID: "s1",
		Project:   "test-project",
		Results:   results,
	})
	require.NoError(t, err, "zero tool calls must NOT propagate (graceful degrade to empty reflections)")
	assert.Equal(t, 0, stored)
}

// TestLearner_QualityGateStillFilters confirms the quality gate is preserved
// after migration: tool calls whose assembled Reflection fails qualityGate are
// dropped before L3 storage, even when sibling calls pass.
func TestLearner_QualityGateStillFilters(t *testing.T) {
	s := setupExaminerStore(t)
	mockClient := llm.NewMockClient(nil)
	mockClient.SetToolResponse("default", []llm.ToolCall{
		// Passes quality gate.
		reportReflectionCall(Reflection{
			Type:             "failure",
			Diagnosis:        "Server timeout",
			Strategy:         "Retry with exponential backoff",
			ConditionPattern: "GET /api/x → 504",
			Category:         "timeout_recovery",
		}),
		// Fails quality gate: empty diagnosis.
		reportReflectionCall(Reflection{
			Type:             "failure",
			Diagnosis:        "",
			Strategy:         "Strategy without diagnosis",
			ConditionPattern: "*",
			Category:         "general_failure",
		}),
		// Fails quality gate: strategy too short.
		reportReflectionCall(Reflection{
			Type:             "success",
			Diagnosis:        "Works well",
			Strategy:         "short",
			ConditionPattern: "GET *",
			Category:         "general_failure",
		}),
	})
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))
	learner := NewLearner(driver, s, zap.NewNop(), nil)

	results := []agent.StepResult{
		makeStepResult("tc-1", "Test", "/api", "works", agent.StepFailed, 500, ""),
	}

	stored, err := learner.Learn(context.Background(), LearnInput{
		SessionID: "s1",
		Project:   "test-project",
		Results:   results,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, stored, "only the quality-gate-passing reflection is stored")
}
