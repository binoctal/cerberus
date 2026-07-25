package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/binoctal/cerberus/internal/types"
)

// testLoop creates a ReActLoop with a mock LLM and in-memory store. The mock
// client is returned so tests can preset tool-call responses via
// SetToolResponse (used by the tool-calling steer/recovery migrations).
func testLoop(t *testing.T, responses map[string]string, server *httptest.Server) (*ReActLoop, *store.Store, *llm.MockClient) {
	t.Helper()

	s, err := store.New(":memory:")
	require.NoError(t, err)
	err = store.RunMigrations(context.Background(), s.DB(), "../../../migrations")
	require.NoError(t, err)

	mockClient := llm.NewMockClient(responses)
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))

	var services []project.Service
	if server != nil {
		services = []project.Service{{Name: "default", URL: server.URL}}
	} else {
		services = []project.Service{{Name: "default", URL: "https://example.com"}}
	}
	engine := NewRuleEngine(services, nil, ".")

	executor := BuildMultiExecutor(".", nil, nil, nil, zap.NewNop())
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

	return loop, s, mockClient
}

func createTestSession(t *testing.T, s *store.Store) string {
	t.Helper()
	sess, err := s.CreateSession(context.Background(), "run", "test", "")
	require.NoError(t, err)
	return sess.ID
}

// makeSteerEnvelope was a JSON-envelope test fixture for the legacy Decide
// steer path. S3 deleted that path (steer is now tool-call-driven), so the
// helper has no callers. SteerOutput + ActionEnvelope stay defined until T4
// deletes the drift subsystem; parse_fallback_test.go and recovery still use
// mustJSON below.

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

	loop, s, _ := testLoop(t, nil, server)
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

	loop, s, mock := testLoop(t, nil, server)
	mock.SetToolResponse("default", []llm.ToolCall{toolCallFromAction(types.HTTPAction{
		Method: "GET", URL: server.URL + "/api/complex",
	})})
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

	loop, s, mock := testLoop(t, nil, server)
	mock.SetToolResponse("default", []llm.ToolCall{toolCallFromAction(types.HTTPAction{
		Method: "GET", URL: server.URL + "/fail",
	})})
	// Install a no-op recovery double to keep this loop-exhaustion test scoped
	// to the steer path: production recovery now also consumes the mock's
	// "default" tool response (S3 migrated it to DecideWithTools), which would
	// make this test exercise the recovery action /fail → fail path. The
	// no-op keeps attempts==MaxSteerAttempts deterministic; recovery's own
	// behavior is covered in recovery_test.go.
	loop.recovery = &fixedRecovery{skip: false}
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

	loop, s, mock := testLoop(t, nil, server)
	mock.SetToolResponse("default", []llm.ToolCall{toolCallFromAction(types.HTTPAction{
		Method: "GET", URL: server.URL + "/fail",
	})})

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

	loop, s, _ := testLoop(t, nil, server)
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

func (f *fixedRecovery) SetProject(name string) {
	// No-op for test double
}

func TestReActLoop_SingleServiceBackwardCompat(t *testing.T) {
	requestReceived := &atomic.Bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestReceived.Store(true)
		require.Equal(t, "/api/users", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	// testLoop builds a single-service engine via Services[0]; configures a single Service
	// with no Service attribution set on the TestCase.
	loop, s, _ := testLoop(t, nil, server)
	sessionID := createTestSession(t, s)
	plan := &TestPlan{Goal: "g", Cases: []TestCase{
		{ID: "t1", Target: "/api/users", Method: "GET", Expectation: "ok"},
	}}
	results, err := loop.ExecutePlan(context.Background(), plan, sessionID)
	require.NoError(t, err)
	require.Equal(t, StepPassed, results[0].Status)
	require.True(t, requestReceived.Load(), "request should have hit Services[0] server")
}
