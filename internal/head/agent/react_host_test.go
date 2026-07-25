package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/types"
)

// TestReActLoop_SteerInjectsServiceHostHeader reproduces the bug where the
// ReAct execution path injects the actor's Authorization but NOT the service's
// Host header. The gateway routes by Host, so a steered request without the
// configured Host lands on the wrong domain (unknown domain). The service's
// Host header must be injected on the ReAct path just as it is on the rule
// engine path.
func TestReActLoop_SteerInjectsServiceHostHeader(t *testing.T) {
	var seenHost atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenHost.Store(r.Host)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	loop, s, mock := testLoopWithServices(t, nil,
		[]project.Service{{Name: "gateway", URL: server.URL, Headers: map[string]string{"Host": "api.modelsite.ai"}}}, nil)
	mock.SetToolResponse("default", []llm.ToolCall{toolCallFromAction(types.HTTPAction{
		Method: "GET", URL: "/api/data",
	})})
	sessionID := createTestSession(t, s)

	plan := &TestPlan{Goal: "g", Cases: []TestCase{
		{ID: "t1", Target: "verify", Service: "gateway", Expectation: "ok"},
	}}
	_, err := loop.ExecutePlan(context.Background(), plan, sessionID)
	require.NoError(t, err)

	require.Equal(t, "api.modelsite.ai", seenHost.Load(),
		"ReAct steer must inject the service's Host header so domain-routed gateways accept the request")
}
