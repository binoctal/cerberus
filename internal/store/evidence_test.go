package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetEvidenceBySession(t *testing.T) {
	s, err := New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	require.NoError(t, RunMigrations(ctx, s.DB(), "../../migrations"))

	sess, err := s.CreateSession(ctx, "run", "evidence test", "project")
	require.NoError(t, err)

	trace1, err := s.CreateTrace(ctx, sess.ID, "http", "GET /a")
	require.NoError(t, err)

	trace2, err := s.CreateTrace(ctx, sess.ID, "http", "GET /b")
	require.NoError(t, err)

	_, err = s.CreateEvidence(ctx, trace1, "screenshot", "base64...")
	require.NoError(t, err)
	_, err = s.CreateEvidence(ctx, trace1, "screenshot", "base64...")
	require.NoError(t, err)
	_, err = s.CreateEvidence(ctx, trace2, "screenshot", "base64...")
	require.NoError(t, err)

	result, err := s.GetEvidenceBySession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Len(t, result[trace1], 2)
	assert.Len(t, result[trace2], 1)

	// Unrelated session returns empty map.
	other, err := s.CreateSession(ctx, "run", "other session", "project")
	require.NoError(t, err)

	otherResult, err := s.GetEvidenceBySession(ctx, other.ID)
	require.NoError(t, err)
	assert.Empty(t, otherResult)
}
