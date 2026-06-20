package agent

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
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/binoctal/cerberus/internal/types"
)

// testLoop creates a ReActLoop with a mock LLM and in-memory store.
func testLoop(t *testing.T, responses map[string]string, server *httptest.Server) (*ReActLoop, *store.Store) {
	t.Helper()

	s, err := store.New(":memory:")
	require.NoError(t, err)
	err = store.RunMigrations(context.Background(), s.DB(), "../../../migrations")
	require.NoError(t, err)

	mockClient := llm.NewMockClient(responses)
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))

	baseURL := ""
	var engine *RuleEngine
	if server != nil {
		baseURL = server.URL
		engine = NewRuleEngine(baseURL, nil, ".")
	} else {
		engine = NewRuleEngine("https://example.com", nil, ".")
	}

	executor := BuildMultiExecutor(".", nil, zap.NewNop())
	emb := embed.NewTrigramProvider(embed.DefaultDimension)
	loop := NewReActLoopWithConfig(ReActLoopConfig{
		Driver:   driver,
		Store:    s,
		Engine:   engine,
		Executor: executor,
		Config:   DefaultReActConfig(),
		Logger:   zap.NewNop(),
		Embedder: emb,
	})

	return loop, s
}

func createTestSession(t *testing.T, s *store.Store) string {
	t.Helper()
	sess, err := s.CreateSession(context.Background(), "run", "test", "")
	require.NoError(t, err)
	return sess.ID
}

// makeSteerEnvelope creates a SteerOutput with an HTTP action envelope.
func makeSteerEnvelope(reasoning, method, url string) SteerOutput {
	return SteerOutput{
		Reasoning: reasoning,
		Envelope: types.ActionEnvelope{
			Type: types.ActionAPIRequest,
			Raw: mustJSON(types.HTTPAction{
				Method: method,
				URL:    url,
			}),
		},
	}
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func TestReActLoop_RuleEngineSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	loop, s := testLoop(t, nil, server)
	sessionID := createTestSession(t, s)

	plan := &TestPlan{
		Goal: "test API",
		Cases: []TestCase{
			{ID: "t1", Name: "get users", Target: "/api/users", Method: "GET"},
		},
		ProjectURL: server.URL,
	}

	results, err := loop.ExecutePlan(context.Background(), plan, sessionID)
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.Equal(t, StepPassed, results[0].Status)
	assert.Equal(t, 1, results[0].Attempts)
	assert.Empty(t, results[0].Error)
}

func TestReActLoop_SteerSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/complex" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"result":"success"}`))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	steerJSON, _ := json.Marshal(makeSteerEnvelope("try the complex endpoint", "GET", server.URL+"/api/complex"))

	loop, s := testLoop(t, map[string]string{
		"default": string(steerJSON),
	}, server)
	sessionID := createTestSession(t, s)

	plan := &TestPlan{
		Goal: "test complex flow",
		Cases: []TestCase{
			{ID: "t1", Name: "complex test", Target: "verify complex API flow", Expectation: "returns success"},
		},
	}

	results, err := loop.ExecutePlan(context.Background(), plan, sessionID)
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.Equal(t, StepPassed, results[0].Status)
	assert.Equal(t, 1, results[0].Attempts)
}

func TestReActLoop_MaxAttemptsExhausted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal"}`))
	}))
	defer server.Close()

	steerJSON, _ := json.Marshal(makeSteerEnvelope("try again", "GET", server.URL+"/fail"))

	loop, s := testLoop(t, map[string]string{
		"default": string(steerJSON),
	}, server)
	sessionID := createTestSession(t, s)

	plan := &TestPlan{
		Goal: "test failure",
		Cases: []TestCase{
			{ID: "t1", Name: "failing test", Target: "test that fails", Expectation: "should fail"},
		},
	}

	results, err := loop.ExecutePlan(context.Background(), plan, sessionID)
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.Equal(t, StepFailed, results[0].Status)
	assert.Equal(t, 3, results[0].Attempts)
}

func TestReActLoop_RecoverySkip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	steerJSON, _ := json.Marshal(makeSteerEnvelope("try request", "GET", server.URL+"/fail"))

	loop, s := testLoop(t, map[string]string{
		"default": string(steerJSON),
	}, server)

	// Override recovery to always skip.
	loop.recovery = &fixedRecovery{skip: true}

	sessionID := createTestSession(t, s)
	plan := &TestPlan{
		Goal: "test recovery skip",
		Cases: []TestCase{
			{ID: "t1", Name: "skip test", Target: "test with skip recovery", Expectation: "skip"},
		},
	}

	results, err := loop.ExecutePlan(context.Background(), plan, sessionID)
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.Equal(t, StepSkipped, results[0].Status)
}

func TestReActExecutePlan_MultipleCases(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"call":%d}`, callCount)
	}))
	defer server.Close()

	loop, s := testLoop(t, nil, server)
	sessionID := createTestSession(t, s)

	plan := &TestPlan{
		Goal: "multi-case test",
		Cases: []TestCase{
			{ID: "t1", Name: "users", Target: "/api/users", Method: "GET"},
			{ID: "t2", Name: "posts", Target: "/api/posts", Method: "GET"},
			{ID: "t3", Name: "health", Target: "/health", Method: "GET"},
		},
	}

	results, err := loop.ExecutePlan(context.Background(), plan, sessionID)
	require.NoError(t, err)
	assert.Len(t, results, 3)

	for _, r := range results {
		assert.Equal(t, StepPassed, r.Status)
	}

	// Verify traces were created.
	traces, err := s.GetTraces(context.Background(), sessionID)
	require.NoError(t, err)
	assert.Len(t, traces, 3)
}

// fixedRecovery is a test double for Recovery that always returns skip.
type fixedRecovery struct {
	skip bool
}

func (f *fixedRecovery) Recover(ctx context.Context, tc TestCase, result types.ExecutorResult, attempt int) (RecoverDecision, error) {
	return RecoverDecision{Skip: f.skip}, nil
}

func (f *fixedRecovery) SetSessionID(id string) {
	// No-op for test double
}
