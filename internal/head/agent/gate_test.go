package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/escalation"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
)

// recordingGate records all events and returns continue.
type recordingGate struct {
	mu     sync.Mutex
	events []escalation.Event
}

func (r *recordingGate) Check(_ context.Context, event escalation.Event) escalation.Decision {
	r.mu.Lock()
	r.events = append(r.events, event)
	r.mu.Unlock()
	return escalation.EscalationContinue
}

func (r *recordingGate) Events() []escalation.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]escalation.Event{}, r.events...)
}

func setupGateTest(t *testing.T) (*ReActLoop, *recordingGate, *store.Store) {
	t.Helper()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	gate := &recordingGate{}
	engine := NewRuleEngine([]project.Service{{Name: "default", URL: "http://localhost:9999"}}, nil, ".")
	exec := BuildMultiExecutor(".", nil, nil, gate, zap.NewNop())
	emb := embed.NewTrigramProvider(embed.DefaultDimension)
	loop := NewReActLoopWithGateWithConfig(ReActLoopConfig{
		Driver:   nil,
		Store:    s,
		Engine:   engine,
		Executor: exec,
		Config:   DefaultReActConfig(),
		Gate:     gate,
		Logger:   zap.NewNop(),
		Embedder: emb,
	})
	return loop, gate, s
}

func TestExecutePlan_TracksConsecutiveFailures(t *testing.T) {
	loop, gate, s := setupGateTest(t)
	ctx := context.Background()

	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../../migrations"))
	sessionID := createTestSession(t, s)

	plan := &TestPlan{
		Goal:       "test systemic failure",
		ProjectURL: "http://localhost:9999",
		Cases: []TestCase{
			// Deterministic failures (closed port, no LLM path): each case's
			// only step dials a port nothing listens on.
			{ID: "tc-1", Name: "fail 1", Target: "http://127.0.0.1:1", Steps: []TestStep{{Action: "http_request", URL: "http://127.0.0.1:1/api/fail", Method: "GET", ExpectStatusClass: "2xx"}}},
			{ID: "tc-2", Name: "fail 2", Target: "http://127.0.0.1:1", Steps: []TestStep{{Action: "http_request", URL: "http://127.0.0.1:1/api/fail", Method: "GET", ExpectStatusClass: "2xx"}}},
			{ID: "tc-3", Name: "fail 3", Target: "http://127.0.0.1:1", Steps: []TestStep{{Action: "http_request", URL: "http://127.0.0.1:1/api/fail", Method: "GET", ExpectStatusClass: "2xx"}}},
			{ID: "tc-4", Name: "fail 4", Target: "http://127.0.0.1:1", Steps: []TestStep{{Action: "http_request", URL: "http://127.0.0.1:1/api/fail", Method: "GET", ExpectStatusClass: "2xx"}}},
			{ID: "tc-5", Name: "fail 5", Target: "http://127.0.0.1:1", Steps: []TestStep{{Action: "http_request", URL: "http://127.0.0.1:1/api/fail", Method: "GET", ExpectStatusClass: "2xx"}}},
		},
	}

	results, err := loop.ExecutePlan(ctx, plan, sessionID)
	assert.NoError(t, err)
	assert.Len(t, results, 5)

	events := gate.Events()
	hasSystemicFailure := false
	for _, e := range events {
		if e.Type == "systemic_failure" {
			hasSystemicFailure = true
		}
	}
	assert.True(t, hasSystemicFailure, "expected systemic_failure escalation after 5 consecutive failures")
}

// TestExecutePlan_SkipsDoNotEscalateSystemicFailure: a skip is a decision not
// to assert (empty-list param chains), not a failure signal. Long runs of
// consecutive skips — the http sweep hits dozens of empty admin lists in a
// row — must not trip the systemic-failure gate, which exists for failure
// streaks.
func TestExecutePlan_SkipsDoNotEscalateSystemicFailure(t *testing.T) {
	loop, gate, s := setupGateTest(t)
	ctx := context.Background()

	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../../migrations"))
	sessionID := createTestSession(t, s)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"things":[]}`))
	}))
	t.Cleanup(srv.Close)

	cases := make([]TestCase, 5)
	for i := range cases {
		cases[i] = TestCase{
			ID: fmt.Sprintf("tc-skip-%d", i+1), Name: "empty list chain", Target: srv.URL,
			Steps: []TestStep{{
				Action: "http_request", URL: srv.URL + "/api/things", Method: "GET",
				ExpectStatusClass: "2xx",
				Capture:           map[string]string{"things.0.id": "p_id"},
			}},
		}
	}
	plan := &TestPlan{Goal: "skips are not failures", ProjectURL: srv.URL, Cases: cases}

	results, err := loop.ExecutePlan(ctx, plan, sessionID)
	require.NoError(t, err)
	require.Len(t, results, 5)
	for _, r := range results {
		if r.Status != StepSkipped {
			t.Logf("case %s status=%s error=%v", r.TestCase.ID, r.Status, r.Error)
		}
		assert.Equal(t, StepSkipped, r.Status, "case %s must skip on the empty list", r.TestCase.ID)
	}
	for _, e := range gate.Events() {
		assert.NotEqual(t, "systemic_failure", e.Type, "consecutive skips must not escalate as systemic failure")
	}
}
