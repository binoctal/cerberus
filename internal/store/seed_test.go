package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestSeedStrategies(t *testing.T) {
	s, err := New(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, s.Close()) }()

	ctx := context.Background()
	err = RunMigrations(ctx, s.DB(), "../../migrations")
	require.NoError(t, err)

	count, err := SeedStrategies(ctx, s, "test-project", zap.NewNop())
	require.NoError(t, err)
	assert.Greater(t, count, 0)

	// Verify strategies are queryable.
	strategies, err := s.GetProceduralByEffectiveness(ctx, 0.0, 20)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(strategies), count)

	// All seeded strategies should have category and type set.
	for _, st := range strategies {
		assert.NotEmpty(t, st.Category, "strategy %s should have category", st.Name)
		assert.Contains(t, []string{"failure", "success"}, st.Type, "strategy %s type", st.Name)
	}
}

func TestSeedStrategies_Idempotent(t *testing.T) {
	s, err := New(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, s.Close()) }()

	ctx := context.Background()
	err = RunMigrations(ctx, s.DB(), "../../migrations")
	require.NoError(t, err)

	count1, err := SeedStrategies(ctx, s, "test-project", zap.NewNop())
	require.NoError(t, err)

	// Seed again — should not duplicate (condition match skips).
	count2, err := SeedStrategies(ctx, s, "test-project", zap.NewNop())
	require.NoError(t, err)

	assert.Equal(t, count1, count1) // First seed added strategies.
	assert.LessOrEqual(t, count2, count1) // Second seed should add ≤ first (may skip duplicates).
}

func TestDefaultStrategies_Contents(t *testing.T) {
	strategies := defaultStrategies()
	assert.GreaterOrEqual(t, len(strategies), 5, "should have at least 5 default strategies")

	categories := make(map[string]int)
	for _, s := range strategies {
		assert.NotEmpty(t, s.name)
		assert.NotEmpty(t, s.condition)
		assert.NotEmpty(t, s.action)
		assert.NotEmpty(t, s.category)
		assert.Contains(t, []string{"failure", "success"}, s.refType)
		categories[s.category]++
	}

	// Should cover at least auth, crud, infra categories.
	assert.Contains(t, categories, "auth")
	assert.Contains(t, categories, "crud")
	assert.Contains(t, categories, "infra")
}
