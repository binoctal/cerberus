package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExecutePlan_SkipsDeprioritizedCases verifies that cases Scout
// deprioritized (priority <= 0) are skipped instead of executed, avoiding
// wasted work on bogus targets.
func TestExecutePlan_SkipsDeprioritizedCases(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	loop, s, _ := testLoop(t, nil, server)
	sessionID := createTestSession(t, s)

	plan := &TestPlan{
		Goal: "t",
		Cases: []TestCase{
			{ID: "keep", Target: "/api/users", Method: "GET", Priority: 0.9, Expectation: "200"},
			{ID: "skip", Target: "badpath", Priority: -1, Expectation: "x"},
		},
		ProjectURL: server.URL,
	}

	results, err := loop.ExecutePlan(context.Background(), plan, sessionID)
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.NotEqual(t, StepSkipped, results[0].Status, "priority>0 case executes")
	assert.Equal(t, StepSkipped, results[1].Status, "priority<=0 case is skipped")
}
