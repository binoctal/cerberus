package scout

import (
	"context"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

// planWithPrefix is a test helper that runs planning with configured services
// and returns the generated test cases.
func planWithPrefix(t *testing.T, services []project.Service, endpoints []string) []testCase {
	t.Helper()

	// Create a mock model with endpoints
	model := &project.ProjectModel{
		API: project.APIModel{
			Endpoints: make([]project.EndpointDef, 0, len(endpoints)),
		},
	}

	// Parse endpoints from "METHOD /path" format
	for _, ep := range endpoints {
		parts := strings.Fields(ep)
		if len(parts) != 2 {
			t.Fatalf("invalid endpoint format: %s", ep)
		}
		model.API.Endpoints = append(model.API.Endpoints, project.EndpointDef{
			Method: parts[0],
			Path:   parts[1],
		})
	}

	// Create a scout with the test configuration
	s := &Scout{
		config: &project.Config{
			Services: services,
		},
	}

	// Run fallback planning to generate test cases
	plan := s.fallbackPlan("test goal", model)

	// Convert agent.TestCase to testCase for testing
	var cases []testCase
	for _, tc := range plan.Cases {
		cases = append(cases, testCase{
			Name:     tc.Name,
			Target:   tc.Target,
			Method:   tc.Method,
			Service:  tc.Service,
			Priority: tc.Priority,
		})
	}

	return cases
}

// testCase is a simplified version of agent.TestCase for testing.
type testCase struct {
	Name     string
	Target   string
	Method   string
	Service  string
	Priority float64
}

// serviceOf finds a test case by its target path and returns its Service.
func serviceOf(cases []testCase, target string) string {
	for _, c := range cases {
		if c.Target == target {
			return c.Service
		}
	}
	return ""
}

// TestPlan_AttributesByPathPrefix verifies that endpoints are attributed
// to the correct service based on PathPrefix matching.
func TestPlan_AttributesByPathPrefix(t *testing.T) {
	services := []project.Service{
		{Name: "gateway", URL: "http://gw", PathPrefix: []string{"/v1"}},
		{Name: "admin", URL: "http://admin", PathPrefix: []string{"/api/admin"}},
	}

	cases := planWithPrefix(t, services, []string{
		"POST /v1/chat/completions",
		"GET /api/admin/users",
	})

	require.Equal(t, "gateway", serviceOf(cases, "/v1/chat/completions"))
	require.Equal(t, "admin", serviceOf(cases, "/api/admin/users"))
}

// TestPlan_AttributesByPathPrefix_UnmatchedPath verifies that an endpoint
// matching NO service's PathPrefix yields Service == "".
func TestPlan_AttributesByPathPrefix_UnmatchedPath(t *testing.T) {
	services := []project.Service{
		{Name: "gateway", URL: "http://gw", PathPrefix: []string{"/v1"}},
		{Name: "admin", URL: "http://admin", PathPrefix: []string{"/api/admin"}},
	}

	cases := planWithPrefix(t, services, []string{
		"GET /unknown/path",
	})

	// Unmatched path should result in empty service
	require.Equal(t, "", serviceOf(cases, "/unknown/path"))
}

// TestPlan_AttributesByPathPrefix_NoPrefixConfigured verifies that when
// every service has an empty PathPrefix, all endpoints have Service == "".
func TestPlan_AttributesByPathPrefix_NoPrefixConfigured(t *testing.T) {
	services := []project.Service{
		{Name: "svc1", URL: "http://svc1", PathPrefix: []string{}},
		{Name: "svc2", URL: "http://svc2", PathPrefix: []string{}},
	}

	cases := planWithPrefix(t, services, []string{
		"GET /api/users",
		"POST /api/posts",
	})

	// All services should be empty when no PathPrefix is configured
	require.Equal(t, "", serviceOf(cases, "/api/users"))
	require.Equal(t, "", serviceOf(cases, "/api/posts"))
}

// TestAssemblePlan_BodyFromToolOrTemplate verifies the body-fill contract that
// TestConvertPlanOutput_BodyFromCaseInfoOrTemplate used to cover: a tool-emitted
// body is preserved verbatim; an empty body falls back to the attributed
// service's body_template. This is now enforced inside assemblePlan.
func TestAssemblePlan_BodyFromToolOrTemplate(t *testing.T) {
	services := []project.Service{
		{Name: "gateway", URL: "http://localhost:8081", PathPrefix: []string{"/v1"},
			BodyTemplate: `{"model":"default","messages":[]}`},
	}

	calls := []llm.ToolCall{
		{Name: "test_http_endpoint", Input: map[string]any{
			"method": "POST", "path": "/v1/chat/completions",
			"body": `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`,
		}},
		{Name: "test_http_endpoint", Input: map[string]any{
			"method": "POST", "path": "/v1/chat/completions",
		}}, // no body → falls back to template
	}

	plan, _, _, _ := assemblePlan(calls, "test goal", "http://localhost:8081", services)

	require.Equal(t, 2, len(plan.Cases))
	require.Equal(t, `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`, plan.Cases[0].Body)
	require.Equal(t, `{"model":"default","messages":[]}`, plan.Cases[1].Body)
}

// TestDirectPlan_ToolCallingAssembly asserts the migrated directPlan path:
// DecideWithTools returns tool calls, assemblePlan turns them into cases. The
// mock preset (one HTTP test + a begin_case/ws_* relay group) must yield at
// least 2 cases (http + ws_flow) — proving directPlan drives tool-assembled
// planning rather than JSON PlanOutput parsing.
func TestDirectPlan_ToolCallingAssembly(t *testing.T) {
	mock := llm.NewMockClient(nil)
	mock.SetToolResponse("plan http + ws relay", []llm.ToolCall{
		{Name: "test_http_endpoint", Input: map[string]any{"method": "GET", "path": "/health"}},
		{Name: "begin_case", Input: map[string]any{"name": "r", "expectation": "ok", "service": "ws"}},
		{Name: "ws_connect", Input: map[string]any{"role": "a"}},
		{Name: "ws_connect", Input: map[string]any{"role": "b"}},
		{Name: "ws_send", Input: map[string]any{"role": "a", "type": "x"}},
		{Name: "ws_receive", Input: map[string]any{"role": "b", "type": "y"}},
	})

	driver := ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000))
	sct := NewScout(driver, setupTestStore(t), &project.Config{
		Project: project.ProjectMeta{Name: "tool-plan"},
		Services: []project.Service{
			{Name: "api", URL: "http://localhost:8080"},
		},
	}, zap.NewNop())

	plan, err := sct.Plan(context.Background(), "plan http + ws relay", &project.ProjectModel{})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(plan.Cases), 2, "http case + ws_flow case (+ executor appends)")
}

// TestDirectPlan_LogsToolCallsAtDebug asserts runAIPlanning emits debug logs
// naming the tool calls received and the assembled case count, so a zero-case
// abort (the 2026-07-27 dogfood incident) is diagnosable from the run log.
func TestDirectPlan_LogsToolCallsAtDebug(t *testing.T) {
	mock := llm.NewMockClient(nil)
	mock.SetToolResponse("plan http + ws relay", []llm.ToolCall{
		{Name: "test_http_endpoint", Input: map[string]any{"method": "GET", "path": "/health"}},
		{Name: "begin_case", Input: map[string]any{"name": "r", "expectation": "ok", "service": "ws"}},
		{Name: "ws_connect", Input: map[string]any{"role": "a"}},
		{Name: "ws_connect", Input: map[string]any{"role": "b"}},
		{Name: "ws_send", Input: map[string]any{"role": "a", "type": "x"}},
		{Name: "ws_receive", Input: map[string]any{"role": "b", "type": "y"}},
	})
	driver := ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000))

	core, recorded := observer.New(zapcore.DebugLevel)
	sct := NewScout(driver, setupTestStore(t), &project.Config{
		Project:  project.ProjectMeta{Name: "log-plan"},
		Services: []project.Service{{Name: "api", URL: "http://localhost:8080"}},
	}, zap.New(core))

	_, err := sct.Plan(context.Background(), "plan http + ws relay", &project.ProjectModel{})
	require.NoError(t, err)

	recv := recorded.FilterMessage("scout planning tool calls received").All()
	require.Len(t, recv, 1, "tool-calls-received debug log should fire once")
	var tools string
	for _, f := range recv[0].Context {
		if f.Key == "tools" {
			tools = f.String
		}
	}
	require.Contains(t, tools, "test_http_endpoint")
	require.Contains(t, tools, "begin_case")
	require.GreaterOrEqual(t, recorded.FilterMessage("scout planning assembled").Len(), 1)
}

// TestDirectPlan_DebugLogsFilteredAtInfo asserts the planning debug logs are
// emitted at Debug level: an info-level observer captures none of them.
func TestDirectPlan_DebugLogsFilteredAtInfo(t *testing.T) {
	mock := llm.NewMockClient(nil)
	mock.SetToolResponse("plan http + ws relay", []llm.ToolCall{
		{Name: "test_http_endpoint", Input: map[string]any{"method": "GET", "path": "/health"}},
		{Name: "begin_case", Input: map[string]any{"name": "r", "expectation": "ok", "service": "ws"}},
		{Name: "ws_connect", Input: map[string]any{"role": "a"}},
		{Name: "ws_connect", Input: map[string]any{"role": "b"}},
		{Name: "ws_send", Input: map[string]any{"role": "a", "type": "x"}},
		{Name: "ws_receive", Input: map[string]any{"role": "b", "type": "y"}},
	})
	driver := ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000))

	core, recorded := observer.New(zapcore.InfoLevel) // captures Info+, drops Debug
	sct := NewScout(driver, setupTestStore(t), &project.Config{
		Project:  project.ProjectMeta{Name: "log-plan-info"},
		Services: []project.Service{{Name: "api", URL: "http://localhost:8080"}},
	}, zap.New(core))

	_, err := sct.Plan(context.Background(), "plan http + ws relay", &project.ProjectModel{})
	require.NoError(t, err)

	require.Equal(t, 0, recorded.FilterMessage("scout planning tool calls received").Len(),
		"debug log must not appear at info level")
}

// errorMockClient is a minimal llm.Client mock that always returns an error.
// Shared with scout_test.go to drive Scout.Plan into its deterministic fallback.
type errorMockClient struct {
	err error
}

func (m *errorMockClient) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	return nil, m.err
}

func (m *errorMockClient) CompleteWithVision(ctx context.Context, prompt string, images [][]byte) (*llm.Response, error) {
	return nil, m.err
}

func (m *errorMockClient) Stream(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
	return nil, m.err
}

// wsRelayConfig is a minimal config whose declared protocol makes
// WSCasesCovered emit the deterministic peer-join relay case (web has an
// OPTIONAL handshake awaiting a signal; bridge is the peer). Used by the
// zero-case fallback tests. The URL value is irrelevant — cases are not
// executed in these tests.
func wsRelayConfig() *project.Config {
	return &project.Config{
		Project: project.ProjectMeta{Name: "relay"},
		Services: []project.Service{{
			Name: "realtime",
			URL:  "http://localhost:8989/u",
			Protocol: &project.Protocol{
				TypePath: "type",
				Roles: map[string]*project.ProtocolRole{
					"web": {
						Params:    map[string]string{"type": "web"},
						Handshake: &project.RoleHandshake{AwaitType: "device:online", Optional: true, Timeout: 2},
					},
					"bridge": {Params: map[string]string{"type": "bridge"}},
				},
			},
		}},
	}
}

// TestPlan_ZeroToolCalls_ProceedsToDeterministic asserts that when the LLM
// returns zero tool calls, Scout.Plan no longer aborts: it proceeds to
// deterministic augmentation and the protocol-derived relay case is generated.
// (Reproduces the 2026-07-27 dogfood zero-case scenario; now passes — zero tool calls
// proceed to deterministic augmentation instead of aborting.)
func TestPlan_ZeroToolCalls_ProceedsToDeterministic(t *testing.T) {
	mock := llm.NewMockClient(nil)
	mock.SetToolResponse("zero tool calls relay goal", []llm.ToolCall{}) // zero tool calls
	driver := ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000))
	sct := NewScout(driver, setupTestStore(t), wsRelayConfig(), zap.NewNop())

	plan, err := sct.Plan(context.Background(), "zero tool calls relay goal", &project.ProjectModel{})
	require.NoError(t, err, "zero LLM tool calls must not abort when deterministic cases apply")
	var hasRelay bool
	for _, c := range plan.Cases {
		if c.ID == "ws-realtime-relay-web-signal-device-online" {
			hasRelay = true
		}
	}
	require.True(t, hasRelay, "expected the deterministic relay case; got case IDs: %v", caseIDStrings(plan.Cases))
}

// TestPlan_ZeroAssembled_ProceedsToDeterministic: the LLM returns a bare
// begin_case with no ws_* follow-ups, which assembly drops (empty ws_flow) →
// zero assembled cases. The plan must still proceed to deterministic cases.
func TestPlan_ZeroAssembled_ProceedsToDeterministic(t *testing.T) {
	mock := llm.NewMockClient(nil)
	mock.SetToolResponse("zero assembled relay goal", []llm.ToolCall{
		{Name: "begin_case", Input: map[string]any{"name": "x", "expectation": "ok", "service": "realtime"}},
		// no ws_* follows → assembly drops the empty ws_flow case → 0 assembled cases
	})
	driver := ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000))
	sct := NewScout(driver, setupTestStore(t), wsRelayConfig(), zap.NewNop())

	plan, err := sct.Plan(context.Background(), "zero assembled relay goal", &project.ProjectModel{})
	require.NoError(t, err)
	require.NotEmpty(t, plan.Cases, "deterministic relay case should survive a zero-assembled LLM round")
}

// TestPlan_NoCasesAtAll_ReturnsEmpty: zero LLM tool calls + no protocol + a
// non-code root (so GenerateExecutorCases is also empty) → the augmented plan
// is empty → Scout.Plan returns the empty plan, nil (graceful completion; the
// session layer treats an empty plan as a successful no-op run).
func TestPlan_NoCasesAtAll_ReturnsEmpty(t *testing.T) {
	mock := llm.NewMockClient(nil)
	mock.SetToolResponse("nothing applies goal", []llm.ToolCall{})
	driver := ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000))
	cfg := &project.Config{
		Project: project.ProjectMeta{Name: "empty"},
		Code:    project.CodeConfig{Root: t.TempDir()}, // non-code dir → no executor cases
	}
	sct := NewScout(driver, setupTestStore(t), cfg, zap.NewNop())

	plan, err := sct.Plan(context.Background(), "nothing applies goal", &project.ProjectModel{})
	require.NoError(t, err)
	require.Empty(t, plan.Cases, "no protocol + non-code root → empty plan, returned gracefully (not an error)")
}

// caseIDStrings returns case IDs for assertion-failure messages.
func caseIDStrings(cases []agent.TestCase) []string {
	ids := make([]string, len(cases))
	for i, c := range cases {
		ids[i] = c.ID
	}
	return ids
}

// TestDowngradeUnmodeledHTTPProbes: planner-invented paths (absent from the
// project model) become deterministic any-class reachability probes — the
// tc-00x noise family cannot fail on invented expectations anymore — while
// modeled paths and ws choreography pass through untouched.
func TestDowngradeUnmodeledHTTPProbes(t *testing.T) {
	model := &project.ProjectModel{API: project.APIModel{Endpoints: []project.EndpointDef{
		{Path: "/api/missions", Method: "POST"},
		{Path: "/health", Method: "GET"},
	}}}
	plan := &agent.TestPlan{Cases: []agent.TestCase{
		{ID: "tc-1", Target: "/health/live", Method: "GET", Expectation: "status 200"},      // invented
		{ID: "tc-2", Target: "/readyz", Method: "GET", Expectation: "Returns 2xx"},          // invented
		{ID: "tc-3", Target: "/api/missions", Method: "POST", Expectation: "status 201"},    // modeled
		{ID: "tc-4", Action: "ws_flow", Target: "ws://x", Expectation: "relay"},             // ws untouched
		{ID: "tc-5", Target: "http://elsewhere/x", Method: "GET", Expectation: "status 200"}, // absolute URL untouched
		{ID: "tc-6", Target: "/api/missions", Method: "GET", Steps: []agent.TestStep{{Action: "http_request"}}, Expectation: "already stepped"}, // stepped untouched
	}}

	downgradeUnmodeledHTTPProbes(plan, model, "http://localhost:8989")

	byID := map[string]agent.TestCase{}
	for _, c := range plan.Cases {
		byID[c.ID] = c
	}

	for _, id := range []string{"tc-1", "tc-2"} {
		c := byID[id]
		if c.Action != "ws_flow" || len(c.Steps) != 1 {
			t.Fatalf("%s not downgraded: %+v", id, c)
		}
		st := c.Steps[0]
		if st.Action != "http_request" || st.ExpectStatusClass != "any" {
			t.Fatalf("%s step = %+v, want http_request any-class", id, st)
		}
		if st.URL != "http://localhost:8989"+c.Target || st.Method != "GET" {
			t.Fatalf("%s step url/method wrong: %+v", id, st)
		}
		if c.Body != "" || c.Expectation == "status 200" || c.Expectation == "Returns 2xx" {
			t.Fatalf("%s expectation/body not rewritten: %+v", id, c)
		}
	}
	if c := byID["tc-3"]; len(c.Steps) != 0 || c.Expectation != "status 201" {
		t.Fatalf("modeled path must pass through untouched: %+v", c)
	}
	if c := byID["tc-4"]; c.Action != "ws_flow" || len(c.Steps) != 0 || c.Expectation != "relay" {
		t.Fatalf("ws choreography must pass through untouched: %+v", c)
	}
	if c := byID["tc-5"]; len(c.Steps) != 0 {
		t.Fatalf("absolute-URL case must pass through untouched: %+v", c)
	}
	if c := byID["tc-6"]; len(c.Steps) != 1 || c.Expectation != "already stepped" {
		t.Fatalf("already-stepped case must pass through untouched: %+v", c)
	}

	downgradeUnmodeledHTTPProbes(nil, model, "x")   // nil-safe
	downgradeUnmodeledHTTPProbes(plan, nil, "x")    // nil model safe
}

// Placeholder-bearing invented paths are dropped, not downgraded (dogfood
// 2026-08-23 run 8: tc-002/tc-003 died on unresolved {{bridge.userId}}).
func TestDowngradeUnmodeledHTTPProbes_DropsPlaceholderPaths(t *testing.T) {
	model := &project.ProjectModel{API: project.APIModel{Endpoints: []project.EndpointDef{
		{Path: "/api/x", Method: "GET"},
	}}}
	plan := &agent.TestPlan{Cases: []agent.TestCase{
		{ID: "tc-keep", Target: "/invented", Method: "GET", Expectation: "status 200"},
		{ID: "tc-drop", Target: "/ws/{{bridge.userId}}", Method: "GET", Expectation: "status 200"},
	}}
	dropped := downgradeUnmodeledHTTPProbes(plan, model, "http://h")
	require.Len(t, dropped, 1)
	assert.Equal(t, "tc-drop", dropped[0].ID)
	require.Len(t, plan.Cases, 1)
	assert.Equal(t, "tc-keep", plan.Cases[0].ID)
	assert.Len(t, plan.Cases[0].Steps, 1, "plain invented path still downgraded")
}
