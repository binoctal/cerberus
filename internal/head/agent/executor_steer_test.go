package agent

import (
	"context"
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
	"github.com/binoctal/cerberus/internal/types"
)

// TestSteer_AssemblesActionToolCall verifies the tool-calling steer path: when
// the LLM emits a single action tool call, steer assembles it to the matching
// TypedAction and reports a non-drift (zeroCall=false) result. Replaces the
// legacy JSON-envelope steer fixture.
func TestSteer_AssemblesActionToolCall(t *testing.T) {
	loop, _, mock := testLoop(t, nil, nil)
	mock.SetToolResponse("default", []llm.ToolCall{toolCallFromAction(types.HTTPAction{
		Method: "POST", URL: "/api/users", Body: `{"name":"x"}`,
	})})

	tc := &TestCase{ID: "tc-1", Name: "hit users", Target: "/api/users", Expectation: "e"}

	action, zeroCall, err := loop.steer(context.Background(), tc, nil, 1)

	require.NoError(t, err)
	assert.False(t, zeroCall, "an action tool call must clear the drift signal")
	ha, ok := action.(types.HTTPAction)
	require.True(t, ok, "action must assemble to HTTPAction")
	assert.Equal(t, "POST", ha.Method)
	assert.Equal(t, "/api/users", ha.URL)
	assert.Equal(t, `{"name":"x"}`, ha.Body)
}

// TestSteer_ZeroToolCallsReturnsWaitAndDriftSignal verifies the drift path:
// when the LLM emits no action tool calls, steer returns a deterministic
// WaitAction (loop-safe intermediate default) and signals zeroCall=true so the
// ReAct loop can escalate to StepSkipped after consecutive drifts. No keyword
// guessing (the deleted FallbackParseAction) — just a clean default.
func TestSteer_ZeroToolCallsReturnsWaitAndDriftSignal(t *testing.T) {
	// No SetToolResponse → mock returns the empty "default" text response →
	// DecideWithTools sees ToolCalls=nil and Content="" → steer treats this as
	// zero-call drift.
	loop, _, _ := testLoop(t, map[string]string{"default": ""}, nil)

	tc := &TestCase{ID: "tc-2", Name: "empty", Target: "/api/x", Expectation: "e"}

	action, zeroCall, err := loop.steer(context.Background(), tc, nil, 1)

	require.NoError(t, err, "zero-call drift must not surface as an error")
	assert.True(t, zeroCall, "zero tool calls must signal drift")
	w, ok := action.(types.WaitAction)
	require.True(t, ok, "drift default must be a WaitAction")
	assert.NotEmpty(t, w.Duration, "WaitAction must carry a positive duration so the loop does not spin")
}

// TestReActLoop_ConsecutiveZeroSteerSkipsCase verifies the spec §3 drift
// escalation end-to-end: when the LLM emits no action tool call across
// driftSkipThreshold consecutive steers, the loop finalizes the case as
// StepSkipped (not StepFailed) so the Examiner can distinguish LLM drift from
// a genuine test failure. The first drift stays in the loop as a WaitAction;
// only the threshold cross escalates.
func TestReActLoop_ConsecutiveZeroSteerSkipsCase(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK) // unused: zero-call drift never executes an action
	}))
	defer server.Close()

	// No SetToolResponse → every steer returns no tool calls → drift signal on
	// every attempt. The default text response is empty, so the mock returns
	// Content="" with ToolCalls=nil and DecideWithTools surfaces zero calls.
	loop, s, _ := testLoop(t, map[string]string{"default": ""}, server)
	sessionID := createTestSession(t, s)

	plan := &TestPlan{
		Goal: "drift escalation",
		Cases: []TestCase{
			{ID: "t1", Name: "drift", Target: "verify drift handling", Expectation: "skip on drift"},
		},
	}

	results, err := loop.ExecutePlan(context.Background(), plan, sessionID)
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.Equal(t, StepSkipped, results[0].Status,
		"consecutive zero-call steers must escalate to StepSkipped, not StepFailed")
	require.Error(t, results[0].Error, "drift skip must surface a reason")
	assert.Contains(t, results[0].Error.Error(), "drift",
		"error must identify the drift cause for Examiner triage")
	assert.Equal(t, driftSkipThreshold, results[0].Attempts,
		"escalation must fire at the threshold, not exhaust MaxSteerAttempts")
}

// TestReActLoop_SingleZeroSteerStaysInLoop verifies the tolerance for a single
// flaky empty steer: one zero-call returns a WaitAction and the loop retries
// rather than escalating. The case only escalates when a SECOND consecutive
// zero-call follows.
func TestReActLoop_SingleZeroSteerStaysInLoop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	loop, s, mock := testLoop(t, nil, server)
	// Steer sequence across attempts: drift, then a real action that passes.
	// We can't time-order mock responses by attempt, but we can exploit the
	// prompt substring: attempt 1's prompt contains "Attempt: 1/3" and attempt
	// 2's contains "Attempt: 2/3". Key the tool response on "Attempt: 2/3" so
	// only the second attempt gets a real action; the first attempt sees no
	// matching tool response and falls through to drift.
	mock.SetToolResponse("Attempt: 2/", []llm.ToolCall{toolCallFromAction(types.HTTPAction{
		Method: "GET", URL: server.URL + "/api/data",
	})})
	sessionID := createTestSession(t, s)

	plan := &TestPlan{
		Goal: "single drift tolerance",
		Cases: []TestCase{
			{ID: "t1", Name: "single drift", Target: "verify one drift is tolerated", Expectation: "ok"},
		},
	}

	results, err := loop.ExecutePlan(context.Background(), plan, sessionID)
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.Equal(t, StepPassed, results[0].Status,
		"a single drift followed by a real action must not escalate; the case passes when the action succeeds")
	assert.Equal(t, 2, results[0].Attempts, "first attempt drifts, second attempt passes")
}

// recordingMockClient captures the last prompt it received for inspection
// while delegating LLM calls (including tool-call responses) to an embedded
// MockClient. SetToolResponse propagates so the tool-calling steer path can be
// fixture-driven.
type recordingMockClient struct {
	mu         sync.Mutex
	lastPrompt string
	*llm.MockClient
}

func newRecordingMockClient(responses map[string]string) *recordingMockClient {
	return &recordingMockClient{MockClient: llm.NewMockClient(responses)}
}

func (r *recordingMockClient) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	r.mu.Lock()
	var contents []string
	for _, msg := range req.Messages {
		contents = append(contents, msg.Content)
	}
	r.lastPrompt = strings.Join(contents, "\n\n")
	r.mu.Unlock()
	return r.MockClient.Complete(ctx, req)
}

func (r *recordingMockClient) CompleteWithVision(ctx context.Context, prompt string, images [][]byte) (*llm.Response, error) {
	return r.MockClient.CompleteWithVision(ctx, prompt, images)
}

func (r *recordingMockClient) Stream(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
	return r.MockClient.Stream(ctx, req)
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

	recordingClient := newRecordingMockClient(responses)
	driver := ai.NewDriver(recordingClient, ai.NewTokenBudget(200000, 10000))

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

	loop, s, recordingClient := testLoopWithRecordingClient(t,
		nil,
		[]project.Service{{Name: "gateway", URL: server.URL}})
	recordingClient.SetToolResponse("default", []llm.ToolCall{toolCallFromAction(types.HTTPAction{
		Method: "GET", URL: "/api/data",
	})})
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

// TestSteer_AssembleErrorFallsBackToDrift verifies the defensive drift fallback:
// when assembleAction fails (e.g., a typo in the tool name from the provider),
// steer treats it as drift and returns WaitAction + zeroCall=true rather than
// hard-failing the case. This ensures the loop's consecutive-zero-call escalation
// still fires on a recoverable glitch.
func TestSteer_AssembleErrorFallsBackToDrift(t *testing.T) {
	loop, _, mock := testLoop(t, nil, nil)
	// Preset a tool call with a bogus name that assembleAction will reject.
	mock.SetToolResponse("default", []llm.ToolCall{{Name: "api_request_typo", Input: map[string]any{"method": "GET", "url": "/x"}}})

	tc := &TestCase{ID: "tc-3", Name: "bad tool", Target: "/api/x", Expectation: "e"}

	action, zeroCall, err := loop.steer(context.Background(), tc, nil, 1)

	require.NoError(t, err, "assembleAction error must be handled gracefully as drift, not surfaced as an error")
	assert.True(t, zeroCall, "an unassembleable tool call should be treated as drift")
	_, isWait := action.(types.WaitAction)
	assert.True(t, isWait, "drift fallback should be a WaitAction")
}
