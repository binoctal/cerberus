package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/types"
)

// TestReActLoop_PureWaitNotJudgedAsPass reproduces a bug where the ReAct loop
// judges a test case as passed whenever any steer action succeeds. A pure
// duration wait (the action the parse fallback emits under pressure) always
// succeeds, yet it performs no verification of the system under test — so a
// case that only ever waits must not be marked pass.
func TestReActLoop_PureWaitNotJudgedAsPass(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK) // unused: a pure wait never hits HTTP
	}))
	defer server.Close()

	waitJSON, _ := json.Marshal(SteerOutput{
		Reasoning: "wait before retrying",
		Envelope: types.ActionEnvelope{
			Type: types.ActionWait,
			Raw:  mustJSON(types.WaitAction{Duration: "1s"}), // pure duration, no selector
		},
	})

	loop, s := testLoop(t, map[string]string{"default": string(waitJSON)}, server)
	sessionID := createTestSession(t, s)

	plan := &TestPlan{
		Goal: "wait must not pass",
		Cases: []TestCase{
			{ID: "t1", Name: "wait", Target: "verify something", Expectation: "should not pass on wait"},
		},
	}

	results, err := loop.ExecutePlan(context.Background(), plan, sessionID)
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.NotEqual(t, StepPassed, results[0].Status,
		"a pure wait performs no verification and must not be judged as pass")
}
