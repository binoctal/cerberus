package session

import (
	"context"
	"testing"

	"github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/stretchr/testify/require"
)

func TestConsolidate_EffectivenessGroupedByProcedural(t *testing.T) {
	// Assert: a memory recalled for 3 cases (2 pass, 1 fail) in one session gets
	// ONE EMA update with signal 2/3, not three. Build rp.verdicts accordingly,
	// seed memory_usage rows via RecordMemoryUsage, run executeConsolidatePhase,
	// check effectiveness moved once and memory_usage rows are consolidated_at-stamped.
	ctx := context.Background()
	rp, cleanup := newTestRunPhase(t)
	defer cleanup()

	rp.session.ID = "sess-1"

	// Insert session record into database (required by foreign key constraint)
	_, err := rp.session.Store.DB().ExecContext(ctx,
		`INSERT INTO sessions (id, mode, status, goal, project_name, coverage_pct, stats, started_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		rp.session.ID, "run", "running", "test goal", "test-project", 0.0, "{}")
	require.NoError(t, err, "failed to insert session record")

	// Create a procedural memory with initial effectiveness 0.5
	proc, err := rp.session.Store.StoreProceduralWithType(
		ctx, "test-name", "test-condition", "test-action", "test-project", "test-category", "failure", nil,
		embed.NewTrigramProvider(embed.DefaultDimension).ModelName())
	require.NoError(t, err)

	// Set initial effectiveness to 0.5
	_, err = rp.session.Store.DB().ExecContext(ctx,
		`UPDATE memory_procedural SET effectiveness = 0.5, usage_count = 0 WHERE id = ?`, proc.ID)
	require.NoError(t, err)

	// Record 3 memory usage rows for the same procedural memory
	err = rp.session.Store.RecordMemoryUsage(ctx, proc.ID, "sess-1", "tc-1", "/api/users/123", 1)
	require.NoError(t, err)
	err = rp.session.Store.RecordMemoryUsage(ctx, proc.ID, "sess-1", "tc-2", "/api/posts/456", 1)
	require.NoError(t, err)
	err = rp.session.Store.RecordMemoryUsage(ctx, proc.ID, "sess-1", "tc-3", "/api/comments/789", 1)
	require.NoError(t, err)

	// Set verdicts: 2 pass, 1 fail with matching targets
	rp.verdicts = []examiner.FinalVerdict{
		{Status: examiner.StatusPass, StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "tc-1", Target: "/api/users/123"}}},
		{Status: examiner.StatusPass, StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "tc-2", Target: "/api/posts/456"}}},
		{Status: examiner.StatusFail, StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "tc-3", Target: "/api/comments/789"}}},
	}

	// Run consolidate phase
	err = rp.executeConsolidatePhase()
	require.NoError(t, err)

	// Assert effectiveness moved ONCE with signal 2/3
	// Expected: 0.7*0.5 + 0.3*(2/3) = 0.35 + 0.2 = 0.55
	var eff float64
	var usage int
	err = rp.session.Store.DB().QueryRowContext(ctx,
		`SELECT effectiveness, usage_count FROM memory_procedural WHERE id = ?`, proc.ID).Scan(&eff, &usage)
	require.NoError(t, err)
	require.InDelta(t, 0.55, eff, 0.001, "effectiveness should be updated once with signal 2/3")
	require.Equal(t, 3, usage, "usage_count should be incremented by 3")

	// Assert all 3 memory_usage rows have consolidated_at set
	var count int
	err = rp.session.Store.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_usage WHERE procedural_id = ? AND consolidated_at IS NOT NULL`, proc.ID).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 3, count, "all 3 memory_usage rows should be consolidated")

	// Assert no unconsolidated rows remain for this session
	err = rp.session.Store.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_usage WHERE session_id = ? AND consolidated_at IS NULL`, "sess-1").Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 0, count, "no unconsolidated rows should remain")
}
