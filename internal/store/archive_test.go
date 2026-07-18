package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupArchiveStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(":memory:")
	require.NoError(t, err)
	require.NoError(t, RunMigrations(context.Background(), s.DB(), "../../migrations"))
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// ArchiveStaleEpisodic flips archived=1 only on rows older than maxAgeDays that
// are not already archived, and returns the count touched.
func TestArchiveStaleEpisodic(t *testing.T) {
	s := setupArchiveStore(t)
	ctx := context.Background()
	sess, err := s.CreateSession(ctx, "run", "g", "p")
	require.NoError(t, err)

	insertEpisodic := func(createdExpr string) {
		_, err := s.DB().ExecContext(ctx,
			`INSERT INTO memory_episodic (session_id, target, status, created_at, archived)
			 VALUES (?, 't', 'pass', datetime('now', ?), 0)`,
			sess.ID, createdExpr)
		require.NoError(t, err)
	}
	insertEpisodic("-100 days") // old → archived
	insertEpisodic("-10 days")  // recent → not archived
	insertEpisodic("-100 days") // old → archived

	n, err := s.ArchiveStaleEpisodic(ctx, 30)
	require.NoError(t, err)
	assert.Equal(t, 2, n, "only the two rows older than 30 days get archived")

	// Idempotent: re-running finds nothing new.
	n2, err := s.ArchiveStaleEpisodic(ctx, 30)
	require.NoError(t, err)
	assert.Equal(t, 0, n2)
}

func TestArchiveStaleSemantic(t *testing.T) {
	s := setupArchiveStore(t)
	ctx := context.Background()

	insertSem := func(createdExpr string) {
		_, err := s.DB().ExecContext(ctx,
			`INSERT INTO memory_semantic (content, source, tags, confidence, created_at, updated_at, archived)
			 VALUES ('c', 's', '[]', 0.5, datetime('now', ?), datetime('now'), 0)`,
			createdExpr)
		require.NoError(t, err)
	}
	insertSem("-100 days") // old → archived
	insertSem("-5 days")   // recent → not archived

	n, err := s.ArchiveStaleSemantic(ctx, 30)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	n2, err := s.ArchiveStaleSemantic(ctx, 30)
	require.NoError(t, err)
	assert.Equal(t, 0, n2)
}
