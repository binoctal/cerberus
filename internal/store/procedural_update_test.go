package store_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/store"
)

func TestApplyProceduralEMA_AtomicOnce(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../migrations"))

	m, err := s.StoreProceduralWithType(ctx, "n", "c", "a", "p", "cat", "failure", nil, embed.NewTrigramProvider(embed.DefaultDimension).ModelName())
	require.NoError(t, err)

	// One grouped update: signal 0.5, delta 3 cases → e = 0.7*0.5 + 0.3*0.5 = 0.5, usage 3.
	require.NoError(t, s.ApplyProceduralEMA(ctx, m.ID, 0.5, 3))

	var eff float64
	var usage int
	require.NoError(t, s.DB().QueryRowContext(ctx,
		`SELECT effectiveness, usage_count FROM memory_procedural WHERE id=?`, m.ID).Scan(&eff, &usage))
	require.InDelta(t, 0.5, eff, 0.001)
	require.Equal(t, 3, usage)
}
