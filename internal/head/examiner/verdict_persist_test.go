package examiner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/store"
)

func TestPersistFinalVerdicts_StoresRecovered(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../../migrations"))

	sess, err := s.CreateSession(ctx, "run", "g", "")
	require.NoError(t, err)
	traceID, err := s.CreateTrace(ctx, sess.ID, "ws", "ws://h/ws")
	require.NoError(t, err)

	verdicts := []FinalVerdict{
		{Status: StatusPass, StepResult: agent.StepResult{
			TestCase: &agent.TestCase{ID: "tc-fb", Target: "ws://h/ws"}, TraceID: traceID, Recovered: true}},
		{Status: StatusPass, StepResult: agent.StepResult{
			TestCase: &agent.TestCase{ID: "tc-ok", Target: "ws://h/ws"}, TraceID: traceID}},
	}

	n, err := PersistFinalVerdicts(ctx, s, zap.NewNop(), sess.ID, verdicts)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	got, err := s.GetVerdicts(ctx, sess.ID)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.True(t, got[0].Recovered, "recovered fallback persisted with recovered=true")
	assert.False(t, got[1].Recovered, "normal verdict stays recovered=false")
}

// An interrupt (SIGINT mid-examination) cancels the run context BEFORE the
// verdicts hit the store. Persistence detaches from that cancellation — a
// canceled ctx must not lose the session's whole outcome (dogfood
// 2026-08-21: 698 verdicts dropped this way).
func TestPersistFinalVerdicts_SurvivesCanceledContext(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../../migrations"))

	sess, err := s.CreateSession(ctx, "run", "g", "")
	require.NoError(t, err)
	traceID, err := s.CreateTrace(ctx, sess.ID, "ws", "ws://h/ws")
	require.NoError(t, err)

	canceled, cancel := context.WithCancel(ctx)
	cancel()

	verdicts := []FinalVerdict{
		{Status: StatusPass, StepResult: agent.StepResult{
			TestCase: &agent.TestCase{ID: "tc-ok", Target: "ws://h/ws"}, TraceID: traceID}},
	}

	n, err := PersistFinalVerdicts(canceled, s, zap.NewNop(), sess.ID, verdicts)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	got, err := s.GetVerdicts(ctx, sess.ID)
	require.NoError(t, err)
	require.Len(t, got, 1, "verdict persisted despite canceled run context")
}
