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

func TestBuildCoverageContract(t *testing.T) {
	// mock LLM returns a valid contract JSON
	mock := llm.NewMockClient(map[string]string{
		"default": `{"depth":"standard","scope":["internal/llm"],"path_types":["happy","alternative"],"error_scope":["4xx"],"boundaries":["empty"],"priorities":{"internal/llm":"high"},"coverage_gate":{"module":"internal/llm","line_threshold":0.65}}`,
	})
	driver := ai.NewDriver(mock, ai.NewTokenBudget(10000, 1000))
	s := NewScout(driver, setupTestStore(t), &project.Config{}, zap.NewNop())

	c, err := s.BuildCoverageContract(context.Background(), "test internal/llm", &project.ProjectModel{}, contract.DepthStandard)
	require.NoError(t, err)
	assert.Equal(t, "standard", c.Depth)
	assert.Contains(t, c.Scope, "internal/llm")
	assert.Equal(t, 0.65, c.CoverageGate.LineThreshold)
}

func TestSelfAssessContract(t *testing.T) {
	mock := llm.NewMockClient(map[string]string{"default": `{"notes":["missing error handling for 5xx","scope omits internal/session"]}`})
	driver := ai.NewDriver(mock, ai.NewTokenBudget(10000, 1000))
	s := NewScout(driver, setupTestStore(t), &project.Config{}, zap.NewNop())

	c := &contract.Contract{Depth: "standard", Scope: []string{"internal/llm"}}
	notes, err := s.SelfAssessContract(context.Background(), c)
	require.NoError(t, err)
	assert.NotEmpty(t, notes)
}
