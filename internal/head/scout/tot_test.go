package scout

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestToTPlanner_SingleStep(t *testing.T) {
	// MockClient returns proposals then evaluations.
	// Since MockClient returns same response for all calls, we need a response
	// that works for both propose and evaluate.
	// Propose expects ProposeOutput, evaluate expects EvaluateOutput.
	// We'll use a propose response — evaluate will fail and use fallback score.
	proposeOut := ProposeOutput{
		Strategies: []StrategyProposal{
			{Description: "Happy path tests", Cases: []string{"GET /api/v1/users returns 200", "POST /api/v1/users returns 201"}},
			{Description: "Error handling tests", Cases: []string{"GET /api/v1/users/999 returns 404", "POST /api/v1/users with invalid input returns 422"}},
		},
	}
	proposeJSON, _ := json.Marshal(proposeOut)

	mockClient := llm.NewMockClient(map[string]string{"default": string(proposeJSON)})
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(500000, 50000))

	cfg := ToTConfig{BeamWidth: 2, GenerateN: 2, MaxSteps: 1}
	planner := NewToTPlanner(driver, cfg, zap.NewNop())

	model := &project.ProjectModel{
		API: project.APIModel{
			Endpoints: []project.EndpointDef{
				{Method: "GET", Path: "/api/v1/users", Confidence: 0.95},
				{Method: "POST", Path: "/api/v1/users", Confidence: 0.95},
			},
		},
	}

	plan, err := planner.Plan(context.Background(), "test all endpoints", model, "http://localhost:8080")
	require.NoError(t, err)

	assert.Equal(t, "test all endpoints", plan.Goal)
	assert.Equal(t, "http://localhost:8080", plan.ProjectURL)
	assert.GreaterOrEqual(t, len(plan.Cases), 1) // Should have at least some cases
}

func TestToTPlanner_MultiStep(t *testing.T) {
	proposeOut := ProposeOutput{
		Strategies: []StrategyProposal{
			{Description: "Comprehensive API test", Cases: []string{"GET /api/v1/users returns list", "POST /api/v1/users creates user"}},
			{Description: "Edge case tests", Cases: []string{"GET /api/v1/users with pagination", "POST /api/v1/users duplicate email"}},
		},
	}
	proposeJSON, _ := json.Marshal(proposeOut)

	mockClient := llm.NewMockClient(map[string]string{"default": string(proposeJSON)})
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(500000, 50000))

	cfg := ToTConfig{BeamWidth: 1, GenerateN: 2, MaxSteps: 2}
	planner := NewToTPlanner(driver, cfg, zap.NewNop())

	model := &project.ProjectModel{
		API: project.APIModel{
			Endpoints: []project.EndpointDef{
				{Method: "GET", Path: "/api/v1/users", Confidence: 0.95},
			},
		},
	}

	plan, err := planner.Plan(context.Background(), "deep test", model, "http://localhost:8080")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(plan.Cases), 1)
}

func TestToTPlanner_ProposeFailure_StopsSearch(t *testing.T) {
	// Invalid JSON → propose fails → returns best current candidate.
	mockClient := llm.NewMockClient(map[string]string{"default": "not json"})
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(500000, 50000))

	cfg := ToTConfig{BeamWidth: 2, GenerateN: 2, MaxSteps: 3}
	planner := NewToTPlanner(driver, cfg, zap.NewNop())

	model := &project.ProjectModel{}

	plan, err := planner.Plan(context.Background(), "test goal", model, "http://localhost:8080")
	require.NoError(t, err)
	// Should return a plan even though AI failed — fallback to best candidate.
	assert.Equal(t, "test goal", plan.Goal)
}

func TestToTCoverageScore(t *testing.T) {
	planner := NewToTPlanner(nil, DefaultToTConfig(), zap.NewNop())

	model := &project.ProjectModel{
		API: project.APIModel{
			Endpoints: []project.EndpointDef{
				{Method: "GET", Path: "/api/v1/users", Confidence: 0.9},
				{Method: "POST", Path: "/api/v1/users", Confidence: 0.9},
				{Method: "GET", Path: "/api/v1/posts", Confidence: 0.9},
			},
		},
	}

	// Candidate mentioning "users" → covers 2/3 endpoints (both /users paths).
	c1 := &PlanCandidate{Cases: []string{"GET /api/v1/users", "POST /api/v1/users"}}
	score1 := planner.coverageScore(c1, model)
	assert.Greater(t, score1, 0.0)
	assert.LessOrEqual(t, score1, 1.0)

	// Candidate mentioning nothing → 0 coverage.
	c2 := &PlanCandidate{Cases: []string{"check homepage loads"}}
	score2 := planner.coverageScore(c2, model)
	assert.Less(t, score2, score1) // First should score higher.
}

func TestToTCoverageScore_NoEndpoints(t *testing.T) {
	planner := NewToTPlanner(nil, DefaultToTConfig(), zap.NewNop())
	model := &project.ProjectModel{}
	c := &PlanCandidate{Cases: []string{"test something"}}
	score := planner.coverageScore(c, model)
	assert.Equal(t, 0.5, score) // Default when no endpoints.
}

func TestBestToPlan(t *testing.T) {
	planner := NewToTPlanner(nil, DefaultToTConfig(), zap.NewNop())

	candidates := []PlanCandidate{
		{
			Description: "Best strategy",
			Cases:       []string{"GET /api/users returns 200", "POST /api/users creates user"},
			Score:       0.9,
		},
	}

	plan := planner.bestToPlan(candidates, "test goal", "http://localhost:8080")
	assert.Equal(t, "test goal", plan.Goal)
	assert.Equal(t, "http://localhost:8080", plan.ProjectURL)
	require.Len(t, plan.Cases, 2)
	assert.Equal(t, "tot-001", plan.Cases[0].ID)
	assert.Equal(t, "tot-002", plan.Cases[1].ID)
	assert.Contains(t, plan.Cases[0].Name, "GET /api/users")
}

func TestBestToPlan_EmptyCandidates(t *testing.T) {
	planner := NewToTPlanner(nil, DefaultToTConfig(), zap.NewNop())
	plan := planner.bestToPlan(nil, "goal", "http://localhost:8080")
	assert.Equal(t, "goal", plan.Goal)
	assert.Empty(t, plan.Cases)
}

func TestInferTarget(t *testing.T) {
	tests := []struct {
		desc     string
		expected string
	}{
		{"GET /api/v1/users returns 200", "get"},         // stops at first space after method
		{"POST /api/v1/users with invalid input", "post"}, // stops at first space after method
		{"Check that /api/v1/health is up", "/api/v1/health"},
		{"Verify the homepage loads", "verify the homepage loads"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, inferTarget(tt.desc))
	}
}

func TestTruncateName(t *testing.T) {
	assert.Equal(t, "short", truncateName("short", 80))
	long := "this is a very long name that should be truncated at some point because it exceeds the limit"
	truncated := truncateName(long, 80)
	assert.Equal(t, 80, len(truncated))
	assert.True(t, strings.HasSuffix(truncated, "..."))
}

func TestScout_DeepPlanMode(t *testing.T) {
	s := setupTestStore(t) // reuse helper from scout_test.go

	proposeOut := ProposeOutput{
		Strategies: []StrategyProposal{
			{Description: "API tests", Cases: []string{"GET /api/users returns 200"}},
		},
	}
	proposeJSON, _ := json.Marshal(proposeOut)

	mockClient := llm.NewMockClient(map[string]string{"default": string(proposeJSON)})
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(500000, 50000))

	cfg := &project.Config{
		Project: project.ProjectMeta{Name: "test"},
		Services: []project.Service{
			{Name: "api", URL: "http://localhost:8080", Health: "/health"},
		},
	}

	scoutHead := NewScout(driver, s, cfg, zap.NewNop())
	scoutHead.SetDeepPlan(ToTConfig{BeamWidth: 1, GenerateN: 1, MaxSteps: 1})

	model := &project.ProjectModel{
		API: project.APIModel{
			Endpoints: []project.EndpointDef{
				{Method: "GET", Path: "/health", Confidence: 0.95},
			},
		},
	}

	plan, err := scoutHead.Plan(context.Background(), "deep test", model)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(plan.Cases), 1)
}

func TestScout_DeepPlanFlag_Integration(t *testing.T) {
	// Verify that Scout without DeepPlan uses direct planning,
	// and Scout with DeepPlan uses ToT.
	s := setupTestStore(t)

	planOutput := PlanOutput{
		Cases: []CaseInfo{
			{ID: "tc-001", Name: "Direct plan", Target: "/api", Method: "GET", Expectation: "200", Priority: 1.0},
		},
	}
	planJSON, _ := json.Marshal(planOutput)

	proposeOut := ProposeOutput{
		Strategies: []StrategyProposal{
			{Description: "ToT plan", Cases: []string{"GET /api returns 200"}},
		},
	}
	proposeJSON, _ := json.Marshal(proposeOut)

	// Direct mode: returns PlanOutput JSON.
	mockDirect := llm.NewMockClient(map[string]string{"default": string(planJSON)})
	driverDirect := ai.NewDriver(mockDirect, ai.NewTokenBudget(500000, 50000))

	cfg := &project.Config{
		Project: project.ProjectMeta{Name: "test"},
		Services: []project.Service{{Name: "api", URL: "http://localhost:8080"}},
	}

	scoutDirect := NewScout(driverDirect, s, cfg, zap.NewNop())
	plan, err := scoutDirect.Plan(context.Background(), "test", &project.ProjectModel{})
	require.NoError(t, err)
	assert.Equal(t, "tc-001", plan.Cases[0].ID) // Direct plan case ID.

	// Deep mode: returns ProposeOutput JSON (MockClient same response).
	mockDeep := llm.NewMockClient(map[string]string{"default": string(proposeJSON)})
	driverDeep := ai.NewDriver(mockDeep, ai.NewTokenBudget(500000, 50000))

	scoutDeep := NewScout(driverDeep, s, cfg, zap.NewNop())
	scoutDeep.SetDeepPlan(ToTConfig{BeamWidth: 1, GenerateN: 1, MaxSteps: 1})
	plan, err = scoutDeep.Plan(context.Background(), "test", &project.ProjectModel{})
	require.NoError(t, err)
	assert.Equal(t, "tot-001", plan.Cases[0].ID) // ToT plan case ID prefix.
}

