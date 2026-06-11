package smoke

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestSessionSmokeTest(t *testing.T) {
	// In-memory SQLite — no external DB needed
	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer s.Close()

	ctx := context.Background()
	err = store.RunMigrations(ctx, s.DB(), "../../migrations")
	require.NoError(t, err)

	mockResp := `{"status":"pass","confidence":0.9,"reasoning":"mock analysis"}`
	client := llm.NewMockClient(map[string]string{"default": mockResp})

	logger, _ := zap.NewDevelopment()

	cfg := project.DefaultConfig()
	cfg.Project.Name = "smoke-test"

	sess, err := session.NewSession(ctx, session.ModeRun, "smoke test goal", &cfg, s, client, logger, nil)
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
	os.Setenv("TEST_URL", "http://localhost:8080")
	defer os.Unsetenv("TEST_URL")

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

func TestAgentSmokeTest(t *testing.T) {
	// Stand up a real HTTP server as the SUT.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/users":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{"users": []string{"alice", "bob"}})
		case "/api/v1/posts":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{"posts": []string{}})
		case "/health":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// In-memory store + migrations.
	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer s.Close()
	ctx := context.Background()
	err = store.RunMigrations(ctx, s.DB(), "../../migrations")
	require.NoError(t, err)

	// Mock LLM: for invariants that need Steer, return a valid action pointing to the right endpoint.
	steerJSON, _ := json.Marshal(agent.SteerOutput{
		Reasoning: "navigate to the endpoint",
		Action: agent.Action{
			Type:   agent.ActionAPIRequest,
			Target: server.URL + "/api/v1/users",
			Method: "GET",
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

	sess, err := session.NewSession(ctx, session.ModeRun, "agent smoke test", &cfg, s, mockClient, logger, nil)
	require.NoError(t, err)

	err = sess.Run(ctx)
	require.NoError(t, err)

	// Verify session completed.
	dbSess, err := s.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, "completed", dbSess.Status)

	// Verify traces were created for each test case.
	traces, err := s.GetTraces(ctx, sess.ID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(traces), 2, "should have traces for health check and invariants")

	// Verify evidence was recorded.
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
			w.Write([]byte(`{"items":[]}`))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Setup store.
	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer s.Close()
	ctx := context.Background()
	err = store.RunMigrations(ctx, s.DB(), "../../migrations")
	require.NoError(t, err)

	// LLM returns a Steer action pointing to /api/v1/items.
	steerJSON, _ := json.Marshal(agent.SteerOutput{
		Reasoning: "try the items endpoint",
		Action: agent.Action{
			Type:   agent.ActionAPIRequest,
			Target: server.URL + "/api/v1/items",
			Method: "GET",
		},
	})
	mockClient := llm.NewMockClient(map[string]string{
		"default": string(steerJSON),
	})

	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))
	engine := agent.NewRuleEngine(server.URL, nil)
	exec := agent.NewHTTPActionExecutor(server.URL, zap.NewNop())
	loop := agent.NewReActLoop(driver, s, engine, exec, agent.DefaultReActConfig(), zap.NewNop())

	// Create session and trace.
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

	// Verify tokens were spent (LLM was called for Steer).
	assert.Less(t, driver.Budget().Remaining(), 200000, "should have spent tokens on Steer")
}
