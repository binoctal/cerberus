package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionCRUD(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "test all APIs", "my-project")
	require.NoError(t, err)
	assert.NotEmpty(t, sess.ID)
	assert.Equal(t, "running", sess.Status)
	assert.Equal(t, "run", sess.Mode)

	got, err := s.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, sess.ID, got.ID)
	assert.Equal(t, "test all APIs", got.Goal)

	err = s.UpdateSessionStatus(ctx, sess.ID, "completed")
	require.NoError(t, err)
	got, err = s.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, "completed", got.Status)
	assert.NotEmpty(t, got.FinishedAt)

	sessions, err := s.ListSessions(ctx, 10)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(sessions), 1)
}

func TestTraceCRUD(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "trace test", "")
	require.NoError(t, err)

	traceID, err := s.CreateTrace(ctx, sess.ID, "api", "GET /api/v1/users")
	require.NoError(t, err)
	assert.Greater(t, traceID, int64(0))

	err = s.FinishTrace(ctx, traceID, "pass")
	require.NoError(t, err)

	traces, err := s.GetTraces(ctx, sess.ID)
	require.NoError(t, err)
	require.Len(t, traces, 1)
	assert.Equal(t, "pass", traces[0].Status)
	assert.Equal(t, "api", traces[0].Category)
}

func TestVerdictCRUD(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "verdict test", "")
	require.NoError(t, err)
	traceID, err := s.CreateTrace(ctx, sess.ID, "api", "POST /api/v1/users")
	require.NoError(t, err)

	v, err := s.CreateVerdict(ctx, sess.ID, traceID, "POST /api/v1/users",
		"pass", 0.95, "judge", "Response matches expected schema", nil)
	require.NoError(t, err)
	assert.Greater(t, v.ID, int64(0))
	assert.Equal(t, 0.95, v.Confidence)

	verdicts, err := s.GetVerdicts(ctx, sess.ID)
	require.NoError(t, err)
	require.Len(t, verdicts, 1)
	assert.Equal(t, "pass", verdicts[0].Status)
	assert.Equal(t, "judge", verdicts[0].Source)
}

func TestEpisodicMemory(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "memory test", "")
	require.NoError(t, err)

	err = s.RecordEpisodic(ctx, sess.ID, "GET /api/v1/users", "pass",
		map[string]any{"status_code": 200}, 2*time.Second)
	require.NoError(t, err)

	memories, err := s.GetEpisodicByTarget(ctx, "GET /api/v1/users", 10)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(memories), 1)
	assert.Equal(t, "pass", memories[0].Status)
}

func TestEvidenceCRUD(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "evidence test", "")
	require.NoError(t, err)
	traceID, err := s.CreateTrace(ctx, sess.ID, "api", "GET /api/v1/users")
	require.NoError(t, err)

	ev, err := s.CreateEvidence(ctx, traceID, "api_response", `{"status":200,"body":"ok"}`)
	require.NoError(t, err)
	assert.Greater(t, ev.ID, int64(0))
	assert.Equal(t, "api_response", ev.Type)

	ev2, err := s.CreateEvidence(ctx, traceID, "error", "connection refused")
	require.NoError(t, err)

	evidence, err := s.GetEvidenceByTrace(ctx, traceID)
	require.NoError(t, err)
	require.Len(t, evidence, 2)
	assert.Equal(t, ev.ID, evidence[0].ID)
	assert.Equal(t, ev2.ID, evidence[1].ID)
}

func TestProceduralCRUD(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()
	ctx := context.Background()

	pm, err := s.StoreProcedural(ctx, "auth-retry",
		"POST /api/v1/*", "Refresh auth token before retry", "test-project")
	require.NoError(t, err)
	assert.Greater(t, pm.ID, int64(0))
	assert.Equal(t, 0.5, pm.Effectiveness)

	// Match by exact condition
	matches, err := s.GetProceduralByMatch(ctx, "POST /api/v1/users", 5)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(matches), 1)
	assert.Equal(t, "auth-retry", matches[0].Name)

	// No match for unrelated target
	noMatch, err := s.GetProceduralByMatch(ctx, "GET /health", 5)
	require.NoError(t, err)
	assert.Empty(t, noMatch)

	// Update effectiveness — success
	err = s.UpdateProceduralEffectiveness(ctx, pm.ID, true)
	require.NoError(t, err)
	matches, _ = s.GetProceduralByMatch(ctx, "POST /api/v1/users", 5)
	assert.Equal(t, 1, matches[0].UsageCount)
	assert.InDelta(t, 0.65, matches[0].Effectiveness, 0.01) // 0.7*0.5 + 0.3*1.0

	// Update effectiveness — failure
	err = s.UpdateProceduralEffectiveness(ctx, pm.ID, false)
	require.NoError(t, err)
	matches, _ = s.GetProceduralByMatch(ctx, "POST /api/v1/users", 5)
	assert.Equal(t, 2, matches[0].UsageCount)
	assert.InDelta(t, 0.455, matches[0].Effectiveness, 0.01) // 0.7*0.65 + 0.3*0.0
}

func TestUpdateSessionStats(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "stats test", "")
	require.NoError(t, err)

	stats := map[string]any{
		"total_tokens": 54000,
		"ai_calls":    23,
		"steps":       20,
	}
	err = s.UpdateSessionStats(ctx, sess.ID, 75.5, stats)
	require.NoError(t, err)

	got, err := s.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.InDelta(t, 75.5, got.CoveragePct, 0.01)
	assert.Contains(t, got.Stats, "54000")
}

func TestProceduralWithType(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()
	ctx := context.Background()

	pm, err := s.StoreProceduralWithType(ctx, "auth_failure", "* returned 401",
		"Refresh auth token before retry", "test-project", "auth_failure", "failure")
	require.NoError(t, err)
	assert.Equal(t, "auth_failure", pm.Category)
	assert.Equal(t, "failure", pm.Type)

	// Verify retrieval by effectiveness includes type info.
	results, err := s.GetProceduralByEffectiveness(ctx, 0.2, 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "failure", results[0].Type)
	assert.Equal(t, "auth_failure", results[0].Category)
}

func TestProceduralArchive(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()
	ctx := context.Background()

	pm, err := s.StoreProcedural(ctx, "test", "* pattern", "action", "project")
	require.NoError(t, err)

	// Archive it.
	err = s.ArchiveProcedural(ctx, pm.ID)
	require.NoError(t, err)

	// Should not appear in match results (archived).
	matches, err := s.GetProceduralByMatch(ctx, "pattern", 10)
	require.NoError(t, err)
	assert.Empty(t, matches)

	// Should not appear in effectiveness query either.
	results, err := s.GetProceduralByEffectiveness(ctx, 0.0, 10)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestAutoArchiveLowEffectiveness(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()
	ctx := context.Background()

	pm, err := s.StoreProcedural(ctx, "low", "* test", "action", "project")
	require.NoError(t, err)

	// Drive effectiveness below 0.2 with repeated failures.
	// Start at 0.5, apply failures until < 0.2.
	for i := 0; i < 5; i++ {
		err := s.UpdateProceduralEffectiveness(ctx, pm.ID, false)
		require.NoError(t, err)
	}

	// Auto-archive.
	archived, err := s.AutoArchiveLowEffectiveness(ctx, 0.2)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, archived, 1)
}

func TestMarkStaleProcedural(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()
	ctx := context.Background()

	// Insert with old created_at directly.
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO memory_procedural (name, condition, action, effectiveness, usage_count, project_name, category, type, archived, created_at)
		 VALUES ('stale', '* old', 'action', 0.3, 1, 'proj', 'general_failure', 'failure', 0, '2020-01-01T00:00:00Z')`)
	require.NoError(t, err)

	// Mark stale: older than 90 days + effectiveness < 0.5.
	stale, err := s.MarkStaleProcedural(ctx, 90, 0.5)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, stale, 1)
}
