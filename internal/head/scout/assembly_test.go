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

func TestAssemblePlan_WSRelaySequence(t *testing.T) {
	calls := []llm.ToolCall{
		{Name: "begin_case", Input: map[string]any{"name": "relay", "expectation": "bridge gets signal", "service": "ws"}},
		{Name: "ws_connect", Input: map[string]any{"role": "web"}},
		{Name: "ws_connect", Input: map[string]any{"role": "bridge"}},
		{Name: "ws_send", Input: map[string]any{"role": "web", "type": "ping"}},
		{Name: "ws_receive", Input: map[string]any{"role": "bridge", "type": "signal", "assert": map[string]any{"online": true}}},
	}
	plan, covered := assemblePlan(calls, "g", "", nil)
	require.Len(t, plan.Cases, 1)
	c := plan.Cases[0]
	assert.Equal(t, "ws_flow", c.Action)
	require.Len(t, c.Steps, 4)
	assert.Equal(t, "ws_connect", c.Steps[0].Action)
	assert.Equal(t, "web", c.Steps[0].ConnectionID)
	assert.Equal(t, "ws_connect", c.Steps[1].Action)
	assert.Equal(t, "bridge", c.Steps[1].ConnectionID)
	assert.Equal(t, `{"type":"ping"}`, c.Steps[2].Message)
	assert.Equal(t, "signal", c.Steps[3].Type)
	assert.Equal(t, map[string]any{"online": true}, c.Steps[3].Asserts)
	// covered records connected roles per service
	assert.True(t, covered["ws"]["web"])
	assert.True(t, covered["ws"]["bridge"])
}

func TestAssemblePlan_WSDroppedWithoutBeginCase(t *testing.T) {
	// ws_* before any begin_case: dropped (no open group).
	calls := []llm.ToolCall{{Name: "ws_connect", Input: map[string]any{"role": "web"}}}
	plan, _ := assemblePlan(calls, "g", "", nil)
	assert.Empty(t, plan.Cases)
}
