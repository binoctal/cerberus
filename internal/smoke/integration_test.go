package smoke

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/binoctal/cerberus/internal/head/scout"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestEndToEnd_FullPipeline tests the complete Scout→Agent→Examiner flow
// with a multi-endpoint httptest server.
func TestEndToEnd_FullPipeline(t *testing.T) {
	// SUT: multi-endpoint HTTP server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/health":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
		case r.URL.Path == "/api/v1/users" && r.Method == "GET":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{"users": []map[string]string{{"id": "1", "name": "Alice"}}})
		case r.URL.Path == "/api/v1/users" && r.Method == "POST":
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{"id": "2", "name": "Bob"})
		case r.URL.Path == "/api/v1/users/1":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"id": "1", "name": "Alice"})
		case r.URL.Path == "/api/v1/posts":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{"posts": []string{}})
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"not found"}`))
		}
	}))
	defer srv.Close()

	// Store + migrations.
	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer s.Close()
	ctx := context.Background()
	err = store.RunMigrations(ctx, s.DB(), "../../migrations")
	require.NoError(t, err)

	// Seed strategies.
	count, _ := store.SeedStrategies(ctx, s, "integration-test", zap.NewNop())
	assert.Greater(t, count, 0, "should seed strategies")

	// Mock LLM: return appropriate responses for each phase.
	// Scout Plan: return test cases for each endpoint.
	planOutput := scout.PlanOutput{
		Cases: []scout.CaseInfo{
			{ID: "tc-001", Name: "GET /health returns 200", Target: "/health", Method: "GET", Expectation: "200", Priority: 0.9},
			{ID: "tc-002", Name: "GET /api/v1/users returns list", Target: "/api/v1/users", Method: "GET", Expectation: "200", Priority: 0.9},
			{ID: "tc-003", Name: "POST /api/v1/users creates user", Target: "/api/v1/users", Method: "POST", Expectation: "201", Priority: 0.8},
			{ID: "tc-004", Name: "GET /api/v1/posts returns list", Target: "/api/v1/posts", Method: "GET", Expectation: "200", Priority: 0.7},
		},
	}
	planJSON, _ := json.Marshal(planOutput)

	// Examiner Judge: return pass verdicts.
	judgeJSON, _ := json.Marshal(examiner.JudgeResult{
		Status:               examiner.StatusPass,
		CorrectnessConfidence: 0.9,
		Reasoning:            "response matches expected",
	})

	// Use a single mock response — works because MockClient returns same for all calls.
	// Plan phase parses as PlanOutput, Judge phase parses as JudgeResult.
	mockClient := llm.NewMockClient(map[string]string{"default": string(planJSON)})
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(500000, 50000))

	cfg := &project.Config{
		Project: project.ProjectMeta{Name: "integration-test"},
		Services: []project.Service{
			{Name: "api", URL: srv.URL, Health: "/health"},
		},
	}

	// Phase 1: Scout — Analyze.
	scoutHead := scout.NewScout(driver, s, cfg, zap.NewNop())
	model, err := scoutHead.Analyze(ctx, scout.TargetInfo{URL: srv.URL, Goal: "test all APIs"})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(model.API.Endpoints), 1, "should have at least the health endpoint")

	// Phase 2: Scout — Plan.
	plan, err := scoutHead.Plan(ctx, "test all APIs", model)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(plan.Cases), 1, "should generate test cases")

	// Phase 3: Agent — Execute.
	sess, err := s.CreateSession(ctx, "run", "test all APIs", "integration-test")
	require.NoError(t, err)

	engine := agent.NewRuleEngine(srv.URL, nil)
	httpExec := agent.NewHTTPActionExecutor(srv.URL, zap.NewNop())
	loop := agent.NewReActLoop(driver, s, engine, httpExec, agent.DefaultReActConfig(), zap.NewNop())

	results, err := loop.ExecutePlan(ctx, plan, sess.ID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1, "should have step results")

	// Most results should pass (rule engine handles known endpoints).
	passed := 0
	for _, r := range results {
		if r.Status == agent.StepPassed {
			passed++
		}
	}
	assert.Greater(t, passed, 0, "at least some tests should pass")

	// Phase 4: Examiner — Judge + Learn.
	// Create a new mock client that returns judge-compatible responses.
	judgeClient := llm.NewMockClient(map[string]string{"default": string(judgeJSON)})
	judgeDriver := ai.NewDriver(judgeClient, ai.NewTokenBudget(500000, 50000))

	examinerCfg := examiner.DefaultExaminerConfig()
	examinerHead := examiner.NewExaminer(judgeDriver, nil, s, examinerCfg, zap.NewNop())
	verdicts, reflections, err := examinerHead.Examine(ctx, results, sess.ID, "integration-test")
	require.NoError(t, err)
	assert.Equal(t, len(results), len(verdicts), "should have verdict for each result")
	assert.GreaterOrEqual(t, reflections, 0, "may store reflections")

	// Verify traces exist.
	traces, err := s.GetTraces(ctx, sess.ID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(traces), 1, "should have traces")
}

// TestEndToEnd_ExaminerDegradation tests the Examiner's graceful degradation
// when the Judge LLM fails — falls back to step status.
func TestEndToEnd_ExaminerDegradation(t *testing.T) {
	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer s.Close()
	ctx := context.Background()
	err = store.RunMigrations(ctx, s.DB(), "../../migrations")
	require.NoError(t, err)

	sess, err := s.CreateSession(ctx, "run", "degradation test", "test")
	require.NoError(t, err)

	// Bad LLM: returns invalid JSON.
	badClient := llm.NewMockClient(map[string]string{"default": "not valid json"})
	badDriver := ai.NewDriver(badClient, ai.NewTokenBudget(500000, 50000))

	results := []agent.StepResult{
		{TestCase: &agent.TestCase{ID: "tc-001"}, Status: agent.StepPassed, LastObs: agent.Observation{StatusCode: 200}},
		{TestCase: &agent.TestCase{ID: "tc-002"}, Status: agent.StepFailed, LastObs: agent.Observation{StatusCode: 500}},
	}

	examinerCfg := examiner.DefaultExaminerConfig()
	ex := examiner.NewExaminer(badDriver, nil, s, examinerCfg, zap.NewNop())
	verdicts, _, err := ex.Examine(ctx, results, sess.ID, "test")
	require.NoError(t, err)
	require.Len(t, verdicts, 2)

	// First: pass (HTTP 200, degraded from judge failure).
	assert.Equal(t, examiner.StatusPass, verdicts[0].Status)
	// Second: fail (HTTP 500, degraded from judge failure).
	assert.Equal(t, examiner.StatusFail, verdicts[1].Status)
}

// TestEndToEnd_ScoutFallback tests that Scout produces a valid plan
// even when the LLM returns garbage.
func TestEndToEnd_ScoutFallback(t *testing.T) {
	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer s.Close()
	ctx := context.Background()
	err = store.RunMigrations(ctx, s.DB(), "../../migrations")
	require.NoError(t, err)

	badClient := llm.NewMockClient(map[string]string{"default": "garbage"})
	driver := ai.NewDriver(badClient, ai.NewTokenBudget(500000, 50000))

	cfg := &project.Config{
		Project: project.ProjectMeta{Name: "fallback-test"},
		Services: []project.Service{
			{Name: "api", URL: "http://localhost:8080", Health: "/health"},
			{Name: "web", URL: "http://localhost:3000", Health: "/"},
		},
	}

	scoutHead := scout.NewScout(driver, s, cfg, zap.NewNop())

	// Analyze should work from config only (no AI needed).
	model, err := scoutHead.Analyze(ctx, scout.TargetInfo{URL: "http://localhost:8080", Goal: "test"})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(model.API.Endpoints), 2, "should have health endpoints from config")

	// Plan should fall back to deterministic plan.
	plan, err := scoutHead.Plan(ctx, "test everything", model)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(plan.Cases), 2, "should have cases for each endpoint")
}
