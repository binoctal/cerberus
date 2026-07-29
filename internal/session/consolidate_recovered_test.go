package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/binoctal/cerberus/internal/memory"
	"github.com/binoctal/cerberus/internal/store"
)

// TestVerdictByNormalizedTarget_RecoveredDoesNotOverwritePrimary: a recovered
// fallback verdict (committed, recovered=true) sharing a target with its
// primary's fail must NOT overwrite the fail in the effectiveness map — the
// recalled strategy's signal is the primary's fail.
func TestVerdictByNormalizedTarget_RecoveredDoesNotOverwritePrimary(t *testing.T) {
	ctx := context.Background()
	rp, cleanup := newTestRunPhase(t)
	defer cleanup()

	rp.session.ID = "sess-1"
	_, err := rp.session.Store.DB().ExecContext(ctx,
		`INSERT INTO sessions (id, mode, status, goal, project_name, coverage_pct, stats, started_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		rp.session.ID, "run", "running", "g", "test-project", 0.0, "{}")
	require.NoError(t, err)

	traceID, err := rp.session.Store.CreateTrace(ctx, rp.session.ID, "ws", "ws://h/ws")
	require.NoError(t, err)

	const target = "ws://h/ws"
	// Primary fail (committed), then recovered fallback (committed), same target.
	_, err = rp.session.Store.CreateVerdict(ctx, rp.session.ID, traceID, target, "fail", 0.4, "judge", "primary failed", nil, "assertion_failed", false, "", "")
	require.NoError(t, err)
	_, err = rp.session.Store.CreateVerdict(ctx, rp.session.ID, traceID, target, "pass", 0.9, "judge", "fallback recovered", nil, "", true, "", "")
	require.NoError(t, err)

	out := verdictByNormalizedTarget(ctx, rp.session, nil)
	info, ok := out[memory.NormalizeTarget(target)]
	require.True(t, ok, "target present")
	require.Equal(t, examiner.StatusFail, info.status, "primary fail wins; recovered does not overwrite")
}

// TestVerdictByNormalizedTarget_UnrecoveredFallbackDoesNotShadowPrimary: an
// UNRECOVERED fallback (FallbackFor set, also failed) committed alongside its
// primary's fail must NOT shadow the primary's failure reason in the
// effectiveness map. Both share the target; the primary is the real signal.
func TestVerdictByNormalizedTarget_UnrecoveredFallbackDoesNotShadowPrimary(t *testing.T) {
	ctx := context.Background()
	rp, cleanup := newTestRunPhase(t)
	defer cleanup()

	rp.session.ID = "sess-shadow"
	_, err := rp.session.Store.DB().ExecContext(ctx,
		`INSERT INTO sessions (id, mode, status, goal, project_name, coverage_pct, stats, started_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		rp.session.ID, "run", "running", "g", "test-project", 0.0, "{}")
	require.NoError(t, err)

	traceID, err := rp.session.Store.CreateTrace(ctx, rp.session.ID, "ws", "ws://h/ws")
	require.NoError(t, err)

	const target = "ws://h/ws"
	// Primary fail carries assertion_failed; the fallback (FallbackFor set) also
	// failed but with an environmental reason. Without the non-unit skip it would
	// overwrite the primary's assertion_failed and let the strategy off the hook.
	_, err = rp.session.Store.CreateVerdict(ctx, rp.session.ID, traceID, target, "fail", 0.4, "judge", "primary failed", nil, "assertion_failed", false, "", "")
	require.NoError(t, err)
	_, err = rp.session.Store.CreateVerdict(ctx, rp.session.ID, traceID, target, "fail", 0.5, "judge", "fallback also failed", nil, "target_unreachable", false, "A", "")
	require.NoError(t, err)

	out := verdictByNormalizedTarget(ctx, rp.session, nil)
	info, ok := out[memory.NormalizeTarget(target)]
	require.True(t, ok, "target present")
	require.Equal(t, examiner.StatusFail, info.status, "primary fail wins")
	require.Equal(t, store.FailureReason("assertion_failed"), info.reason, "primary's assertion_failed reason wins; unrecovered fallback does not shadow")
}

// TestWriteEpisodicMemory_SkipsFallback: a recovered fallback verdict does not
// produce a second episodic row for its primary's target.
func TestWriteEpisodicMemory_SkipsFallback(t *testing.T) {
	ctx := context.Background()
	rp, cleanup := newTestRunPhase(t)
	defer cleanup()

	rp.session.ID = "sess-2"
	_, err := rp.session.Store.DB().ExecContext(ctx,
		`INSERT INTO sessions (id, mode, status, goal, project_name, coverage_pct, stats, started_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		rp.session.ID, "run", "running", "g", "test-project", 0.0, "{}")
	require.NoError(t, err)

	rp.verdicts = []examiner.FinalVerdict{
		{Status: examiner.StatusFail, StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "A", Target: "/x"}}},
		{Status: examiner.StatusPass, StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "A'", Target: "/x", FallbackFor: "A"}, Recovered: true}},
	}
	require.NoError(t, writeEpisodicMemory(ctx, rp.session, rp.verdicts))

	var n int
	require.NoError(t, rp.session.Store.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_episodic WHERE session_id='sess-2'`).Scan(&n))
	require.Equal(t, 1, n, "one episodic row per target (primary only)")
}
