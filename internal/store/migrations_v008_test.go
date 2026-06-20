package store_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/store"
)

func TestV008_AppliesAndDedups(t *testing.T) {
	ctx := context.Background()

	// Create a temporary directory for migrations without V008
	tmpDir, err := os.MkdirTemp("", "migrations_test_*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Copy V001-V007 migration files to temp directory
	migrationsDir := "../../migrations"
	entries, err := os.ReadDir(migrationsDir)
	require.NoError(t, err)

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		// Skip V008
		matched, err := filepath.Match("V008*", e.Name())
		require.NoError(t, err)
		if matched {
			continue
		}
		// Copy other migration files
		content, err := os.ReadFile(filepath.Join(migrationsDir, e.Name()))
		require.NoError(t, err)
		err = os.WriteFile(filepath.Join(tmpDir, e.Name()), content, 0644)
		require.NoError(t, err)
	}

	// Create store and run migrations up to V007
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, store.RunMigrations(ctx, s.DB(), tmpDir))

	// Seed two duplicate (project, condition, action) rows directly.
	_, err = s.DB().ExecContext(ctx, `INSERT INTO memory_procedural
		(name, condition, action, effectiveness, usage_count, project_name, category, type, archived, created_at)
		VALUES ('n','C','A',0.5,0,'p','c','failure',0,'2026-06-01T00:00:00Z'),
		       ('n','C','A',0.4,3,'p','c','failure',0,'2026-06-20T00:00:00Z')`)
	require.NoError(t, err)

	// Verify duplicates exist before V008
	var n int
	require.NoError(t, s.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_procedural WHERE project_name='p' AND condition='C' AND action='A'`).Scan(&n))
	require.Equal(t, 2, n, "should have 2 duplicate rows before V008")

	// Now run V008 migration from the full migrations directory
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../migrations"))

	// Re-running migrations must succeed (idempotent) and the unique index must exist.
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../migrations"))

	// Duplicates should have been collapsed to 1 (newest by created_at)
	require.NoError(t, s.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_procedural WHERE project_name='p' AND condition='C' AND action='A'`).Scan(&n))
	require.Equal(t, 1, n, "duplicate procedural rows should collapse to the newest")

	// Verify the remaining row is the newest (usage_count=3, created_at='2026-06-20')
	var usageCount int
	require.NoError(t, s.DB().QueryRowContext(ctx,
		`SELECT usage_count FROM memory_procedural WHERE project_name='p' AND condition='C' AND action='A'`).Scan(&usageCount))
	require.Equal(t, 3, usageCount, "should keep the newest row")

	// Columns added by V008 exist.
	for _, col := range []string{"embedding", "embedding_model"} {
		var v string
		require.NoError(t, s.DB().QueryRowContext(ctx,
			fmt.Sprintf("SELECT COALESCE(%s,'') FROM memory_procedural LIMIT 1", col)).Scan(&v),
			"column %s should exist", col)
	}

	// Unique index exists
	var indexName string
	err = s.DB().QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_procedural_dedup'`).Scan(&indexName)
	require.NoError(t, err, "unique index idx_procedural_dedup should exist")
	require.Equal(t, "idx_procedural_dedup", indexName)

	// Verify unique constraint works (trying to insert duplicate should fail)
	_, err = s.DB().ExecContext(ctx, `INSERT INTO memory_procedural
		(name, condition, action, effectiveness, usage_count, project_name, category, type, archived, created_at)
		VALUES ('n2','C','A',0.6,1,'p','c','failure',0,'2026-06-25T00:00:00Z')`)
	require.Error(t, err, "unique constraint should prevent duplicate insertions")
	require.Contains(t, err.Error(), "UNIQUE constraint failed")
}
