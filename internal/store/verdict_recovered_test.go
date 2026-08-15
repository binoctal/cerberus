package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerdict_RecoveredRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, err := New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	require.NoError(t, RunMigrations(ctx, s.DB(), "../../migrations"))

	sess, err := s.CreateSession(ctx, "run", "test goal", "")
	require.NoError(t, err)
	traceID, err := s.CreateTrace(ctx, sess.ID, "ws", "ws://h/ws")
	require.NoError(t, err)

	// Recovered verdict persists recovered=true; a normal verdict stays false.
	_, err = s.CreateVerdict(ctx, sess.ID, traceID, "ws://h/ws", "" , "pass", 0.9, "judge", "r1", nil, FailureReasonNone, true, "", "")
	require.NoError(t, err)
	_, err = s.CreateVerdict(ctx, sess.ID, traceID, "ws://h/ws", "" , "fail", 0.4, "judge", "r2", nil, FailureReasonAssertionFailed, false, "", "")
	require.NoError(t, err)

	got, err := s.GetVerdicts(ctx, sess.ID)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.True(t, got[0].Recovered, "first verdict round-trips recovered=true")
	assert.False(t, got[1].Recovered, "second verdict stays recovered=false")
}

// TestVerdict_NonUnitRoundTrip asserts fallback_for/replaces persist and read
// back, and default to empty for a unit verdict (back-compat with pre-V012 rows
// is exercised by COALESCE in the SELECT).
func TestVerdict_NonUnitRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, err := New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	require.NoError(t, RunMigrations(ctx, s.DB(), "../../migrations"))

	sess, err := s.CreateSession(ctx, "run", "test goal", "")
	require.NoError(t, err)
	traceID, err := s.CreateTrace(ctx, sess.ID, "ws", "ws://h/ws")
	require.NoError(t, err)

	// Fallback verdict carries fallback_for; a replacement carries replaces; a
	// primary/unit verdict carries neither.
	_, err = s.CreateVerdict(ctx, sess.ID, traceID, "ws://h/ws", "" , "pass", 0.9, "judge", "fallback", nil, FailureReasonNone, false, "tc-1", "")
	require.NoError(t, err)
	_, err = s.CreateVerdict(ctx, sess.ID, traceID, "ws://h/ws", "" , "pass", 0.9, "judge", "replacement", nil, FailureReasonNone, false, "", "tc-2")
	require.NoError(t, err)
	_, err = s.CreateVerdict(ctx, sess.ID, traceID, "ws://h/ws", "" , "fail", 0.4, "judge", "primary", nil, FailureReasonAssertionFailed, false, "", "")
	require.NoError(t, err)

	got, err := s.GetVerdicts(ctx, sess.ID)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, "tc-1", got[0].FallbackFor, "fallback_for round-trips")
	assert.Equal(t, "", got[0].Replaces)
	assert.Equal(t, "", got[1].FallbackFor)
	assert.Equal(t, "tc-2", got[1].Replaces, "replaces round-trips")
	assert.Equal(t, "", got[2].FallbackFor, "unit verdict has empty fallback_for")
	assert.Equal(t, "", got[2].Replaces, "unit verdict has empty replaces")
}
