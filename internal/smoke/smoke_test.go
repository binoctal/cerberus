package smoke

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/contract"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/binoctal/cerberus/internal/types"
)

// smokeCoverageFn is a stub CoverageFn for smoke tests, avoiding real go
// test/jest/pytest subprocesses when ProjectDir is the cerberus repo under
// test (which would recurse). See internal/session/coverage.go.
func smokeCoverageFn() func(context.Context, *session.Session) contract.CoverageMeasurement {
	return func(context.Context, *session.Session) contract.CoverageMeasurement {
		return contract.CoverageMeasurement{Pct: 1.0, Unit: "line", Known: true}
	}
}

func TestSessionSmokeTest(t *testing.T) {
	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

	ctx := context.Background()
	err = store.RunMigrations(ctx, s.DB(), "../../migrations")
	require.NoError(t, err)

	mockResp := `{"status":"pass","confidence":0.9,"reasoning":"mock analysis"}`
	client := llm.NewMockClient(map[string]string{"default": mockResp})

	logger, _ := zap.NewDevelopment()

	cfg := project.DefaultConfig()
	cfg.Project.Name = "smoke-test"

	sess, err := session.NewSession(ctx, session.SessionConfig{
		Mode:       session.ModeRun,
		Goal:       "smoke test goal",
		Config:     &cfg,
		Store:      s,
		Client:     client,
		Logger:     logger,
		Gate:       nil,
		ProjectDir: ".",
		CoverageFn: smokeCoverageFn(),
	})
	require.NoError(t, err)
	assert.NotEmpty(t, sess.ID)

	err = sess.Run(ctx)
	require.NoError(t, err)

	dbSess, err := s.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, "completed", dbSess.Status)
	assert.Equal(t, "smoke test goal", dbSess.Goal)

	sess.Close()
}

func TestAIDriverSmokeTest(t *testing.T) {
	mockResp := `{"verdict":"pass","confidence":0.95,"reasoning":"response matches expected"}`
	client := llm.NewMockClient(map[string]string{"default": mockResp})

	driver := ai.NewDriver(client, ai.NewTokenBudget(200000, 10000))

	type Verdict struct {
		Verdict    string  `json:"verdict"`
		Confidence float64 `json:"confidence"`
		Reasoning  string  `json:"reasoning"`
	}

	var v Verdict
	err := driver.Decide(context.Background(),
		ai.NewPrompt().
			System("You are a test judge").
			Task("Evaluate: POST /api/v1/users returned 201").
			Output("JSON with verdict, confidence, reasoning").
			Build(),
		&v,
	)
	require.NoError(t, err)
	assert.Equal(t, "pass", v.Verdict)
	assert.InDelta(t, 0.95, v.Confidence, 0.01)
	assert.Less(t, driver.Budget().Remaining(), 200000)
}

func TestProjectLoaderSmokeTest(t *testing.T) {
	t.Setenv("TEST_URL", "http://localhost:8080")
	// t.Setenv auto-cleans

	yaml := `
project:
  name: smoke-app
services:
  - name: api
    url: "${TEST_URL}"
settings:
  confidence_threshold: 0.8
`
	cfg, err := project.LoadFromYAML([]byte(yaml))
	require.NoError(t, err)
	assert.Equal(t, "smoke-app", cfg.Project.Name)
	assert.Equal(t, "http://localhost:8080", cfg.Services[0].URL)
	assert.Equal(t, 0.8, cfg.Settings.ConfidenceThreshold)

	assert.Equal(t, 200000, cfg.Settings.AIBudget.SessionTotalTokens)
}

func mustRawJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func TestAgentSmokeTest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/users":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"users": []string{"alice", "bob"}})
		case "/api/v1/posts":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"posts": []string{}})
		case "/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	err = store.RunMigrations(ctx, s.DB(), "../../migrations")
	require.NoError(t, err)

	steerJSON, _ := json.Marshal(agent.SteerOutput{
		Reasoning: "navigate to the endpoint",
		Envelope: types.ActionEnvelope{
			Type: types.ActionAPIRequest,
			Raw: mustRawJSON(types.HTTPAction{
				Method: "GET",
				URL:    server.URL + "/api/v1/users",
			}),
		},
	})
	mockClient := llm.NewMockClient(map[string]string{
		"default": string(steerJSON),
	})
	logger := zap.NewNop()

	cfg := project.DefaultConfig()
	cfg.Project.Name = "agent-smoke"
	cfg.Services = []project.Service{
		{Name: "api", URL: server.URL, Health: "/health"},
	}
	cfg.Invariants = []project.Invariant{
		{ID: "users-api", Description: "Users endpoint works", Check: "/api/v1/users", Assertion: "returns 200"},
		{ID: "posts-api", Description: "Posts endpoint works", Check: "/api/v1/posts", Assertion: "returns 200"},
	}

	sess, err := session.NewSession(ctx, session.SessionConfig{
		Mode:       session.ModeRun,
		Goal:       "agent smoke test",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     logger,
		Gate:       nil,
		ProjectDir: ".",
		CoverageFn: smokeCoverageFn(),
	})
	require.NoError(t, err)

	err = sess.Run(ctx)
	require.NoError(t, err)

	dbSess, err := s.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, "completed", dbSess.Status)

	traces, err := s.GetTraces(ctx, sess.ID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(traces), 2, "should have traces for health check and invariants")

	for _, tr := range traces {
		evidence, err := s.GetEvidenceByTrace(ctx, tr.ID)
		require.NoError(t, err)
		assert.NotEmpty(t, evidence, "trace %d should have evidence", tr.ID)
	}

	sess.Close()
}

func TestAgentWithReActSmokeTest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/items" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"items":[]}`))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	err = store.RunMigrations(ctx, s.DB(), "../../migrations")
	require.NoError(t, err)

	steerJSON, _ := json.Marshal(agent.SteerOutput{
		Reasoning: "try the items endpoint",
		Envelope: types.ActionEnvelope{
			Type: types.ActionAPIRequest,
			Raw: mustRawJSON(types.HTTPAction{
				Method: "GET",
				URL:    server.URL + "/api/v1/items",
			}),
		},
	})
	mockClient := llm.NewMockClient(map[string]string{
		"default": string(steerJSON),
	})

	services := []project.Service{{Name: "default", URL: server.URL}}
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))
	engine := agent.NewRuleEngine(services, nil, ".")
	exec := agent.BuildMultiExecutor(".", nil, nil, nil, zap.NewNop())
	emb := embed.NewTrigramProvider(embed.DefaultDimension)
	loop := agent.NewReActLoopWithConfig(agent.ReActLoopConfig{
		Driver:   driver,
		Store:    s,
		Engine:   engine,
		Executor: exec,
		Config:   agent.DefaultReActConfig(),
		Logger:   zap.NewNop(),
		Embedder: emb,
	})

	sess, err := s.CreateSession(ctx, "run", "react smoke", "")
	require.NoError(t, err)

	plan := &agent.TestPlan{
		Goal:       "test items endpoint",
		ProjectURL: server.URL,
		Cases: []agent.TestCase{
			{ID: "r1", Name: "find items", Target: "find the items endpoint", Expectation: "returns list"},
		},
	}

	results, err := loop.ExecutePlan(ctx, plan, sess.ID)
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.Equal(t, agent.StepPassed, results[0].Status)
	assert.Equal(t, 1, results[0].Attempts)

	assert.Less(t, driver.Budget().Remaining(), 200000, "should have spent tokens on Steer")
}
