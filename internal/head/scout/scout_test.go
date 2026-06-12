package scout

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })

	ctx := context.Background()
	err = store.RunMigrations(ctx, s.DB(), "../../../migrations")
	require.NoError(t, err)
	return s
}

func TestAnalyze_ConfigOnlyModel(t *testing.T) {
	// When config has enough data (health endpoints + invariants),
	// Analyze should return a model without calling AI.
	mockClient := llm.NewMockClient(nil) // No responses configured — AI should NOT be called.

	cfg := &project.Config{
		Project: project.ProjectMeta{Name: "test-project"},
		Services: []project.Service{
			{Name: "api", URL: "http://localhost:8080", Health: "/health"},
			{Name: "web", URL: "http://localhost:3000", Health: "/ready"},
		},
		Invariants: []project.Invariant{
			{ID: "inv-1", Description: "Users endpoint works", Check: "/api/users", Assertion: "returns 200"},
			{ID: "inv-2", Description: "Auth works", Check: "/auth/login", Assertion: "returns token"},
		},
	}

	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))
	s := setupTestStore(t)

	scout := NewScout(driver, s, cfg, zap.NewNop())
	model, err := scout.Analyze(context.Background(), TargetInfo{
		URL:  "http://localhost:8080",
		Goal: "verify all endpoints",
	})
	require.NoError(t, err)

	// Should have endpoints from health checks.
	assert.Len(t, model.API.Endpoints, 2) // /health + /ready
	assert.Len(t, model.InvariantHints, 2)

	// Verify endpoint data.
	assert.Equal(t, "GET", model.API.Endpoints[0].Method)
	assert.Equal(t, "/health", model.API.Endpoints[0].Path)
	assert.InDelta(t, 0.95, model.API.Endpoints[0].Confidence, 0.01)
}

func TestAnalyze_AIInference(t *testing.T) {
	// When config is sparse, AI should be called to infer additional endpoints.
	analyzeOutput := AnalyzeOutput{
		Endpoints: []EndpointInfo{
			{Method: "GET", Path: "/api/v1/users", Confidence: 0.8},
			{Method: "POST", Path: "/api/v1/users", Confidence: 0.7},
		},
		Pages: []PageInfo{
			{Path: "/dashboard", Confidence: 0.6},
		},
		TechStack: []string{"react", "node"},
	}
	analyzeJSON, _ := json.Marshal(analyzeOutput)

	mockClient := llm.NewMockClient(map[string]string{
		"default": string(analyzeJSON),
	})

	cfg := &project.Config{
		Project:  project.ProjectMeta{Name: "sparse-project"},
		Services: []project.Service{
			{Name: "api", URL: "http://localhost:8080"}, // No health endpoint → sparse model
		},
	}

	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))
	s := setupTestStore(t)

	scout := NewScout(driver, s, cfg, zap.NewNop())
	model, err := scout.Analyze(context.Background(), TargetInfo{
		URL:  "http://localhost:8080",
		Goal: "discover endpoints",
	})
	require.NoError(t, err)

	// AI-inferred endpoints should be merged.
	assert.Len(t, model.API.Endpoints, 2)
	assert.Equal(t, "/api/v1/users", model.API.Endpoints[0].Path)
	assert.Equal(t, "GET", model.API.Endpoints[0].Method)

	// AI-inferred pages.
	assert.Len(t, model.Navigation.Pages, 1)
	assert.Equal(t, "/dashboard", model.Navigation.Pages[0].Path)

	// Tech stack inferred.
	assert.Contains(t, model.TechStack, "react")
}

func TestAnalyze_AIErrorGracefulDegradation(t *testing.T) {
	// When AI call fails, should return config-only model without error.
	mockClient := llm.NewMockClient(map[string]string{
		"default": "not valid json {{{", // Will cause parse error.
	})

	cfg := &project.Config{
		Project: project.ProjectMeta{Name: "test"},
		Services: []project.Service{
			{Name: "api", URL: "http://localhost:8080"}, // No health → triggers AI
		},
	}

	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))
	s := setupTestStore(t)

	scout := NewScout(driver, s, cfg, zap.NewNop())
	model, err := scout.Analyze(context.Background(), TargetInfo{
		URL:  "http://localhost:8080",
		Goal: "test",
	})
	require.NoError(t, err)

	// Should gracefully degrade to empty model (no endpoints from config or AI).
	assert.Empty(t, model.API.Endpoints)
}

func TestPlan_AIPlanning(t *testing.T) {
	planOutput := PlanOutput{
		Cases: []CaseInfo{
			{ID: "tc-001", Name: "List users", Target: "/api/users", Method: "GET", Expectation: "Returns 200", Priority: 1.0},
			{ID: "tc-002", Name: "Create user", Target: "/api/users", Method: "POST", Expectation: "Returns 201", Priority: 0.9},
			{ID: "tc-003", Name: "Get dashboard", Target: "/dashboard", Action: "navigate", Expectation: "Loads without error", Priority: 0.7},
		},
	}
	planJSON, _ := json.Marshal(planOutput)

	mockClient := llm.NewMockClient(map[string]string{
		"default": string(planJSON),
	})

	cfg := &project.Config{
		Project: project.ProjectMeta{Name: "test"},
		Services: []project.Service{
			{Name: "api", URL: "http://localhost:8080"},
		},
	}

	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))
	s := setupTestStore(t)

	scout := NewScout(driver, s, cfg, zap.NewNop())

	model := &project.ProjectModel{
		API: project.APIModel{
			Endpoints: []project.EndpointDef{
				{Method: "GET", Path: "/api/users", Confidence: 0.95},
				{Method: "POST", Path: "/api/users", Confidence: 0.95},
			},
		},
		Navigation: project.NavigationModel{
			Pages: []project.PageDef{
				{Path: "/dashboard", Confidence: 0.7},
			},
		},
	}

	plan, err := scout.Plan(context.Background(), "verify API surface", model)
	require.NoError(t, err)

	assert.Equal(t, "verify API surface", plan.Goal)
	assert.Equal(t, "http://localhost:8080", plan.ProjectURL)
	require.Len(t, plan.Cases, 3)

	// Verify case conversion.
	assert.Equal(t, "tc-001", plan.Cases[0].ID)
	assert.Equal(t, "List users", plan.Cases[0].Name)
	assert.Equal(t, "/api/users", plan.Cases[0].Target)
	assert.Equal(t, "GET", plan.Cases[0].Method)
	assert.InDelta(t, 1.0, plan.Cases[0].Priority, 0.01)

	assert.Equal(t, "navigate", plan.Cases[2].Action)
}

func TestPlan_FallbackPlan(t *testing.T) {
	// When AI fails, should generate deterministic test cases from the model.
	mockClient := llm.NewMockClient(map[string]string{
		"default": "invalid json", // Will cause parse error.
	})

	cfg := &project.Config{
		Project: project.ProjectMeta{Name: "test"},
		Services: []project.Service{
			{Name: "api", URL: "http://localhost:8080"},
		},
	}

	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))
	s := setupTestStore(t)

	scout := NewScout(driver, s, cfg, zap.NewNop())

	model := &project.ProjectModel{
		API: project.APIModel{
			Endpoints: []project.EndpointDef{
				{Method: "GET", Path: "/api/users", Confidence: 0.95},
				{Method: "POST", Path: "/api/users", Confidence: 0.9},
			},
		},
		InvariantHints: []project.InvariantHint{
			{ID: "inv-1", Description: "Users must be listable", Confidence: 0.9},
		},
	}

	plan, err := scout.Plan(context.Background(), "test goal", model)
	require.NoError(t, err)

	assert.Equal(t, "test goal", plan.Goal)

	// Should have 2 endpoint cases + 1 invariant case.
	require.Len(t, plan.Cases, 3)

	// Endpoint cases.
	assert.Equal(t, "tc-001", plan.Cases[0].ID)
	assert.Contains(t, plan.Cases[0].Name, "GET /api/users")
	assert.Equal(t, "/api/users", plan.Cases[0].Target)
	assert.Equal(t, "GET", plan.Cases[0].Method)

	assert.Equal(t, "tc-002", plan.Cases[1].ID)
	assert.Contains(t, plan.Cases[1].Name, "POST /api/users")

	// Invariant case.
	assert.Equal(t, "inv-001", plan.Cases[2].ID)
	assert.Contains(t, plan.Cases[2].Name, "Invariant: inv-1")
}

func TestPlan_FallbackPlan_DefaultHealthCheck(t *testing.T) {
	// When model is empty and baseURL exists, should add default health check.
	// We need AI to fail to trigger fallback. Use invalid response.
	cfg := &project.Config{
		Project: project.ProjectMeta{Name: "test"},
		Services: []project.Service{
			{Name: "api", URL: "http://localhost:8080"},
		},
	}

	s := setupTestStore(t)

	// Empty model triggers fallbackPlan, and since no endpoints → default health check.
	scout2 := NewScout(
		ai.NewDriver(llm.NewMockClient(map[string]string{"default": "bad"}), ai.NewTokenBudget(200000, 10000)),
		s, cfg, zap.NewNop(),
	)

	plan, err := scout2.Plan(context.Background(), "test", &project.ProjectModel{})
	require.NoError(t, err)

	require.Len(t, plan.Cases, 1)
	assert.Equal(t, "default-health", plan.Cases[0].ID)
	assert.Equal(t, "/", plan.Cases[0].Target)
}

func TestMergeAIInference_NoDuplicates(t *testing.T) {
	cfg := &project.Config{
		Project: project.ProjectMeta{Name: "test"},
		Services: []project.Service{
			{Name: "api", URL: "http://localhost:8080", Health: "/health"},
		},
	}

	s := setupTestStore(t)
	scout := NewScout(nil, s, cfg, zap.NewNop())

	model := scout.buildModelFromConfig()
	require.Len(t, model.API.Endpoints, 1) // /health from config

	// AI returns /health again plus a new endpoint.
	scout.mergeAIInference(model, AnalyzeOutput{
		Endpoints: []EndpointInfo{
			{Method: "GET", Path: "/health", Confidence: 0.8},       // Duplicate
			{Method: "GET", Path: "/api/users", Confidence: 0.7},    // New
		},
		Pages: []PageInfo{
			{Path: "/dashboard", Confidence: 0.6},
		},
	})

	// Should not duplicate /health.
	assert.Len(t, model.API.Endpoints, 2)
	assert.Equal(t, "/health", model.API.Endpoints[0].Path)
	assert.Equal(t, "/api/users", model.API.Endpoints[1].Path)
}

func TestBuildAnalyzeContext(t *testing.T) {
	cfg := &project.Config{
		Project: project.ProjectMeta{Name: "my-app"},
		Services: []project.Service{
			{Name: "api", URL: "http://localhost:8080", Health: "/health"},
		},
		Invariants: []project.Invariant{
			{ID: "inv-1", Description: "Users endpoint works", Check: "/api/users", Assertion: "returns 200"},
		},
		Databases: []project.Database{
			{Name: "postgres"},
		},
	}

	s := setupTestStore(t)
	scout := NewScout(nil, s, cfg, zap.NewNop())

	ctx := scout.buildAnalyzeContext(TargetInfo{URL: "http://localhost:8080", Goal: "test"})

	assert.Contains(t, ctx, "Project: my-app")
	assert.Contains(t, ctx, "Base URL: http://localhost:8080")
	assert.Contains(t, ctx, "Services:")
	assert.Contains(t, ctx, "api: http://localhost:8080")
	assert.Contains(t, ctx, "Invariants:")
	assert.Contains(t, ctx, "inv-1")
	assert.Contains(t, ctx, "Databases:")
	assert.Contains(t, ctx, "postgres")
	assert.Contains(t, ctx, "Known endpoints:")
	assert.Contains(t, ctx, "/health")
}

func TestBuildPlanContext(t *testing.T) {
	cfg := &project.Config{
		Project: project.ProjectMeta{Name: "test"},
		Services: []project.Service{
			{Name: "api", URL: "http://localhost:8080"},
		},
	}

	s := setupTestStore(t)
	scout := NewScout(nil, s, cfg, zap.NewNop())

	model := &project.ProjectModel{
		API: project.APIModel{
			Endpoints: []project.EndpointDef{
				{Method: "GET", Path: "/api/users", Confidence: 0.95},
			},
		},
		Navigation: project.NavigationModel{
			Pages: []project.PageDef{
				{Path: "/dashboard", Confidence: 0.7},
			},
		},
		InvariantHints: []project.InvariantHint{
			{ID: "inv-1", Description: "Test invariant"},
		},
	}

	ctx := scout.buildPlanContext(model)

	assert.Contains(t, ctx, "API Endpoints:")
	assert.Contains(t, ctx, "GET /api/users")
	assert.Contains(t, ctx, "Pages:")
	assert.Contains(t, ctx, "/dashboard")
	assert.Contains(t, ctx, "Invariants:")
	assert.Contains(t, ctx, "inv-1")
	assert.Contains(t, ctx, "Raw Model")
}

func TestEndToEnd_AnalyzeThenPlan(t *testing.T) {
	// Full Scout pipeline: Analyze → Plan with real httptest server.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/users":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{"users": []string{}})
		case "/health":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	analyzeOutput := AnalyzeOutput{
		Endpoints: []EndpointInfo{
			{Method: "GET", Path: "/api/v1/users", Confidence: 0.85},
		},
	}
	analyzeJSON, _ := json.Marshal(analyzeOutput)

	planOutput := PlanOutput{
		Cases: []CaseInfo{
			{ID: "tc-001", Name: "Get users", Target: "/api/v1/users", Method: "GET", Expectation: "Returns 200", Priority: 1.0},
		},
	}
	planJSON, _ := json.Marshal(planOutput)

	// MockClient returns plan output. Analyze will skip AI because config has
	// enough coverage (health + invariant → InfoScore ≥ 0.7).
	mockClient := llm.NewMockClient(map[string]string{
		"default": string(planJSON),
	})

	cfg := &project.Config{
		Project: project.ProjectMeta{Name: "e2e-test"},
		Services: []project.Service{
			{Name: "api", URL: server.URL, Health: "/health"},
		},
		Invariants: []project.Invariant{
			{ID: "users-exist", Description: "Users endpoint accessible", Check: "/api/v1/users", Assertion: "returns 200"},
		},
	}

	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))
	s := setupTestStore(t)

	_ = analyzeJSON // Used in concept; actual Analyze skips AI due to config coverage.
	scout := NewScout(driver, s, cfg, zap.NewNop())

	// Analyze — should skip AI (config has health + invariant → InfoScore ≥ 0.7).
	model, err := scout.Analyze(context.Background(), TargetInfo{
		URL:  server.URL,
		Goal: "verify all endpoints",
	})
	require.NoError(t, err)
	assert.Len(t, model.API.Endpoints, 1) // /health
	assert.Len(t, model.InvariantHints, 1)

	// Plan — should call AI.
	plan, err := scout.Plan(context.Background(), "verify all endpoints", model)
	require.NoError(t, err)

	assert.Equal(t, server.URL, plan.ProjectURL)
	require.Len(t, plan.Cases, 1)
	assert.Equal(t, "tc-001", plan.Cases[0].ID)

	// Verify the plan can be executed by Agent head.
	// Create a real session for foreign key constraints.
	dbSess, err := s.CreateSession(context.Background(), "run", "e2e scout test", "e2e-test")
	require.NoError(t, err)

	engine := agent.NewRuleEngine(server.URL, nil)
	exec := agent.BuildMultiExecutor(".", nil, zap.NewNop())
	loop := agent.NewReActLoop(driver, s, engine, exec, agent.DefaultReActConfig(), zap.NewNop())

	results, err := loop.ExecutePlan(context.Background(), plan, dbSess.ID)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, agent.StepPassed, results[0].Status)
}
