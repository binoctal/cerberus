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
	plan, covered, _ := assemblePlan(calls, "goal", "http://x", nil)
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
	plan, _, _ := assemblePlan(calls, "g", "", svcs)
	assert.Equal(t, "api", plan.Cases[0].Service, "attributeService must override wrong LLM tag")
}

func TestAssemblePlan_RunProcessBuildOnly(t *testing.T) {
	calls := []llm.ToolCall{{
		Name:  "run_process",
		Input: map[string]any{"action": "build"},
	}}
	plan, _, _ := assemblePlan(calls, "g", "", nil)
	require.Len(t, plan.Cases, 1)
	assert.Equal(t, "process_build", plan.Cases[0].Action)
}

func TestAssemblePlan_AnalyzeCodeAllThree(t *testing.T) {
	for _, a := range []string{"analyze", "lint", "symbols"} {
		calls := []llm.ToolCall{{Name: "analyze_code", Input: map[string]any{"action": a}}}
		plan, _, _ := assemblePlan(calls, "g", "", nil)
		require.Len(t, plan.Cases, 1, "action=%s", a)
		assert.Equal(t, "code_"+a, plan.Cases[0].Action)
	}
}

func TestAssemblePlan_IDsSequential(t *testing.T) {
	calls := []llm.ToolCall{
		{Name: "navigate", Input: map[string]any{"path": "/"}},
		{Name: "check_invariant", Input: map[string]any{"description": "x", "assertion": "y"}},
	}
	plan, _, _ := assemblePlan(calls, "g", "", nil)
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
	// The signal receive is grounded by bridge's declared handshake await_type,
	// so the ws_flow is sound and marks its connected roles covered (the
	// contract is now sound coverage — A1 unsound-fallback Phase 1).
	svcs := []project.Service{{Name: "ws", Protocol: &project.Protocol{Roles: map[string]*project.ProtocolRole{
		"bridge": {Handshake: &project.RoleHandshake{AwaitType: "signal"}},
	}}}}
	plan, covered, _ := assemblePlan(calls, "g", "", svcs)
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
	plan, _, _ := assemblePlan(calls, "g", "", nil)
	assert.Empty(t, plan.Cases)
}

// TestAssemblePlan_DropsEmptyWSFlowCase asserts the defense side of ws_flow
// emission stability (spec 2026-07-27-ws-flow-emission-stability): a begin_case
// the LLM emitted with NO following ws_* calls must NOT become a 0-step ws_flow
// case — it is dropped, so it cannot waste an Agent cycle or confuse the
// Examiner. (GLM does this non-deterministically; run-2 of the 2026-07-26
// dogfood emitted exactly this.) Contrast TestAssemblePlan_WSRelaySequence,
// which has ws_* after begin_case and must still produce a populated case.
func TestAssemblePlan_DropsEmptyWSFlowCase(t *testing.T) {
	calls := []llm.ToolCall{
		{Name: "begin_case", Input: map[string]any{"name": "relay", "expectation": "bridge gets signal", "service": "ws"}},
		// no ws_* follows — the model opened a case and moved on
	}
	plan, covered, _ := assemblePlan(calls, "g", "", nil)
	assert.Empty(t, plan.Cases, "empty ws_flow case (begin_case with 0 ws_* steps) must be dropped")
	assert.Empty(t, covered, "an empty ws_flow case records no connected roles")
}

// TestAssemblePlan_WSFlowCaseTargetIsServiceURL asserts the execution-side
// companion of ws_flow emission stability: a ws_flow case (begin_case + ws_*)
// is executed by runSteps via stepToAction, which dials ws_connect at
// TestCase.Target. The LLM's begin_case carries the service NAME (not URL), so
// assembly must set Target to that service's URL — otherwise ws_connect dials
// "" (10µs fail, proto nil → unknown role). The 2026-07-27 dogfood surfaced
// this: emission stability made the LLM emit a complete ws_flow, but it could
// not execute because Target was empty.
func TestAssemblePlan_WSFlowCaseTargetIsServiceURL(t *testing.T) {
	svcs := []project.Service{{Name: "realtime", URL: "http://localhost:8989/ws/u1"}}
	calls := []llm.ToolCall{
		{Name: "begin_case", Input: map[string]any{"name": "relay", "expectation": "web gets signal", "service": "realtime"}},
		{Name: "ws_connect", Input: map[string]any{"role": "web"}},
	}
	plan, _, _ := assemblePlan(calls, "g", "", svcs)
	require.Len(t, plan.Cases, 1)
	assert.Equal(t, "http://localhost:8989/ws/u1", plan.Cases[0].Target,
		"ws_flow case Target must be the service URL so stepToAction's ws_connect can dial it")
}

// TestAssembleAnalyze_DeclareTechForcesStrings asserts the Analyze tool-calling
// migration: declare_tech's schema forces a string array, so assembleAnalyze
// produces []string directly — flexibleStrings drift absorption is gone.
func TestAssembleAnalyze_DeclareTechForcesStrings(t *testing.T) {
	calls := []llm.ToolCall{
		{Name: "report_endpoint", Input: map[string]any{"method": "GET", "path": "/api"}},
		{Name: "declare_tech", Input: map[string]any{"stack": []any{"go", "make"}}},
	}
	out := assembleAnalyze(calls)
	require.Len(t, out.Endpoints, 1)
	assert.Equal(t, "GET", out.Endpoints[0].Method)
	assert.Equal(t, []string{"go", "make"}, []string(out.TechStack))
}

// TestAssembleContract_PrioritiesForcedStringSlice asserts the contract
// tool-calling migration: set_priority's schema forces map[string][]string, so
// assembleContract produces []string directly — the Priorities.UnmarshalJSON
// dual-shape drift patch is gone.
func TestAssembleContract_PrioritiesForcedStringSlice(t *testing.T) {
	calls := []llm.ToolCall{
		{Name: "set_priority", Input: map[string]any{"bucket": "high", "modules": []any{"go/build"}}},
		{Name: "set_coverage_gate", Input: map[string]any{"module": "go/build", "line_threshold": float64(0.8)}},
		{Name: "declare_scope", Input: map[string]any{"modules": []any{"a", "b"}}},
	}
	c := assembleContract(calls, "standard", nil)
	assert.Equal(t, []string{"go/build"}, c.Priorities["high"])
	assert.Equal(t, "go/build", c.CoverageGate.Module)
	assert.Equal(t, []string{"a", "b"}, c.Scope)
	assert.Equal(t, "standard", c.Depth)
}
