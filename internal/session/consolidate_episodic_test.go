package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/contract"
	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
)

// newTestRunPhase creates a minimal runPhase with in-memory store for testing.
// Mirror existing session test setup patterns from lifecycle_test.go.
func newTestRunPhase(t *testing.T) (rp *runPhase, cleanup func()) {
	t.Helper()

	// Create in-memory store with migrations
	s, err := store.New(":memory:")
	require.NoError(t, err)
	err = store.RunMigrations(context.Background(), s.DB(), "../../migrations")
	require.NoError(t, err)

	// Create minimal session config
	cfg := project.DefaultConfig()
	cfg.Project.Name = "test-project"

	mockClient := llm.NewMockClient(map[string]string{"default": `{"status":"pass"}`})
	logger := zap.NewNop()

	sess, err := NewSession(context.Background(), SessionConfig{
		Mode:       ModeRun,
		Goal:       "test goal",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     logger,
		Gate:       nil,
		ProjectDir: ".",
		CoverageFn: func(context.Context, *Session) contract.CoverageMeasurement {
			return contract.CoverageMeasurement{Pct: 1.0, Unit: "line", Known: true}
		},
	})
	require.NoError(t, err)

	// Create runPhase with necessary fields
	rp = &runPhase{
		session:  sess,
		ctx:      context.Background(),
		verdicts: []examiner.FinalVerdict{}, // Will be set by test
	}

	cleanup = func() {
		_ = s.Close()
	}

	return rp, cleanup
}

func TestConsolidate_WritesEpisodicPerVerdict(t *testing.T) {
	// Build a runPhase with two synthetic verdicts (one pass, one skip) and call
	// executeConsolidatePhase; assert two episodic rows exist with normalized targets.
	ctx := context.Background()
	rp, cleanup := newTestRunPhase(t)
	defer cleanup()

	t.Logf("Session ID: %s", rp.session.ID)
	rp.session.ID = "sess-1"

	// Insert session record into database (required by foreign key constraint)
	_, err := rp.session.Store.DB().ExecContext(ctx,
		`INSERT INTO sessions (id, mode, status, goal, project_name, coverage_pct, stats, started_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		rp.session.ID, "run", "running", "test goal", "test-project", 0.0, "{}")
	require.NoError(t, err, "failed to insert session record")

	rp.verdicts = []examiner.FinalVerdict{
		{Status: examiner.StatusPass, StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "tc-1", Target: "/api/users/123"}}},
		{Status: examiner.StatusSkip, StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "tc-2", Target: "/health"}}},
	}

	t.Logf("Executing consolidate phase with %d verdicts", len(rp.verdicts))
	err = rp.executeConsolidatePhase()
	t.Logf("Consolidate phase error: %v", err)
	require.NoError(t, err)

	var n int
	err = rp.session.Store.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_episodic WHERE session_id='sess-1'`).Scan(&n)
	require.NoError(t, err)
	t.Logf("Episodic count for sess-1: %d", n)
	require.NoError(t, err)
	require.Equal(t, 2, n)

	var target string
	err = rp.session.Store.DB().QueryRowContext(ctx,
		`SELECT target FROM memory_episodic WHERE session_id='sess-1' AND target LIKE '%users%'`).Scan(&target)
	require.NoError(t, err)
	t.Logf("Target for users test: %s", target)
	require.Equal(t, "/api/users/{id}", target, "target must be normalized")
}
