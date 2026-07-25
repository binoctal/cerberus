package smoke

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/binoctal/cerberus/internal/head/scout"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/binoctal/cerberus/internal/types"
)

// TestEndToEnd_FullPipeline tests the complete Scout→Agent→Examiner flow
// with a multi-endpoint httptest server.
func TestEndToEnd_FullPipeline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case r.URL.Path == "/api/v1/users" && r.Method == "GET":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"users": []map[string]string{{"id": "1", "name": "Alice"}}})
		case r.URL.Path == "/api/v1/users" && r.Method == "POST":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "2", "name": "Bob"})
		case r.URL.Path == "/api/v1/users/1":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "1", "name": "Alice"})
		case r.URL.Path == "/api/v1/posts":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"posts": []string{}})
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
		}
	}))
	defer srv.Close()

	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	err = store.RunMigrations(ctx, s.DB(), "../../migrations")
	require.NoError(t, err)

	count, _ := store.SeedStrategies(ctx, s, "integration-test", zap.NewNop())
	assert.Greater(t, count, 0, "should seed strategies")

	// Tool-call preset drives Scout.Plan via DecideWithTools.
	mockClient := llm.NewMockClient(nil)
	mockClient.SetToolResponse("test all APIs", []llm.ToolCall{
		{Name: "test_http_endpoint", Input: map[string]any{"method": "GET", "path": "/health"}},
		{Name: "test_http_endpoint", Input: map[string]any{"method": "GET", "path": "/api/v1/users"}},
		{Name: "test_http_endpoint", Input: map[string]any{"method": "POST", "path": "/api/v1/users", "expect_status": 201}},
		{Name: "test_http_endpoint", Input: map[string]any{"method": "GET", "path": "/api/v1/posts"}},
	})

	judgeJSON, _ := json.Marshal(examiner.JudgeResult{
		Status:                examiner.StatusPass,
		CorrectnessConfidence: 0.9,
		Reasoning:             "response matches expected",
	})

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

	services := []project.Service{{Name: "default", URL: srv.URL}}
	engine := agent.NewRuleEngine(services, nil, ".")
	multiExec := agent.BuildMultiExecutor(".", nil, nil, nil, zap.NewNop())
	emb := embed.NewTrigramProvider(embed.DefaultDimension)
	loop := agent.NewReActLoopWithConfig(agent.ReActLoopConfig{Driver: driver, Store: s, Engine: engine, Executor: multiExec, Config: agent.DefaultReActConfig(), Logger: zap.NewNop(), Embedder: emb})

	results, err := loop.ExecutePlan(ctx, plan, sess.ID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1, "should have step results")

	passed := 0
	for _, r := range results {
		if r.Status == agent.StepPassed {
			passed++
		}
	}
	assert.Greater(t, passed, 0, "at least some tests should pass")

	// Phase 4: Examiner — Judge + Learn.
	judgeClient := llm.NewMockClient(map[string]string{"default": string(judgeJSON)})
	judgeDriver := ai.NewDriver(judgeClient, ai.NewTokenBudget(500000, 50000))

	examinerCfg := examiner.DefaultExaminerConfig()
	examinerHead := examiner.NewExaminer(judgeDriver, nil, s, examinerCfg, zap.NewNop())
	verdicts, reflections, err := examinerHead.Examine(ctx, results, sess.ID, "integration-test")
	require.NoError(t, err)
	assert.Equal(t, len(results), len(verdicts), "should have verdict for each result")
	assert.GreaterOrEqual(t, reflections, 0, "may store reflections")

	traces, err := s.GetTraces(ctx, sess.ID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(traces), 1, "should have traces")
}

// TestEndToEnd_ExaminerDegradation tests the Examiner's graceful degradation
// when the Judge LLM fails — falls back to step status.
func TestEndToEnd_ExaminerDegradation(t *testing.T) {
	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	err = store.RunMigrations(ctx, s.DB(), "../../migrations")
	require.NoError(t, err)

	sess, err := s.CreateSession(ctx, "run", "degradation test", "test")
	require.NoError(t, err)

	badClient := llm.NewMockClient(map[string]string{"default": "not valid json"})
	badDriver := ai.NewDriver(badClient, ai.NewTokenBudget(500000, 50000))

	results := []agent.StepResult{
		{TestCase: &agent.TestCase{ID: "tc-001"}, Status: agent.StepPassed, Result: types.HTTPResult{OK: true, StatusCode: 200}},
		{TestCase: &agent.TestCase{ID: "tc-002"}, Status: agent.StepFailed, Result: types.HTTPResult{OK: false, StatusCode: 500}},
	}

	examinerCfg := examiner.DefaultExaminerConfig()
	ex := examiner.NewExaminer(badDriver, nil, s, examinerCfg, zap.NewNop())
	verdicts, _, err := ex.Examine(ctx, results, sess.ID, "test")
	require.NoError(t, err)
	require.Len(t, verdicts, 2)

	assert.Equal(t, examiner.StatusPass, verdicts[0].Status)
	assert.Equal(t, examiner.StatusFail, verdicts[1].Status)
}

// TestEndToEnd_ScoutFallback tests that Scout produces a deterministic
// fallback plan when the LLM is unavailable (transient provider error). The
// legacy variant fed "garbage" JSON to trigger parse-failure fallback; parsing
// no longer exists, so the trigger is now a Complete() error, which is the
// production signal of a provider outage.
func TestEndToEnd_ScoutFallback(t *testing.T) {
	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	err = store.RunMigrations(ctx, s.DB(), "../../migrations")
	require.NoError(t, err)

	driver := ai.NewDriver(&smokeErrorClient{}, ai.NewTokenBudget(500000, 50000))

	cfg := &project.Config{
		Project: project.ProjectMeta{Name: "fallback-test"},
		Services: []project.Service{
			{Name: "api", URL: "http://localhost:8080", Health: "/health"},
			{Name: "web", URL: "http://localhost:3000", Health: "/"},
		},
	}

	scoutHead := scout.NewScout(driver, s, cfg, zap.NewNop())

	model, err := scoutHead.Analyze(ctx, scout.TargetInfo{URL: "http://localhost:8080", Goal: "test"})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(model.API.Endpoints), 2, "should have health endpoints from config")

	plan, err := scoutHead.Plan(ctx, "test everything", model)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(plan.Cases), 2, "should have cases for each endpoint")
}

// smokeErrorClient is a minimal llm.Client that always returns an error — the
// production signal of a provider outage that triggers Scout's fallback path.
type smokeErrorClient struct{}

func (m *smokeErrorClient) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	return nil, fmt.Errorf("simulated provider outage")
}
func (m *smokeErrorClient) CompleteWithVision(ctx context.Context, prompt string, images [][]byte) (*llm.Response, error) {
	return nil, fmt.Errorf("simulated provider outage")
}
func (m *smokeErrorClient) Stream(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
	return nil, fmt.Errorf("simulated provider outage")
}
