package store_test

import (
	"context"
	"testing"

	"github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/stretchr/testify/require"
)

func TestGetProceduralByEmbedding(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../migrations"))

	emb := embed.NewTrigramProvider(embed.DefaultDimension)
	condVec, _ := emb.Embed(ctx, "post /api/v1/* returned 401")
	_, err = s.StoreProceduralWithType(ctx, "n", "post /api/v1/* returned 401", "retry auth", "p", "auth", "failure", condVec, emb.ModelName())
	require.NoError(t, err)

	q, _ := emb.Embed(ctx, "post /api/v1/login")
	got, err := s.GetProceduralByEmbedding(ctx, q, "p", 5, 0.1, emb.ModelName())
	require.NoError(t, err)
	require.Len(t, got, 1, "embedding recall should match the auth failure")
	require.Equal(t, "retry auth", got[0].Action)

	// Wrong model → not recalled.
	got0, err := s.GetProceduralByEmbedding(ctx, q, "p", 5, 0.1, "other-model")
	require.NoError(t, err)
	require.Empty(t, got0)
}
