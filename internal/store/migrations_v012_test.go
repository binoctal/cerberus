package store_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/store"
)

// TestV012_AddsNonUnitColumns asserts the verdicts table carries the
// fallback_for and replaces columns after migration, and that re-running
// migrations is idempotent.
func TestV012_AddsNonUnitColumns(t *testing.T) {
	ctx := context.Background()

	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../migrations"))

	for _, col := range []string{"fallback_for", "replaces"} {
		var name string
		require.NoError(t, s.DB().QueryRowContext(ctx,
			fmt.Sprintf("SELECT name FROM pragma_table_info('verdicts') WHERE name='%s'", col)).Scan(&name),
			"column %s should exist on verdicts", col)
		require.Equal(t, col, name)
	}

	// Re-running migrations must succeed (idempotent).
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../migrations"))
}
