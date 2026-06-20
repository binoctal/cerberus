package main

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/store"
)

func TestMemoryCmd_List(t *testing.T) {
	t.Setenv("CERBERUS_MIGRATION_DIR", migrationDir(t))
	tmpFile := t.TempDir() + "/test.db"

	s, err := store.New(tmpFile)
	require.NoError(t, err)
	require.NoError(t, store.RunMigrations(context.Background(), s.DB(), migrationDir(t)))

	ctx := context.Background()

	// Seed a procedural memory
	emb := embed.NewTrigramProvider(embed.DefaultDimension)
	vec, _ := emb.Embed(ctx, "test condition")
	_, err = s.StoreProceduralWithType(ctx, "test-memory", "test condition", "test action", "test-project", "test-category", "failure", vec, emb.ModelName())
	require.NoError(t, err)
	_ = s.Close()

	// Set environment variable to override the DB path
	origDB := os.Getenv("CERBERUS_DB_PATH")
	t.Setenv("CERBERUS_DB_PATH", tmpFile)
	t.Cleanup(func() { os.Setenv("CERBERUS_DB_PATH", origDB) })

	// Test list command
	cmd := memoryListCmd()
	output := captureStdout(t, func() {
		err = cmd.RunE(cmd, []string{})
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Procedural Memories:")
	assert.Contains(t, output, "test-memory")
}

func TestMemoryCmd_Show(t *testing.T) {
	t.Setenv("CERBERUS_MIGRATION_DIR", migrationDir(t))
	tmpFile := t.TempDir() + "/test.db"

	s, err := store.New(tmpFile)
	require.NoError(t, err)
	require.NoError(t, store.RunMigrations(context.Background(), s.DB(), migrationDir(t)))

	ctx := context.Background()

	// Seed a procedural memory
	emb := embed.NewTrigramProvider(embed.DefaultDimension)
	vec, _ := emb.Embed(ctx, "test condition")
	_, err = s.StoreProceduralWithType(ctx, "test-memory", "test condition", "test action", "test-project", "test-category", "failure", vec, emb.ModelName())
	require.NoError(t, err)
	_ = s.Close()

	// Set environment variable to override the DB path
	origDB := os.Getenv("CERBERUS_DB_PATH")
	t.Setenv("CERBERUS_DB_PATH", tmpFile)
	t.Cleanup(func() { os.Setenv("CERBERUS_DB_PATH", origDB) })

	// Test show command
	cmd := memoryShowCmd()
	require.NoError(t, cmd.Flags().Set("id", "1"))
	output := captureStdout(t, func() {
		err = cmd.RunE(cmd, []string{})
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Procedural Memory:")
	assert.Contains(t, output, "test-memory")
	assert.Contains(t, output, "test condition")
	assert.Contains(t, output, "test action")
}

func TestMemoryCmd_Prune(t *testing.T) {
	t.Setenv("CERBERUS_MIGRATION_DIR", migrationDir(t))
	tmpFile := t.TempDir() + "/test.db"

	s, err := store.New(tmpFile)
	require.NoError(t, err)
	require.NoError(t, store.RunMigrations(context.Background(), s.DB(), migrationDir(t)))

	ctx := context.Background()

	// Seed a procedural memory
	emb := embed.NewTrigramProvider(embed.DefaultDimension)
	vec, _ := emb.Embed(ctx, "test condition")
	_, err = s.StoreProceduralWithType(ctx, "test-memory", "test condition", "test action", "test-project", "test-category", "failure", vec, emb.ModelName())
	require.NoError(t, err)
	_ = s.Close()

	// Set environment variable to override the DB path
	origDB := os.Getenv("CERBERUS_DB_PATH")
	t.Setenv("CERBERUS_DB_PATH", tmpFile)
	t.Cleanup(func() { os.Setenv("CERBERUS_DB_PATH", origDB) })

	// Test prune command (soft archive by default)
	cmd := memoryPruneCmd()
	output := captureStdout(t, func() {
		err = cmd.RunE(cmd, []string{})
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Soft prune:")
}

func TestMemoryCmd_Reembed(t *testing.T) {
	t.Setenv("CERBERUS_MIGRATION_DIR", migrationDir(t))
	tmpFile := t.TempDir() + "/test.db"

	s, err := store.New(tmpFile)
	require.NoError(t, err)
	require.NoError(t, store.RunMigrations(context.Background(), s.DB(), migrationDir(t)))

	ctx := context.Background()

	// Seed a procedural memory with empty embedding (legacy case)
	_, err = s.StoreProceduralWithType(ctx, "legacy-memory", "old condition", "old action", "test-project", "test-category", "failure", nil, "")
	require.NoError(t, err)

	// Seed a semantic memory with empty embedding
	semID, err := s.StoreSemantic(ctx, "legacy semantic", "test-source", "test-project", []string{"tag1"}, nil, "")
	require.NoError(t, err)
	_ = s.Close()

	// Set environment variable to override the DB path
	origDB := os.Getenv("CERBERUS_DB_PATH")
	t.Setenv("CERBERUS_DB_PATH", tmpFile)
	t.Cleanup(func() { os.Setenv("CERBERUS_DB_PATH", origDB) })

	// Test reembed command
	cmd := memoryReembedCmd()
	output := captureStdout(t, func() {
		err = cmd.RunE(cmd, []string{})
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Re-embedded")
	assert.Contains(t, output, "procedural")
	assert.Contains(t, output, "semantic")

	// Verify actual behavior: re-open DB and assert embedding_model changed
	s2, err := store.New(tmpFile)
	require.NoError(t, err)

	// Check procedural memory was updated with trigram model
	procMem, err := s2.GetProceduralByExactKey(ctx, "test-project", "old condition", "old action")
	require.NoError(t, err)
	assert.Equal(t, "trigram-v1", procMem.EmbeddingModel, "procedural embedding_model should be trigram-v1 after reembed")
	assert.NotEmpty(t, procMem.Embedding, "procedural embedding should be non-empty after reembed")

	// Check semantic memory was updated with trigram model
	semMem, err := s2.GetSemanticByID(ctx, semID)
	require.NoError(t, err)
	assert.Equal(t, "trigram-v1", semMem.EmbeddingModel, "semantic embedding_model should be trigram-v1 after reembed")
	assert.NotEmpty(t, semMem.Embedding, "semantic embedding should be non-empty after reembed")

	_ = s2.Close()
}

func TestStore_UpdateEmbeddingHelpers(t *testing.T) {
	t.Setenv("CERBERUS_MIGRATION_DIR", migrationDir(t))
	tmpFile := t.TempDir() + "/test.db"

	s, err := store.New(tmpFile)
	require.NoError(t, err)
	require.NoError(t, store.RunMigrations(context.Background(), s.DB(), migrationDir(t)))

	ctx := context.Background()

	// Create a procedural memory
	emb := embed.NewTrigramProvider(embed.DefaultDimension)
	vec1, _ := emb.Embed(ctx, "original condition")
	mem, err := s.StoreProceduralWithType(ctx, "test-memory", "original condition", "test action", "test-project", "test-category", "failure", vec1, emb.ModelName())
	require.NoError(t, err)

	// Update embedding
	vec2, _ := emb.Embed(ctx, "updated condition")
	err = s.UpdateProceduralEmbedding(ctx, mem.ID, vec2, "trigram-v2")
	require.NoError(t, err)

	// Verify update
	updated, err := s.GetProceduralByExactKey(ctx, "test-project", "original condition", "test action")
	require.NoError(t, err)
	assert.Equal(t, "trigram-v2", updated.EmbeddingModel)

	// Test semantic embedding update
	semID, err := s.StoreSemantic(ctx, "test content", "test-source", "test-project", []string{"tag1"}, vec1, emb.ModelName())
	require.NoError(t, err)

	vec3, _ := emb.Embed(ctx, "new content")
	err = s.UpdateSemanticEmbedding(ctx, semID, vec3, "trigram-v2")
	require.NoError(t, err)

	// Verify update
	updatedSem, err := s.GetSemanticByID(ctx, semID)
	require.NoError(t, err)
	assert.Equal(t, "trigram-v2", updatedSem.EmbeddingModel)

	_ = s.Close()
}
