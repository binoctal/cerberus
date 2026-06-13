package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveAndLoadPlan(t *testing.T) {
	s, err := New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	require.NoError(t, RunMigrations(ctx, s.DB(), "../../migrations"))

	// Create a session to reference.
	sess, err := s.CreateSession(ctx, "run", "plan test", "project")
	require.NoError(t, err)

	// Save a plan.
	plan := map[string]any{
		"goal":  "test the API",
		"cases": []map[string]any{{"id": "tc-001", "target": "/health"}},
	}
	require.NoError(t, s.SavePlan(ctx, sess.ID, plan))

	// Load it back.
	var loaded map[string]any
	require.NoError(t, s.LoadPlan(ctx, sess.ID, &loaded))
	assert.Equal(t, "test the API", loaded["goal"])

	cases, ok := loaded["cases"].([]any)
	require.True(t, ok)
	require.Len(t, cases, 1)
}

func TestSavePlan_Upsert(t *testing.T) {
	s, err := New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	require.NoError(t, RunMigrations(ctx, s.DB(), "../../migrations"))

	sess, err := s.CreateSession(ctx, "run", "upsert test", "project")
	require.NoError(t, err)

	require.NoError(t, s.SavePlan(ctx, sess.ID, map[string]string{"v": "1"}))
	require.NoError(t, s.SavePlan(ctx, sess.ID, map[string]string{"v": "2"}))

	var loaded map[string]string
	require.NoError(t, s.LoadPlan(ctx, sess.ID, &loaded))
	assert.Equal(t, "2", loaded["v"])
}

func TestLoadPlan_NotFound(t *testing.T) {
	s, err := New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	require.NoError(t, RunMigrations(ctx, s.DB(), "../../migrations"))

	var loaded map[string]any
	err = s.LoadPlan(ctx, "nonexistent", &loaded)
	assert.Error(t, err)
}

func TestGetCompletedTargets(t *testing.T) {
	s, err := New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	require.NoError(t, RunMigrations(ctx, s.DB(), "../../migrations"))

	sess, err := s.CreateSession(ctx, "run", "targets test", "project")
	require.NoError(t, err)

	// Create traces and verdicts.
	trace1, _ := s.CreateTrace(ctx, sess.ID, "http", "GET /a")
	s.FinishTrace(ctx, trace1, "pass")
	s.CreateVerdict(ctx, sess.ID, trace1, "GET /a", "pass", 0.9, "judge", "ok", nil)

	trace2, _ := s.CreateTrace(ctx, sess.ID, "http", "GET /b")
	s.FinishTrace(ctx, trace2, "fail")
	s.CreateVerdict(ctx, sess.ID, trace2, "GET /b", "fail", 0.8, "judge", "err", nil)

	completed, err := s.GetCompletedTargets(ctx, sess.ID)
	require.NoError(t, err)
	assert.True(t, completed["GET /a"])
	assert.True(t, completed["GET /b"])
	assert.False(t, completed["GET /c"])
}

func TestGetCompletedTargets_Empty(t *testing.T) {
	s, err := New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	require.NoError(t, RunMigrations(ctx, s.DB(), "../../migrations"))

	sess, err := s.CreateSession(ctx, "run", "empty targets", "project")
	require.NoError(t, err)

	completed, err := s.GetCompletedTargets(ctx, sess.ID)
	require.NoError(t, err)
	assert.Empty(t, completed)
}

func TestHasPlan(t *testing.T) {
	s, err := New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	require.NoError(t, RunMigrations(ctx, s.DB(), "../../migrations"))

	sess, err := s.CreateSession(ctx, "run", "has plan", "project")
	require.NoError(t, err)

	has, err := s.HasPlan(ctx, sess.ID)
	require.NoError(t, err)
	assert.False(t, has)

	require.NoError(t, s.SavePlan(ctx, sess.ID, map[string]string{"goal": "test"}))

	has, err = s.HasPlan(ctx, sess.ID)
	require.NoError(t, err)
	assert.True(t, has)
}
