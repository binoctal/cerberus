package scout

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

func TestDeterministicScore_RanksCoveringCandidateHigher(t *testing.T) {
	model := &project.ProjectModel{API: project.APIModel{Endpoints: []project.EndpointDef{{Method: "GET", Path: "/users"}}}}
	model.InvariantHints = []project.InvariantHint{{ID: "inv1", Description: "users must be unique"}}
	high := &PlanCandidate{Cases: []string{"GET /users", "check inv1 users must be unique"}}
	low := &PlanCandidate{Cases: []string{"check something unrelated"}}
	planner := NewToTPlanner(nil, nil, DefaultToTConfig(), zap.NewNop())
	hs := planner.deterministicScore(high, model, "test users")
	ls := planner.deterministicScore(low, model, "test users")
	assert.Greater(t, hs, ls, "candidate covering endpoints+invariants must score higher")
}

func TestDeterministicScore_FloorTriggersFailSafe(t *testing.T) {
	// "x" matches no endpoint/invariant/page/goal token → score ≈0.06 < floor 0.10.
	model := &project.ProjectModel{API: project.APIModel{Endpoints: []project.EndpointDef{{Method: "GET", Path: "/users"}}}}
	model.InvariantHints = []project.InvariantHint{{ID: "inv1", Description: "users must be unique"}}
	planner := NewToTPlanner(nil, nil, DefaultToTConfig(), zap.NewNop())
	empty := []PlanCandidate{{Cases: []string{"x"}}}
	_, err := planner.evaluate(context.Background(), empty, model, "test users")
	assert.Error(t, err)
}

func TestToTPlanner_BuildProposeTask_MemoryInjected(t *testing.T) {
	planner := NewToTPlanner(nil, nil, DefaultToTConfig(), zap.NewNop())
	parent := PlanCandidate{Description: "cover auth endpoints"}
	model := &project.ProjectModel{}

	// Empty memory: task has no memory section (no regression for standalone).
	task := planner.buildProposeTask(parent, model, "test login flow")
	assert.NotContains(t, task, "Prior-session memory")

	// With memory: task prepends the episodic/semantic context.
	planner.SetMemory("LESSON: /login rate-limits after 5 attempts")
	task = planner.buildProposeTask(parent, model, "test login flow")
	assert.Contains(t, task, "Prior-session memory")
	assert.Contains(t, task, "LESSON: /login rate-limits after 5 attempts")
}

func TestToTPlanner_SingleStep(t *testing.T) {
	// MockClient returns propose_strategy tool calls; evaluate is deterministic
	// (no LLM). Each tool call becomes one PlanCandidate.
	mockClient := llm.NewMockClient(nil)
	mockClient.SetToolResponse("test all endpoints", []llm.ToolCall{
		{Name: "propose_strategy", Input: map[string]any{
			"description": "Happy path tests",
			"cases":       []any{"GET /api/v1/users returns 200", "POST /api/v1/users returns 201"},
		}},
		{Name: "propose_strategy", Input: map[string]any{
			"description": "Error handling tests",
			"cases":       []any{"GET /api/v1/users/999 returns 404", "POST /api/v1/users with invalid input returns 422"},
		}},
	})
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(500000, 50000))

	cfg := ToTConfig{BeamWidth: 2, GenerateN: 2, MaxSteps: 1}
	planner := NewToTPlanner(driver, driver, cfg, zap.NewNop())

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
	mockClient := llm.NewMockClient(nil)
	mockClient.SetToolResponse("deep test", []llm.ToolCall{
		{Name: "propose_strategy", Input: map[string]any{
			"description": "Comprehensive API test",
			"cases":       []any{"GET /api/v1/users returns list", "POST /api/v1/users creates user"},
		}},
		{Name: "propose_strategy", Input: map[string]any{
			"description": "Edge case tests",
			"cases":       []any{"GET /api/v1/users with pagination", "POST /api/v1/users duplicate email"},
		}},
	})
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(500000, 50000))

	cfg := ToTConfig{BeamWidth: 1, GenerateN: 2, MaxSteps: 2}
	planner := NewToTPlanner(driver, driver, cfg, zap.NewNop())

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
	// No tool calls (text-only "not json" response) → propose returns zero
	// candidates → Plan stops the search and returns the best current candidate.
	mockClient := llm.NewMockClient(map[string]string{"default": "not json"})
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(500000, 50000))

	cfg := ToTConfig{BeamWidth: 2, GenerateN: 2, MaxSteps: 3}
	planner := NewToTPlanner(driver, driver, cfg, zap.NewNop())

	model := &project.ProjectModel{}

	plan, err := planner.Plan(context.Background(), "test goal", model, "http://localhost:8080")
	require.NoError(t, err)
	// Should return a plan even though AI failed — fallback to best candidate.
	assert.Equal(t, "test goal", plan.Goal)
}

func TestToTCoverageScore(t *testing.T) {
	planner := NewToTPlanner(nil, nil, DefaultToTConfig(), zap.NewNop())

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
	planner := NewToTPlanner(nil, nil, DefaultToTConfig(), zap.NewNop())
	model := &project.ProjectModel{}
	c := &PlanCandidate{Cases: []string{"test something"}}
	score := planner.coverageScore(c, model)
	assert.Equal(t, 0.5, score) // Default when no endpoints.
}

func TestBestToPlan(t *testing.T) {
	planner := NewToTPlanner(nil, nil, DefaultToTConfig(), zap.NewNop())

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
	planner := NewToTPlanner(nil, nil, DefaultToTConfig(), zap.NewNop())
	plan := planner.bestToPlan(nil, "goal", "http://localhost:8080")
	assert.Equal(t, "goal", plan.Goal)
	assert.Empty(t, plan.Cases)
}

func TestInferTarget(t *testing.T) {
	tests := []struct {
		desc     string
		expected string
	}{
		{"GET /api/v1/users returns 200", "get"},          // stops at first space after method
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

	mockClient := llm.NewMockClient(nil)
	mockClient.SetToolResponse("deep test", []llm.ToolCall{
		{Name: "propose_strategy", Input: map[string]any{
			"description": "API tests",
			"cases":       []any{"GET /api/users returns 200"},
		}},
	})
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(500000, 50000))

	cfg := &project.Config{
		Project: project.ProjectMeta{Name: "test"},
		Services: []project.Service{
			{Name: "api", URL: "http://localhost:8080", Health: "/health"},
		},
	}

	scoutHead := NewScout(driver, s, cfg, zap.NewNop())
	scoutHead.SetDeepPlan(ToTConfig{BeamWidth: 1, GenerateN: 1, MaxSteps: 1}, nil, nil)

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
	// Verify that Scout without DeepPlan uses direct tool-use planning,
	// and Scout with DeepPlan uses ToT.
	s := setupTestStore(t)

	// Direct mode: DecideWithTools returns tool calls.
	mockDirect := llm.NewMockClient(nil)
	mockDirect.SetToolResponse("test", []llm.ToolCall{
		{Name: "test_http_endpoint", Input: map[string]any{"method": "GET", "path": "/api"}},
	})
	driverDirect := ai.NewDriver(mockDirect, ai.NewTokenBudget(500000, 50000))

	cfg := &project.Config{
		Project:  project.ProjectMeta{Name: "test"},
		Services: []project.Service{{Name: "api", URL: "http://localhost:8080"}},
	}

	scoutDirect := NewScout(driverDirect, s, cfg, zap.NewNop())
	plan, err := scoutDirect.Plan(context.Background(), "test", &project.ProjectModel{})
	require.NoError(t, err)
	assert.Equal(t, "tc-001", plan.Cases[0].ID) // Direct plan case ID.

	// Deep mode: ToT planner consumes propose_strategy tool calls.
	mockDeep := llm.NewMockClient(nil)
	mockDeep.SetToolResponse("test", []llm.ToolCall{
		{Name: "propose_strategy", Input: map[string]any{
			"description": "ToT plan",
			"cases":       []any{"GET /api returns 200"},
		}},
	})
	driverDeep := ai.NewDriver(mockDeep, ai.NewTokenBudget(500000, 50000))

	scoutDeep := NewScout(driverDeep, s, cfg, zap.NewNop())
	scoutDeep.SetDeepPlan(ToTConfig{BeamWidth: 1, GenerateN: 1, MaxSteps: 1}, nil, nil)
	plan, err = scoutDeep.Plan(context.Background(), "test", &project.ProjectModel{})
	require.NoError(t, err)
	assert.Equal(t, "tot-001", plan.Cases[0].ID) // ToT plan case ID prefix.
}
