package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupSemanticStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	err = RunMigrations(ctx, s.DB(), "../../migrations")
	require.NoError(t, err)
	return s
}

func TestStoreSemantic_AndGetByID(t *testing.T) {
	s := setupSemanticStore(t)
	ctx := context.Background()

	emb := []float64{0.1, 0.2, 0.3}
	id, err := s.StoreSemantic(ctx, "API returns 404 for missing user", "test_run", "cerberus",
		[]string{"api", "error"}, emb, "trigram-v1")
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))

	got, err := s.GetSemanticByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "API returns 404 for missing user", got.Content)
	assert.Equal(t, "test_run", got.Source)
	assert.Equal(t, "cerberus", got.ProjectName)
	assert.Equal(t, []string{"api", "error"}, got.Tags)
	assert.InDeltaSlice(t, emb, got.Embedding, 1e-9)
	assert.Equal(t, "trigram-v1", got.EmbeddingModel)
}

func TestSearchSemantic_SortedByScore(t *testing.T) {
	s := setupSemanticStore(t)
	ctx := context.Background()

	// Store 3 records with different embeddings.
	records := []struct {
		content   string
		embedding []float64
	}{
		{"auth token required", []float64{1.0, 0.0, 0.0, 0.0}},
		{"authentication via OAuth2", []float64{0.9, 0.1, 0.0, 0.0}},
		{"weather forecast", []float64{0.0, 0.0, 1.0, 0.0}},
	}
	for _, r := range records {
		_, err := s.StoreSemantic(ctx, r.content, "test", "proj", nil, r.embedding, "test")
		require.NoError(t, err)
	}

	// Query with a vector similar to "auth" records.
	query := []float64{1.0, 0.0, 0.0, 0.0}
	results, err := s.SearchSemantic(ctx, query, 10, 0.5)
	require.NoError(t, err)

	assert.Len(t, results, 2, "only auth-related records should match threshold")
	assert.Equal(t, "auth token required", results[0].Content)
	assert.Equal(t, "authentication via OAuth2", results[1].Content)
	assert.GreaterOrEqual(t, results[0].Score, results[1].Score)
}

func TestSearchSemantic_ThresholdFiltering(t *testing.T) {
	s := setupSemanticStore(t)
	ctx := context.Background()

	_, err := s.StoreSemantic(ctx, "close match", "test", "proj", nil,
		[]float64{1.0, 0.0}, "test")
	require.NoError(t, err)
	_, err = s.StoreSemantic(ctx, "distant match", "test", "proj", nil,
		[]float64{0.0, 1.0}, "test")
	require.NoError(t, err)

	// Query for [1,0]. Close match has score 1.0, distant has 0.0.
	// Threshold 0.5 should return only close match.
	results, err := s.SearchSemantic(ctx, []float64{1.0, 0.0}, 10, 0.5)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "close match", results[0].Content)
}

func TestSearchSemantic_Limit(t *testing.T) {
	s := setupSemanticStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_, err := s.StoreSemantic(ctx, "record", "test", "proj", nil,
			[]float64{1.0, 0.0}, "test")
		require.NoError(t, err)
	}

	results, err := s.SearchSemantic(ctx, []float64{1.0, 0.0}, 2, 0.0)
	require.NoError(t, err)
	assert.Len(t, results, 2, "should respect limit")
}

func TestDeleteSemantic(t *testing.T) {
	s := setupSemanticStore(t)
	ctx := context.Background()

	id, err := s.StoreSemantic(ctx, "to delete", "test", "proj", nil,
		[]float64{0.5}, "test")
	require.NoError(t, err)

	err = s.DeleteSemantic(ctx, id)
	require.NoError(t, err)

	_, err = s.GetSemanticByID(ctx, id)
	assert.Error(t, err, "should not find deleted record")
}

func TestSearchSemantic_NoEmbedding(t *testing.T) {
	s := setupSemanticStore(t)
	ctx := context.Background()

	// Store record with empty embedding (default '[]').
	_, err := s.StoreSemantic(ctx, "no embedding", "test", "proj", nil, nil, "")
	require.NoError(t, err)

	results, err := s.SearchSemantic(ctx, []float64{1.0}, 10, 0.0)
	require.NoError(t, err)
	assert.Empty(t, results, "records without embeddings should be skipped")
}

func TestUpdateSemanticTimestamp(t *testing.T) {
	s := setupSemanticStore(t)
	ctx := context.Background()

	id, err := s.StoreSemantic(ctx, "ts test", "test", "proj", nil, nil, "")
	require.NoError(t, err)

	before, err := s.GetSemanticByID(ctx, id)
	require.NoError(t, err)

	// Small sleep to ensure timestamp differs.
	time.Sleep(10 * time.Millisecond)

	time.Sleep(1100 * time.Millisecond) // SQLite timestamps are second-precision.

	err = s.UpdateSemanticTimestamp(ctx, id)
	require.NoError(t, err)

	after, err := s.GetSemanticByID(ctx, id)
	require.NoError(t, err)
	assert.NotEqual(t, before.UpdatedAt, after.UpdatedAt, "timestamp should be updated")
}

func TestSearchSemanticForProject_ScopeFiltering(t *testing.T) {
	s := setupSemanticStore(t)
	ctx := context.Background()

	// Store records for different projects.
	vec1 := []float64{1.0, 0.0, 0.0}
	vec2 := []float64{0.9, 0.1, 0.0}
	vecGlobal := []float64{0.95, 0.05, 0.0}

	_, err := s.StoreSemantic(ctx, "proj-a content", "test", "proj-a", nil, vec1, "")
	require.NoError(t, err)
	_, err = s.StoreSemantic(ctx, "proj-b content", "test", "proj-b", nil, vec2, "")
	require.NoError(t, err)
	_, err = s.StoreSemantic(ctx, "global content", "test", "", nil, vecGlobal, "")
	require.NoError(t, err)

	query := []float64{1.0, 0.0, 0.0}

	// Search for proj-a: should return proj-a + global, but NOT proj-b.
	results, err := s.SearchSemanticForProject(ctx, query, "proj-a", 10, 0.0)
	require.NoError(t, err)
	assert.Len(t, results, 2, "should return proj-a and global entries")
	for _, r := range results {
		assert.True(t, r.ProjectName == "proj-a" || r.ProjectName == "",
			"unexpected project: %s", r.ProjectName)
	}

	// Search for proj-b: should return proj-b + global, but NOT proj-a.
	results, err = s.SearchSemanticForProject(ctx, query, "proj-b", 10, 0.0)
	require.NoError(t, err)
	assert.Len(t, results, 2)
	for _, r := range results {
		assert.True(t, r.ProjectName == "proj-b" || r.ProjectName == "",
			"unexpected project: %s", r.ProjectName)
	}
}
