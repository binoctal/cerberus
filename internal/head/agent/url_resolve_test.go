package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/types"
)

// TestReActLoop_RelativeURLResolvedAgainstBaseURL reproduces a bug where a
// ReAct-steered HTTP action carries only a server-relative URL (e.g.
// "/v1/chat/completions"), which is what the LLM and the parse fallback emit
// when they copy the test case's path-only target. Without resolution against
// the configured base URL the request cannot connect (HTTP 0 in production
// logs) and the service-level headers — matched by URL host — never inject.
//
// Expected: the relative URL is resolved against the engine's base URL before
// execution, so the request reaches the target server.
func TestReActLoop_RelativeURLResolvedAgainstBaseURL(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	// LLM steers to a RELATIVE path (no scheme/host), mimicking the fallback /
	// LLM path that produced HTTP 0 in production.
	steerJSON, _ := json.Marshal(makeSteerEnvelope("hit endpoint", "GET", "/api/data"))

	loop, s := testLoop(t, map[string]string{"default": string(steerJSON)}, server)
	sessionID := createTestSession(t, s)

	plan := &TestPlan{
		Goal: "test relative url resolution",
		Cases: []TestCase{
			{ID: "t1", Name: "relative", Target: "verify endpoint", Expectation: "ok"},
		},
	}

	results, err := loop.ExecutePlan(context.Background(), plan, sessionID)
	require.NoError(t, err)
	require.Len(t, results, 1)

	require.Equal(t, int32(1), hits.Load(),
		"relative URL must be resolved against base URL and reach the server exactly once")
	assert.Equal(t, StepPassed, results[0].Status)
}

// actionRecovery is a test double for Recovery that always returns a fixed
// action, letting recovery-path tests bypass the LLM.
type actionRecovery struct {
	action types.TypedAction
}

func (a *actionRecovery) Recover(ctx context.Context, tc TestCase, result types.ExecutorResult, attempt int) (RecoverDecision, error) {
	return RecoverDecision{Action: a.action}, nil
}

func (a *actionRecovery) SetSessionID(id string) {}

func (a *actionRecovery) SetProject(name string) {}

// TestReActLoop_RecoveryRelativeURLResolvedAgainstBaseURL reproduces the same
// relative-URL bug on the recovery path: when recovery returns an HTTP action
// whose URL is a server-relative path, it must be resolved against the base URL
// before execution. The steer target fails so recovery engages.
func TestReActLoop_RecoveryRelativeURLResolvedAgainstBaseURL(t *testing.T) {
	var recoveredHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/recovered" {
			recoveredHits.Add(1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError) // /fail and any other path
	}))
	defer server.Close()

	// Steer hits a failing endpoint so the ReAct loop falls through to recovery.
	steerJSON, _ := json.Marshal(makeSteerEnvelope("try failing endpoint", "GET", server.URL+"/fail"))

	loop, s := testLoop(t, map[string]string{"default": string(steerJSON)}, server)
	// Recovery emits a RELATIVE-url action (mimics LLM copying a path-only target).
	loop.recovery = &actionRecovery{action: types.HTTPAction{Method: "GET", URL: "/api/recovered"}}
	sessionID := createTestSession(t, s)

	plan := &TestPlan{
		Goal: "recovery relative url resolution",
		Cases: []TestCase{
			{ID: "t1", Name: "rec", Target: "verify recovered", Expectation: "ok"},
		},
	}

	_, err := loop.ExecutePlan(context.Background(), plan, sessionID)
	require.NoError(t, err)

	require.GreaterOrEqual(t, recoveredHits.Load(), int32(1),
		"recovery's relative URL must be resolved against base URL and reach the server")
}
