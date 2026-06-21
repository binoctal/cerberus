package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/store"
)

func TestGatherMemoryStats(t *testing.T) {
	t.Setenv("CERBERUS_MIGRATION_DIR", migrationDir(t))
	s, err := store.New(t.TempDir() + "/test.db")
	require.NoError(t, err)
	require.NoError(t, store.RunMigrations(context.Background(), s.DB(), migrationDir(t)))
	ctx := context.Background()

	emb := embed.NewTrigramProvider(embed.DefaultDimension)
	vec, _ := emb.Embed(ctx, "post /api/v1/auth/login returned 401")
	// Active, embedded, mid-effectiveness (0.7*0.5+0.3*1.0=0.65), recalled twice.
	m1, err := s.StoreProceduralWithType(ctx, "auth-401", "post /api/v1/auth/login returned 401", "retry auth", "p", "auth_failure", "failure", vec, emb.ModelName())
	require.NoError(t, err)
	require.NoError(t, s.ApplyProceduralEMA(ctx, m1.ID, 1.0, 2)) // eff 0.5->0.65 (mid), usage 2

	// Dormant, not embedded, default effectiveness.
	_, _ = s.StoreProceduralWithType(ctx, "dormant", "some condition", "some action", "p", "general_failure", "failure", nil, "")

	// Archived row.
	_, err = s.DB().ExecContext(ctx, `UPDATE memory_procedural SET archived=1 WHERE id=?`, m1.ID)
	require.NoError(t, err)

	// Episodic + a session FK for it.
	_, err = s.DB().ExecContext(ctx, `INSERT INTO sessions (id, mode, status, goal, project_name, coverage_pct, stats, started_at) VALUES ('s1','run','done','g','p',0,'{}',datetime('now'))`)
	require.NoError(t, err)
	require.NoError(t, s.RecordEpisodic(ctx, "s1", "/api/x", "pass", "pass", 0))
	require.NoError(t, s.RecordEpisodic(ctx, "s1", "/api/y", "fail", "fail", 0))
	_, err = s.DB().ExecContext(ctx, `UPDATE memory_episodic SET archived=1 WHERE target='/api/y'`)
	require.NoError(t, err)

	st, err := gatherMemoryStats(ctx, s)
	require.NoError(t, err)

	assert.Equal(t, 2, st.ProcTotal)
	assert.Equal(t, 1, st.ProcEmbedded)
	assert.Equal(t, 1, st.ProcArchived)
	assert.Equal(t, 1, st.ProcActive, "m1 has usage 2")
	assert.Equal(t, 1, st.ProcDormant)
	assert.Equal(t, 2, st.EffMid, "m1 eff 0.65 + dormant 0.5 both in mid bucket")
	assert.Equal(t, 0, st.EffHigh)
	assert.Equal(t, 2, st.TopUsage)
	assert.Equal(t, 2, st.EpiTotal)
	assert.Equal(t, 1, st.EpiArchived)
}
