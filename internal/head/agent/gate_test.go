package agent

import (
	"context"
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
	exec := BuildMultiExecutor(".", nil, gate, zap.NewNop())
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

	plan := &TestPlan{
		Goal:       "test systemic failure",
		ProjectURL: "http://localhost:9999",
		Cases: []TestCase{
			{ID: "tc-1", Name: "fail 1", Target: "/api/fail", Method: "GET", Expectation: "should fail"},
			{ID: "tc-2", Name: "fail 2", Target: "/api/fail", Method: "GET", Expectation: "should fail"},
			{ID: "tc-3", Name: "fail 3", Target: "/api/fail", Method: "GET", Expectation: "should fail"},
			{ID: "tc-4", Name: "fail 4", Target: "/api/fail", Method: "GET", Expectation: "should fail"},
			{ID: "tc-5", Name: "fail 5", Target: "/api/fail", Method: "GET", Expectation: "should fail"},
		},
	}

	results, err := loop.ExecutePlan(ctx, plan, "test-session")
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
