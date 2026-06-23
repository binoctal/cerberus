package scout

import (
	"strings"
	"testing"

	"github.com/binoctal/cerberus/internal/project"
	"github.com/stretchr/testify/require"
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
