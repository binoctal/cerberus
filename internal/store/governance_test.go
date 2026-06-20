package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/binoctal/cerberus/internal/store"
	"github.com/stretchr/testify/require"
)

func TestGovernance_ArchivesByPolicy(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../migrations"))

	old := time.Now().Add(-(31 * 24 * time.Hour)).UTC().Format(time.RFC3339)
	// L3: low effectiveness, used 5x, old → archived.
	_, err = s.DB().ExecContext(ctx, `INSERT INTO memory_procedural
		(name,condition,action,effectiveness,usage_count,project_name,category,type,archived,created_at)
		VALUES ('n','c','a',0.2,5,'p','cat','failure',0,?)`, old)
	require.NoError(t, err)

	n, err := s.AutoArchiveLowEffectiveness(ctx, "p")
	require.NoError(t, err)
	require.Equal(t, 1, n)

	// L3: rare-useless (usage<2, old>90d) → archived via the second clause.
	veryOld := time.Now().Add(-(91 * 24 * time.Hour)).UTC().Format(time.RFC3339)
	_, err = s.DB().ExecContext(ctx, `INSERT INTO memory_procedural
		(name,condition,action,effectiveness,usage_count,project_name,category,type,archived,created_at)
		VALUES ('n2','c2','a2',0.6,1,'p','cat','failure',0,?)`, veryOld)
	require.NoError(t, err)
	n, err = s.AutoArchiveLowEffectiveness(ctx, "p")
	require.NoError(t, err)
	require.GreaterOrEqual(t, n, 1)
}

func TestGovernance_ReadFiltersExcludeArchived(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../migrations"))

	// Create a session first to satisfy foreign key constraint.
	sess, err := s.CreateSession(ctx, "run", "test session", "test-project")
	require.NoError(t, err)

	// Insert archived episodic memory.
	_, err = s.DB().ExecContext(ctx,
		`INSERT INTO memory_episodic (session_id, target, status, verdict, duration_ms, archived, created_at)
		 VALUES (?, 'target1', 'pass', 'pass', 100, 1, '2024-01-01T00:00:00Z')`, sess.ID)
	require.NoError(t, err)

	// Insert active episodic memory.
	_, err = s.DB().ExecContext(ctx,
		`INSERT INTO memory_episodic (session_id, target, status, verdict, duration_ms, archived, created_at)
		 VALUES (?, 'target1', 'pass', 'pass', 100, 0, '2024-01-01T00:00:00Z')`, sess.ID)
	require.NoError(t, err)

	// GetEpisodicByTarget should only return active (archived=0) memories.
	memories, err := s.GetEpisodicByTarget(ctx, "target1", 10)
	require.NoError(t, err)
	require.Len(t, memories, 1, "should filter out archived episodic memories")
}
