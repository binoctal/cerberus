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
)

// TestEndToEnd_CRUDPipeline tests full CRUD + auth + health against
// a 9-route httptest server, verifying each pipeline stage.
func TestEndToEnd_CRUDPipeline(t *testing.T) {
	type user struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	users := []user{{ID: "1", Name: "Alice"}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		switch {
		case r.URL.Path == "/health":
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"status":"ok"}`)
		case r.URL.Path == "/api/v1/users" && r.Method == "GET":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"users": users})
		case r.URL.Path == "/api/v1/users" && r.Method == "POST":
			if auth == "" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = fmt.Fprint(w, `{"error":"unauthorized"}`)
				return
			}
			var u user
			_ = json.NewDecoder(r.Body).Decode(&u)
			u.ID = fmt.Sprintf("%d", len(users)+1)
			users = append(users, u)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(u)
		case r.URL.Path == "/api/v1/users/1" && r.Method == "GET":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(users[0])
		case r.URL.Path == "/api/v1/users/1" && r.Method == "PUT":
			if auth == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			var u user
			_ = json.NewDecoder(r.Body).Decode(&u)
			users[0].Name = u.Name
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(users[0])
		case r.URL.Path == "/api/v1/users/1" && r.Method == "DELETE":
			if auth == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/api/v1/posts":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"posts": []string{}})
		case r.URL.Path == "/api/v1/stats":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"uptime": 99.9, "requests": 1000})
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprintf(w, `{"error":"not found","path":"%s"}`, r.URL.Path)
		}
	}))
	defer srv.Close()

	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../migrations"))

	_, _ = store.SeedStrategies(ctx, s, "e2e-test", zap.NewNop())

	cfg := &project.Config{
		Project:  project.ProjectMeta{Name: "e2e-crud"},
		Services: []project.Service{{Name: "api", URL: srv.URL, Health: "/health"}},
	}

	// Stage 1: Scout Analyze.
	mockAnalyze := llm.NewMockClient(map[string]string{"default": `{"endpoints":[{"path":"/health","method":"GET","confidence":0.95},{"path":"/api/v1/users","method":"GET","confidence":0.95},{"path":"/api/v1/users","method":"POST","confidence":0.9},{"path":"/api/v1/users/1","method":"GET","confidence":0.95},{"path":"/api/v1/users/1","method":"PUT","confidence":0.9},{"path":"/api/v1/users/1","method":"DELETE","confidence":0.9},{"path":"/api/v1/posts","method":"GET","confidence":0.85},{"path":"/api/v1/stats","method":"GET","confidence":0.85}],"pages":[],"tech_stack":["go"]}`})
	analyzeDriver := ai.NewDriver(mockAnalyze, ai.NewTokenBudget(500000, 50000))
	scoutHead := scout.NewScout(analyzeDriver, s, cfg, zap.NewNop())

	model, err := scoutHead.Analyze(ctx, scout.TargetInfo{URL: srv.URL, Goal: "test all CRUD operations"})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(model.API.Endpoints), 4, "should discover endpoints")

	// Stage 2: Scout Plan.
	planOutput := scout.PlanOutput{
		Cases: []scout.CaseInfo{
			{ID: "tc-001", Name: "Health check", Target: "/health", Method: "GET", Expectation: "200", Priority: 1.0},
			{ID: "tc-002", Name: "List users", Target: "/api/v1/users", Method: "GET", Expectation: "200", Priority: 0.9},
			{ID: "tc-003", Name: "Create user (unauth)", Target: "/api/v1/users", Method: "POST", Expectation: "401", Priority: 0.8},
			{ID: "tc-004", Name: "Get user 1", Target: "/api/v1/users/1", Method: "GET", Expectation: "200", Priority: 0.9},
			{ID: "tc-005", Name: "Update user (unauth)", Target: "/api/v1/users/1", Method: "PUT", Expectation: "401", Priority: 0.7},
			{ID: "tc-006", Name: "Delete user (unauth)", Target: "/api/v1/users/1", Method: "DELETE", Expectation: "401", Priority: 0.7},
			{ID: "tc-007", Name: "List posts", Target: "/api/v1/posts", Method: "GET", Expectation: "200", Priority: 0.6},
			{ID: "tc-008", Name: "Get stats", Target: "/api/v1/stats", Method: "GET", Expectation: "200", Priority: 0.5},
		},
	}
	planJSON, _ := json.Marshal(planOutput)
	mockPlan := llm.NewMockClient(map[string]string{"default": string(planJSON)})
	planDriver := ai.NewDriver(mockPlan, ai.NewTokenBudget(500000, 50000))
	scoutWithPlan := scout.NewScout(planDriver, s, cfg, zap.NewNop())

	plan, err := scoutWithPlan.Plan(ctx, "test all CRUD operations", model)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(plan.Cases), 8, "should plan 8+ cases")

	// Stage 3: Agent Execute.
	sess, err := s.CreateSession(ctx, "run", "test all CRUD operations", "e2e-crud")
	require.NoError(t, err)

	engine := agent.NewRuleEngine(srv.URL, nil, ".")
	multiExec := agent.BuildMultiExecutor(".", nil, zap.NewNop())
	reactCfg := agent.DefaultReActConfig()
	emb := embed.NewTrigramProvider(embed.DefaultDimension)
	loop := agent.NewReActLoopWithConfig(agent.ReActLoopConfig{
		Driver:   planDriver,
		Store:    s,
		Engine:   engine,
		Executor: multiExec,
		Config:   reactCfg,
		Logger:   zap.NewNop(),
		Embedder: emb,
	})

	results, err := loop.ExecutePlan(ctx, plan, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, len(plan.Cases), len(results), "should have result for each planned case")

	passed, failed := 0, 0
	for _, r := range results {
		switch r.Status {
		case agent.StepPassed:
			passed++
		case agent.StepFailed:
			failed++
		}
	}
	t.Logf("Results: %d passed, %d failed out of %d", passed, failed, len(results))
	assert.Greater(t, passed, 0, "at least some tests should pass")

	// Stage 4: Examiner Judge.
	judgeOutput := examiner.JudgeResult{
		Status:                examiner.StatusPass,
		CorrectnessConfidence: 0.95,
		Reasoning:             "response matches expected",
	}
	judgeJSON, _ := json.Marshal(judgeOutput)
	judgeClient := llm.NewMockClient(map[string]string{"default": string(judgeJSON)})
	judgeDriver := ai.NewDriver(judgeClient, ai.NewTokenBudget(500000, 50000))

	examinerCfg := examiner.DefaultExaminerConfig()
	examinerHead := examiner.NewExaminer(judgeDriver, nil, s, examinerCfg, zap.NewNop())
	verdicts, reflections, err := examinerHead.Examine(ctx, results, sess.ID, "e2e-crud")
	require.NoError(t, err)
	assert.Equal(t, len(results), len(verdicts), "should have verdict for each result")
	assert.GreaterOrEqual(t, reflections, 0, "may store reflections")

	// Stage 5: Verify traces.
	traces, err := s.GetTraces(ctx, sess.ID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(traces), passed, "should have traces for passed cases")
}

// TestEndToEnd_ProgressEvents verifies that progress events are emitted during execution.
func TestEndToEnd_ProgressEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../migrations"))

	sess, err := s.CreateSession(ctx, "run", "progress test", "test")
	require.NoError(t, err)

	planJSON, _ := json.Marshal(scout.PlanOutput{
		Cases: []scout.CaseInfo{
			{ID: "tc-001", Name: "GET /ping", Target: "/ping", Method: "GET", Expectation: "200"},
			{ID: "tc-002", Name: "GET /status", Target: "/status", Method: "GET", Expectation: "200"},
		},
	})

	mockClient := llm.NewMockClient(map[string]string{"default": string(planJSON)})
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(500000, 50000))

	engine := agent.NewRuleEngine(srv.URL, nil, ".")
	multiExec := agent.BuildMultiExecutor(".", nil, zap.NewNop())
	emb := embed.NewTrigramProvider(embed.DefaultDimension)
	loop := agent.NewReActLoopWithConfig(agent.ReActLoopConfig{Driver: driver, Store: s, Engine: engine, Executor: multiExec, Config: agent.DefaultReActConfig(), Logger: zap.NewNop(), Embedder: emb})

	// Collect progress events.
	progressCh := make(chan agent.ProgressEvent, 32)
	loop.SetProgressChannel(progressCh)

	plan := &agent.TestPlan{
		Goal:       "progress test",
		ProjectURL: srv.URL,
		Cases: []agent.TestCase{
			{ID: "tc-001", Name: "GET /ping", Target: "/ping", Method: "GET"},
			{ID: "tc-002", Name: "GET /status", Target: "/status", Method: "GET"},
		},
	}

	results, err := loop.ExecutePlan(ctx, plan, sess.ID)
	require.NoError(t, err)
	assert.Len(t, results, 2)

	// Collect all events.
	close(progressCh)
	var events []agent.ProgressEvent
	for e := range progressCh {
		events = append(events, e)
	}

	// Expect: 2x case_start + 2x case_complete + 1x plan_complete = 5.
	assert.GreaterOrEqual(t, len(events), 5, "should have at least 5 progress events")

	// Verify plan_complete is the last event.
	last := events[len(events)-1]
	assert.Equal(t, "plan_complete", last.Type)
}

// TestEndToEnd_RuleEngineStats verifies rule engine hit/miss stats after execution.
func TestEndToEnd_RuleEngineStats(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../migrations"))

	sess, err := s.CreateSession(ctx, "run", "stats test", "test")
	require.NoError(t, err)

	mockClient := llm.NewMockClient(map[string]string{"default": "{}"})
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(500000, 50000))

	engine := agent.NewRuleEngine(srv.URL, nil, ".")
	multiExec := agent.BuildMultiExecutor(".", nil, zap.NewNop())
	emb := embed.NewTrigramProvider(embed.DefaultDimension)
	loop := agent.NewReActLoopWithConfig(agent.ReActLoopConfig{Driver: driver, Store: s, Engine: engine, Executor: multiExec, Config: agent.DefaultReActConfig(), Logger: zap.NewNop(), Embedder: emb})

	plan := &agent.TestPlan{
		Goal:       "stats test",
		ProjectURL: srv.URL,
		Cases: []agent.TestCase{
			{ID: "tc-001", Name: "GET /api", Target: "/api", Method: "GET"},       // matches Rule 1
			{ID: "tc-002", Name: "Navigate", Target: "/page", Action: "navigate"}, // matches Rule 2
			{ID: "tc-003", Name: "Custom", Target: "weird-thing"},                 // no match (miss)
		},
	}

	_, err = loop.ExecutePlan(ctx, plan, sess.ID)
	require.NoError(t, err)

	hits, misses := engine.Stats()
	assert.Greater(t, hits, int64(0), "should have rule engine hits")
	t.Logf("Rule engine stats: hits=%d, misses=%d", hits, misses)
}
