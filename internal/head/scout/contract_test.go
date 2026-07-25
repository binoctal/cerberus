package scout

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/contract"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

// contractToolCalls presets the six contract tools the migrated
// BuildCoverageContract expects: scope, path_types, error_scope, boundaries,
// priority, coverage_gate. set_priority's schema forces map[string][]string,
// so the Priorities.UnmarshalJSON dual-shape drift patch is gone.
func contractToolCalls() []llm.ToolCall {
	return []llm.ToolCall{
		{Name: "declare_scope", Input: map[string]any{"modules": []any{"internal/llm"}}},
		{Name: "declare_path_types", Input: map[string]any{"types": []any{"happy", "alternative"}}},
		{Name: "declare_error_scope", Input: map[string]any{"scopes": []any{"4xx"}}},
		{Name: "declare_boundaries", Input: map[string]any{"boundaries": []any{"empty"}}},
		{Name: "set_priority", Input: map[string]any{"bucket": "high", "modules": []any{"internal/llm"}}},
		{Name: "set_coverage_gate", Input: map[string]any{"module": "internal/llm", "line_threshold": float64(0.65)}},
	}
}

func TestBuildCoverageContract(t *testing.T) {
	mock := llm.NewMockClient(nil)
	mock.SetToolResponse("test internal/llm", contractToolCalls())
	driver := ai.NewDriver(mock, ai.NewTokenBudget(10000, 1000))
	s := NewScout(driver, setupTestStore(t), &project.Config{}, zap.NewNop())

	c, err := s.BuildCoverageContract(context.Background(), "test internal/llm", &project.ProjectModel{}, contract.DepthStandard)
	require.NoError(t, err)
	assert.Equal(t, "standard", c.Depth)
	assert.Contains(t, c.Scope, "internal/llm")
	assert.Equal(t, 0.65, c.CoverageGate.LineThreshold)
	assert.Equal(t, []string{"internal/llm"}, c.Priorities["high"])
}

// TestBuildCoverageContract_FencedRealisticLLM guards against the dogfood
// regression: a real LLM wraps prose around tool calls. Under the tool-calling
// migration this is no longer a parsing concern — tool calls ride on
// Response.ToolCalls, not Content — but the case remains a useful fixture for
// the bucket-style priorities shape {bucket: [modules]} that the old JSON path
// struggled with.
func TestBuildCoverageContract_FencedRealisticLLM(t *testing.T) {
	mock := llm.NewMockClient(nil)
	mock.SetToolResponse("dogfood cerberus", []llm.ToolCall{
		{Name: "declare_scope", Input: map[string]any{"modules": []any{"go/build", "go/vet"}}},
		{Name: "declare_path_types", Input: map[string]any{"types": []any{"happy", "alternative"}}},
		{Name: "declare_error_scope", Input: map[string]any{"scopes": []any{"4xx", "validation"}}},
		{Name: "declare_boundaries", Input: map[string]any{"boundaries": []any{"empty", "zero", "max"}}},
		{Name: "set_priority", Input: map[string]any{"bucket": "critical", "modules": []any{"go/build"}}},
		{Name: "set_priority", Input: map[string]any{"bucket": "high", "modules": []any{"go/vet"}}},
		{Name: "set_coverage_gate", Input: map[string]any{"module": "cerberus", "line_threshold": float64(0.5)}},
	})
	driver := ai.NewDriver(mock, ai.NewTokenBudget(10000, 1000))
	s := NewScout(driver, setupTestStore(t), &project.Config{}, zap.NewNop())

	c, err := s.BuildCoverageContract(context.Background(), "dogfood cerberus", &project.ProjectModel{}, contract.DepthStandard)
	require.NoError(t, err, "must assemble tool calls with bucket-style priorities (real LLM shape)")
	assert.Equal(t, "standard", c.Depth)
	assert.Equal(t, []string{"go/build", "go/vet"}, c.Scope)
	assert.Equal(t, []string{"go/vet"}, c.Priorities["high"])
	assert.Equal(t, []string{"go/build"}, c.Priorities["critical"])
}

func TestSelfAssessContract(t *testing.T) {
	mock := llm.NewMockClient(nil)
	mock.SetToolResponse("Contract: ", []llm.ToolCall{
		{Name: "report_contract_gap", Input: map[string]any{"note": "missing error handling for 5xx"}},
		{Name: "report_contract_gap", Input: map[string]any{"note": "scope omits internal/session"}},
	})
	driver := ai.NewDriver(mock, ai.NewTokenBudget(10000, 1000))
	s := NewScout(driver, setupTestStore(t), &project.Config{}, zap.NewNop())

	c := &contract.Contract{Depth: "standard", Scope: []string{"internal/llm"}}
	notes, err := s.SelfAssessContract(context.Background(), c)
	require.NoError(t, err)
	assert.NotEmpty(t, notes)
}
