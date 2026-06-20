package store_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/store"
)

func TestStoreProceduralWithType_UpsertPreserves(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../migrations"))

	m1, err := s.StoreProceduralWithType(ctx, "n", "cond", "act", "p", "cat", "failure", []float64{0.1, 0.2}, "trigram-v1")
	require.NoError(t, err)
	// Simulate effectiveness earned later.
	require.NoError(t, s.UpdateProceduralEffectiveness(ctx, m1.ID, true))
	require.NoError(t, s.UpdateProceduralEffectiveness(ctx, m1.ID, true))

	// Upsert same (project, condition, action): must NOT duplicate, must preserve effectiveness.
	m2, err := s.StoreProceduralWithType(ctx, "n2", "cond", "act", "p", "cat2", "success", []float64{0.3, 0.4}, "trigram-v1")
	require.NoError(t, err)
	require.Equal(t, m1.ID, m2.ID, "upsert must return same row id")

	var count int
	require.NoError(t, s.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_procedural WHERE project_name='p' AND condition='cond' AND action='act'`).Scan(&count))
	require.Equal(t, 1, count)

	require.NotEqual(t, 0.5, m2.Effectiveness, "effectiveness must be preserved across upsert, not reset to default")
	assert.Equal(t, []float64{0.3, 0.4}, m2.Embedding, "embedding refreshed (always-embed)")
	assert.Equal(t, "cat2", m2.Category, "category refreshed on upsert")
}
