package scout

import (
	"context"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/ai"
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

	plan, _ := assemblePlan(calls, "test goal", "http://localhost:8081", services)

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
