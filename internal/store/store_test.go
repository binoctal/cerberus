package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionCRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	s, cleanup := testStore(t)
	defer cleanup()
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "test all APIs", "my-project")
	require.NoError(t, err)
	assert.NotEmpty(t, sess.ID)
	assert.Equal(t, "running", sess.Status)
	assert.Equal(t, "run", sess.Mode)

	got, err := s.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, sess.ID, got.ID)
	assert.Equal(t, "test all APIs", got.Goal)

	err = s.UpdateSessionStatus(ctx, sess.ID, "completed")
	require.NoError(t, err)
	got, err = s.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, "completed", got.Status)
	assert.NotNil(t, got.FinishedAt)

	sessions, err := s.ListSessions(ctx, 10)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(sessions), 1)
}

func TestTraceCRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	s, cleanup := testStore(t)
	defer cleanup()
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "trace test", "")
	require.NoError(t, err)

	traceID, err := s.CreateTrace(ctx, sess.ID, "api", "GET /api/v1/users")
	require.NoError(t, err)
	assert.Greater(t, traceID, int64(0))

	err = s.FinishTrace(ctx, traceID, "pass")
	require.NoError(t, err)

	traces, err := s.GetTraces(ctx, sess.ID)
	require.NoError(t, err)
	require.Len(t, traces, 1)
	assert.Equal(t, "pass", traces[0].Status)
	assert.Equal(t, "api", traces[0].Category)
}

func TestVerdictCRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	s, cleanup := testStore(t)
	defer cleanup()
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "verdict test", "")
	require.NoError(t, err)
	traceID, err := s.CreateTrace(ctx, sess.ID, "api", "POST /api/v1/users")
	require.NoError(t, err)

	v, err := s.CreateVerdict(ctx, sess.ID, traceID, "POST /api/v1/users",
		"pass", 0.95, "judge", "Response matches expected schema", nil)
	require.NoError(t, err)
	assert.Greater(t, v.ID, int64(0))
	assert.Equal(t, 0.95, v.Confidence)

	verdicts, err := s.GetVerdicts(ctx, sess.ID)
	require.NoError(t, err)
	require.Len(t, verdicts, 1)
	assert.Equal(t, "pass", verdicts[0].Status)
	assert.Equal(t, "judge", verdicts[0].Source)
}

func TestEpisodicMemory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	s, cleanup := testStore(t)
	defer cleanup()
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "memory test", "")
	require.NoError(t, err)

	err = s.RecordEpisodic(ctx, sess.ID, "GET /api/v1/users", "pass",
		map[string]any{"status_code": 200}, 2*time.Second)
	require.NoError(t, err)

	memories, err := s.GetEpisodicByTarget(ctx, "GET /api/v1/users", 10)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(memories), 1)
	assert.Equal(t, "pass", memories[0].Status)
}
