package store_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/store"
)

func TestSearchSemantic_FiltersByModel(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../migrations"))

	_, err = s.StoreSemantic(ctx, "auth login failure", "reflexion", "p", []string{"a"}, []float64{1, 0}, "trigram-v1")
	require.NoError(t, err)
	_, err = s.StoreSemantic(ctx, "auth login failure", "reflexion", "p", []string{"a"}, []float64{1, 0}, "old-model")
	require.NoError(t, err)

	res, err := s.SearchSemanticForProject(ctx, []float64{1, 0}, "p", 5, 0.0, "trigram-v1")
	require.NoError(t, err)
	require.Len(t, res, 1, "only rows matching the current model should be recalled")
	require.Equal(t, "trigram-v1", res[0].EmbeddingModel)
}
