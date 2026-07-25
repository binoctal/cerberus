package scout

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

func TestAssemblePlan_HighLevelHTTP(t *testing.T) {
	calls := []llm.ToolCall{{
		Name:  "test_http_endpoint",
		Input: map[string]any{"method": "GET", "path": "/api/users", "expect_status": float64(200)},
	}}
	plan, covered := assemblePlan(calls, "goal", "http://x", nil)
	require.Len(t, plan.Cases, 1)
	c := plan.Cases[0]
	assert.Equal(t, "tc-001", c.ID)
	assert.Equal(t, "GET", c.Method)
	assert.Equal(t, "/api/users", c.Target)
	assert.Contains(t, c.Expectation, "200")
	assert.Empty(t, covered)
}

// Service attribution: attributeService override replaces verifyServiceAttribution.
func TestAssemblePlan_ServiceOverride(t *testing.T) {
	svcs := []project.Service{{Name: "api", PathPrefix: []string{"/api"}}}
	calls := []llm.ToolCall{{
		Name:  "test_http_endpoint",
		Input: map[string]any{"method": "GET", "path": "/api/users", "service": "wrong"},
	}}
	plan, _ := assemblePlan(calls, "g", "", svcs)
	assert.Equal(t, "api", plan.Cases[0].Service, "attributeService must override wrong LLM tag")
}

func TestAssemblePlan_RunProcessBuildOnly(t *testing.T) {
	calls := []llm.ToolCall{{
		Name:  "run_process",
		Input: map[string]any{"action": "build"},
	}}
	plan, _ := assemblePlan(calls, "g", "", nil)
	require.Len(t, plan.Cases, 1)
	assert.Equal(t, "process_build", plan.Cases[0].Action)
}

func TestAssemblePlan_AnalyzeCodeAllThree(t *testing.T) {
	for _, a := range []string{"analyze", "lint", "symbols"} {
		calls := []llm.ToolCall{{Name: "analyze_code", Input: map[string]any{"action": a}}}
		plan, _ := assemblePlan(calls, "g", "", nil)
		require.Len(t, plan.Cases, 1, "action=%s", a)
		assert.Equal(t, "code_"+a, plan.Cases[0].Action)
	}
}

func TestAssemblePlan_IDsSequential(t *testing.T) {
	calls := []llm.ToolCall{
		{Name: "navigate", Input: map[string]any{"path": "/"}},
		{Name: "check_invariant", Input: map[string]any{"description": "x", "assertion": "y"}},
	}
	plan, _ := assemblePlan(calls, "g", "", nil)
	require.Len(t, plan.Cases, 2)
	assert.Equal(t, "tc-001", plan.Cases[0].ID)
	assert.Equal(t, "tc-002", plan.Cases[1].ID)
}
