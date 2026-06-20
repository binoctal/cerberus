package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMemoryUsage_Record(t *testing.T) {
	ctx := context.Background()
	s, err := New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	require.NoError(t, RunMigrations(ctx, s.DB(), "../../migrations"))

	// First record
	err = s.RecordMemoryUsage(ctx, 1, "sess-1", "case-1", "target-1", 1)
	require.NoError(t, err)

	// Verify row exists
	var n int
	err = s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_usage WHERE session_id='sess-1'`).Scan(&n)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	// Second record with SAME (session, case, procedural) → should be ignored by UNIQUE constraint
	err = s.RecordMemoryUsage(ctx, 1, "sess-1", "case-1", "target-2", 2)
	require.NoError(t, err)

	// Still only 1 row
	err = s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_usage WHERE session_id='sess-1'`).Scan(&n)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	// Different procedural_id → new row
	err = s.RecordMemoryUsage(ctx, 2, "sess-1", "case-1", "target-1", 1)
	require.NoError(t, err)

	err = s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_usage WHERE session_id='sess-1'`).Scan(&n)
	require.NoError(t, err)
	require.Equal(t, 2, n)
}

func TestMemoryUsage_Unconsolidated(t *testing.T) {
	ctx := context.Background()
	s, err := New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	require.NoError(t, RunMigrations(ctx, s.DB(), "../../migrations"))

	// Record some usage
	err = s.RecordMemoryUsage(ctx, 1, "sess-1", "case-1", "target-1", 1)
	require.NoError(t, err)
	err = s.RecordMemoryUsage(ctx, 2, "sess-1", "case-2", "target-2", 1)
	require.NoError(t, err)
	err = s.RecordMemoryUsage(ctx, 3, "sess-2", "case-3", "target-3", 1)
	require.NoError(t, err)

	// UnconsolidatedUsage returns only sess-1 rows
	rows, err := s.UnconsolidatedUsage(ctx, "sess-1")
	require.NoError(t, err)
	require.Len(t, rows, 2)

	require.Equal(t, int64(1), rows[0].ProceduralID)
	require.Equal(t, "sess-1", rows[0].SessionID)
	require.Equal(t, "case-1", rows[0].CaseID)
	require.Equal(t, "target-1", rows[0].Target)
	require.Equal(t, 1, rows[0].Attempt)

	require.Equal(t, int64(2), rows[1].ProceduralID)
	require.Equal(t, "sess-1", rows[1].SessionID)
	require.Equal(t, "case-2", rows[1].CaseID)
}

func TestMemoryUsage_MarkConsolidated(t *testing.T) {
	ctx := context.Background()
	s, err := New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	require.NoError(t, RunMigrations(ctx, s.DB(), "../../migrations"))

	// Record usage
	err = s.RecordMemoryUsage(ctx, 1, "sess-1", "case-1", "target-1", 1)
	require.NoError(t, err)
	err = s.RecordMemoryUsage(ctx, 2, "sess-1", "case-2", "target-2", 1)
	require.NoError(t, err)

	// Get IDs to consolidate
	rows, err := s.UnconsolidatedUsage(ctx, "sess-1")
	require.NoError(t, err)
	require.Len(t, rows, 2)

	ids := []int64{rows[0].ID, rows[1].ID}
	err = s.MarkUsageConsolidated(ctx, ids)
	require.NoError(t, err)

	// Now unconsolidated returns empty
	rows, err = s.UnconsolidatedUsage(ctx, "sess-1")
	require.NoError(t, err)
	require.Len(t, rows, 0)

	// Verify consolidated_at is set
	var count int
	err = s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_usage WHERE consolidated_at IS NOT NULL`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 2, count)
}
