package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
)

// TestSteer_FallsBackOnActionUnmarshalError verifies that when the LLM returns
// a steer action whose envelope type resolves but whose payload is empty (a
// common schema mismatch with non-Claude models), steer falls back to a safe
// action instead of hard-failing. Previously this error bypassed the
// isParseError fallback (it lives in types.UnmarshalAction, not the driver),
// wasting every MaxSteerAttempts retry and failing the whole test case.
func TestSteer_FallsBackOnActionUnmarshalError(t *testing.T) {
	loop, _ := testLoop(t, map[string]string{
		"default": `{"reasoning":"x","action":{"type":"api_request"}}`,
	}, nil)

	tc := &TestCase{ID: "tc-fb", Name: "fallback", Target: "/t", Expectation: "e"}

	action, err := loop.steer(context.Background(), tc, nil, 1)

	require.NoError(t, err, "steer must not hard-fail on action unmarshal error")
	assert.NotNil(t, action, "steer must return a fallback action")
}

// recordingMockClient captures the last prompt it received for inspection.
type recordingMockClient struct {
	mu         sync.Mutex
	lastPrompt string
	responses  map[string]string
}

func (r *recordingMockClient) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	r.mu.Lock()
	// Capture the full prompt content
	var contents []string
	for _, msg := range req.Messages {
		contents = append(contents, msg.Content)
	}
	r.lastPrompt = strings.Join(contents, "\n\n")
	r.mu.Unlock()

	// Delegate to the real mock client
	mock := llm.NewMockClient(r.responses)
	return mock.Complete(ctx, req)
}

func (r *recordingMockClient) CompleteWithVision(ctx context.Context, prompt string, images [][]byte) (*llm.Response, error) {
	mock := llm.NewMockClient(r.responses)
	return mock.CompleteWithVision(ctx, prompt, images)
}

func (r *recordingMockClient) Stream(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
	mock := llm.NewMockClient(r.responses)
	return mock.Stream(ctx, req)
}

func (r *recordingMockClient) getLastPrompt() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastPrompt
}

// testLoopWithRecordingClient creates a ReActLoop with a client that records prompts.
func testLoopWithRecordingClient(t *testing.T, responses map[string]string, services []project.Service) (*ReActLoop, *store.Store, *recordingMockClient) {
	t.Helper()

	s, err := store.New(":memory:")
	require.NoError(t, err)
	err = store.RunMigrations(context.Background(), s.DB(), "../../../migrations")
	require.NoError(t, err)

	recordingClient := &recordingMockClient{responses: responses}
	driver := ai.NewDriver(recordingClient, ai.NewTokenBudget(200000, 10000))

	engine := NewRuleEngine(services, nil, ".")

	executor := BuildMultiExecutor(".", nil, nil, zap.NewNop())
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

	return loop, s, recordingClient
}

// TestSteer_TaskContextIncludesServiceBase verifies that the steer prompt
// includes the service base URL when tc.Service is set. It captures the
// actual prompt sent to the LLM and asserts it contains the base URL hint.
func TestSteer_TaskContextIncludesServiceBase(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	steerJSON, _ := json.Marshal(makeSteerEnvelope("hit", "GET", "/api/data"))
	loop, s, recordingClient := testLoopWithRecordingClient(t,
		map[string]string{"default": string(steerJSON)},
		[]project.Service{{Name: "gateway", URL: server.URL}})
	sessionID := createTestSession(t, s)

	plan := &TestPlan{Goal: "g", Cases: []TestCase{
		{ID: "t1", Name: "hit gateway", Target: "verify", Service: "gateway", Expectation: "ok"},
	}}
	_, err := loop.ExecutePlan(context.Background(), plan, sessionID)
	require.NoError(t, err)

	// Verify the prompt contained the service base URL hint
	lastPrompt := recordingClient.getLastPrompt()
	require.Contains(t, lastPrompt, "Service base URL:",
		"the steer prompt must include the service base URL hint")
	require.Contains(t, lastPrompt, server.URL,
		"the service base URL hint must include the actual service URL")
	require.Contains(t, lastPrompt, "use this host for api_request URLs",
		"the hint must instruct the LLM to use this host for api_request URLs")
}
