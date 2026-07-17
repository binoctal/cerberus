package session

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/contract"
	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/memory"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
)

// TestResumeIdempotency_ConsolidatedAtGuard tests that memory_usage rows are only
// consolidated once even when consolidate runs multiple times (consolidated_at guard).
// Note: episodic rows are NOT idempotent - each consolidate call creates new rows.
func TestResumeIdempotency_ConsolidatedAtGuard(t *testing.T) {
	ctx := context.Background()

	// Create in-memory store with migrations
	s, err := store.New(":memory:")
	require.NoError(t, err)
	err = store.RunMigrations(context.Background(), s.DB(), "../../migrations")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

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
	sess.ID = "sess-resume-idempotency"

	// Insert session record into database (required by foreign key constraint)
	_, err = sess.Store.DB().ExecContext(ctx,
		`INSERT INTO sessions (id, mode, status, goal, project_name, coverage_pct, stats, started_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		sess.ID, "run", "running", "test goal", "test-project", 0.0, "{}")
	require.NoError(t, err)

	// Create a procedural memory to record usage against
	procID, err := sess.Store.StoreProceduralWithType(ctx,
		"test-strategy", "/api/users", "check auth",
		"test-project", "test", "failure", []float64{0.1, 0.2}, "test-model")
	require.NoError(t, err)
	require.NotZero(t, procID)

	// Create a runPhase with verdicts
	rp := &runPhase{
		session: sess,
		ctx:     ctx,
		verdicts: []examiner.FinalVerdict{
			{Status: examiner.StatusPass, StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "tc-1", Target: "/api/users/123"}}},
			{Status: examiner.StatusPass, StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "tc-2", Target: "/api/health"}}},
		},
	}

	// Record memory usage for the procedural memory against tc-1
	err = sess.Store.RecordMemoryUsage(ctx, procID.ID, sess.ID, "tc-1", memory.NormalizeTarget("/api/users/123"), 1)
	require.NoError(t, err)

	// First consolidate: writes episodic, marks usage consolidated
	err = rp.executeConsolidatePhase()
	require.NoError(t, err)

	// Assert episodic rows exist
	var episodicCount int
	err = sess.Store.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_episodic WHERE session_id=?`, sess.ID).Scan(&episodicCount)
	require.NoError(t, err)
	require.Equal(t, 2, episodicCount, "should have 2 episodic rows after first consolidate")

	// Assert usage is consolidated
	var consolidatedAt string
	err = sess.Store.DB().QueryRowContext(ctx,
		`SELECT consolidated_at FROM memory_usage WHERE procedural_id=? AND session_id=?`,
		procID.ID, sess.ID).Scan(&consolidatedAt)
	require.NoError(t, err, "usage should be consolidated after first consolidate")
	require.NotEmpty(t, consolidatedAt, "consolidated_at should be set")

	// Second consolidate: simulates resume re-running consolidate phase
	// This creates duplicate episodic rows (NOT idempotent for episodic)
	err = rp.executeConsolidatePhase()
	require.NoError(t, err)

	// Assert episodic count increased (duplicates created)
	var episodicCount2 int
	err = sess.Store.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_episodic WHERE session_id=?`, sess.ID).Scan(&episodicCount2)
	require.NoError(t, err)
	require.Equal(t, 4, episodicCount2, "episodic count doubles on second consolidate (not idempotent)")

	// Assert consolidated_at unchanged (idempotent for effectiveness EMA)
	var consolidatedAt2 string
	err = sess.Store.DB().QueryRowContext(ctx,
		`SELECT consolidated_at FROM memory_usage WHERE procedural_id=? AND session_id=?`,
		procID.ID, sess.ID).Scan(&consolidatedAt2)
	require.NoError(t, err)
	require.Equal(t, consolidatedAt, consolidatedAt2, "consolidated_at should not change on second consolidate")

	// Assert no unconsolidated usage rows remain (consolidated_at guard prevents double EMA)
	rows, err := sess.Store.UnconsolidatedUsage(ctx, sess.ID)
	require.NoError(t, err)
	require.Empty(t, rows, "no unconsolidated rows should remain")
}

// TestResumeIdempotency_UsageGuard tests that memory_usage rows are only
// consolidated once even when consolidate runs multiple times.
func TestResumeIdempotency_UsageGuard(t *testing.T) {
	ctx := context.Background()

	s, err := store.New(":memory:")
	require.NoError(t, err)
	err = store.RunMigrations(context.Background(), s.DB(), "../../migrations")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

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
	sess.ID = "sess-usage-guard"

	_, err = sess.Store.DB().ExecContext(ctx,
		`INSERT INTO sessions (id, mode, status, goal, project_name, coverage_pct, stats, started_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		sess.ID, "run", "running", "test goal", "test-project", 0.0, "{}")
	require.NoError(t, err)

	// Create procedural memory
	procID, err := sess.Store.StoreProceduralWithType(ctx,
		"test-strategy-2", "/api/items", "check schema",
		"test-project", "test", "failure", []float64{0.1, 0.2}, "test-model")
	require.NoError(t, err)
	require.NotZero(t, procID)

	// Record usage
	err = sess.Store.RecordMemoryUsage(ctx, procID.ID, sess.ID, "tc-1", memory.NormalizeTarget("/api/items/456"), 1)
	require.NoError(t, err)

	// First consolidate
	rp := &runPhase{
		session: sess,
		ctx:     ctx,
		verdicts: []examiner.FinalVerdict{
			{Status: examiner.StatusPass, StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "tc-1", Target: "/api/items/456"}}},
		},
	}
	err = rp.executeConsolidatePhase()
	require.NoError(t, err)

	// Check consolidated_at is set
	var consolidatedAt string
	err = sess.Store.DB().QueryRowContext(ctx,
		`SELECT consolidated_at FROM memory_usage WHERE procedural_id=? AND session_id=?`,
		procID.ID, sess.ID).Scan(&consolidatedAt)
	require.NoError(t, err)
	require.NotEmpty(t, consolidatedAt)

	// Get unconsolidated usage — should be empty
	rows, err := sess.Store.UnconsolidatedUsage(ctx, sess.ID)
	require.NoError(t, err)
	require.Empty(t, rows, "no unconsolidated rows should remain after consolidate")

	// Second consolidate — should be no-op for effectiveness since no unconsolidated rows
	err = rp.executeConsolidatePhase()
	require.NoError(t, err)

	// Verify consolidated_at unchanged
	var consolidatedAt2 string
	err = sess.Store.DB().QueryRowContext(ctx,
		`SELECT consolidated_at FROM memory_usage WHERE procedural_id=? AND session_id=?`,
		procID.ID, sess.ID).Scan(&consolidatedAt2)
	require.NoError(t, err)
	require.Equal(t, consolidatedAt, consolidatedAt2)
}

// TestResumeIdempotency_EpisodicUniqueness tests that episodic rows are created
// for each verdict in each consolidate call. Resume only processes new cases,
// so duplicate episodic rows for the same case don't occur in normal usage.
func TestResumeIdempotency_EpisodicUniqueness(t *testing.T) {
	ctx := context.Background()

	s, err := store.New(":memory:")
	require.NoError(t, err)
	err = store.RunMigrations(context.Background(), s.DB(), "../../migrations")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

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
	sess.ID = "sess-episodic-uniq"

	_, err = sess.Store.DB().ExecContext(ctx,
		`INSERT INTO sessions (id, mode, status, goal, project_name, coverage_pct, stats, started_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		sess.ID, "run", "running", "test goal", "test-project", 0.0, "{}")
	require.NoError(t, err)

	// First run: two cases
	rp1 := &runPhase{
		session: sess,
		ctx:     ctx,
		verdicts: []examiner.FinalVerdict{
			{Status: examiner.StatusPass, StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "tc-1", Target: "/api/test"}}},
			{Status: examiner.StatusPass, StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "tc-2", Target: "/api/other"}}},
		},
	}
	err = rp1.executeConsolidatePhase()
	require.NoError(t, err)

	var count1 int
	err = sess.Store.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_episodic WHERE session_id=?`, sess.ID).Scan(&count1)
	require.NoError(t, err)
	require.Equal(t, 2, count1)

	// Simulate resume: only run new case (tc-3)
	rp2 := &runPhase{
		session: sess,
		ctx:     ctx,
		verdicts: []examiner.FinalVerdict{
			{Status: examiner.StatusPass, StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "tc-3", Target: "/api/new"}}},
		},
	}
	err = rp2.executeConsolidatePhase()
	require.NoError(t, err)

	var count2 int
	err = sess.Store.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_episodic WHERE session_id=?`, sess.ID).Scan(&count2)
	require.NoError(t, err)
	require.Equal(t, 3, count2, "should have 3 episodic rows after resume")

	// Verify tc-3 exists
	var exists int
	err = sess.Store.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_episodic WHERE session_id=? AND target=?`,
		sess.ID, memory.NormalizeTarget("/api/new")).Scan(&exists)
	require.NoError(t, err)
	require.Equal(t, 1, exists, "new case should be recorded")
}

// TestResumePhase_ConsolidateAppliesEffectiveness tests that resumePhase.executeConsolidatePhase
// applies effectiveness EMA and governance, matching runPhase behavior.
// This is a regression test for the merge-blocker where resume only wrote episodic rows.
func TestResumePhase_ConsolidateAppliesEffectiveness(t *testing.T) {
	ctx := context.Background()

	s, err := store.New(":memory:")
	require.NoError(t, err)
	err = store.RunMigrations(context.Background(), s.DB(), "../../migrations")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

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
	sess.ID = "sess-resume-consolidate"

	// Insert session record
	_, err = sess.Store.DB().ExecContext(ctx,
		`INSERT INTO sessions (id, mode, status, goal, project_name, coverage_pct, stats, started_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		sess.ID, "run", "running", "test goal", "test-project", 0.0, "{}")
	require.NoError(t, err)

	// Create a procedural memory with default effectiveness 0.5
	procID, err := sess.Store.StoreProceduralWithType(ctx,
		"resume-strategy", "/api/widgets", "validate widgets",
		"test-project", "test", "failure", []float64{0.1, 0.2}, "test-model")
	require.NoError(t, err)
	require.NotZero(t, procID)

	// Verify initial effectiveness is 0.5
	var initialEffectiveness float64
	err = sess.Store.DB().QueryRowContext(ctx,
		`SELECT effectiveness FROM memory_procedural WHERE id=?`, procID.ID).Scan(&initialEffectiveness)
	require.NoError(t, err)
	require.Equal(t, 0.5, initialEffectiveness, "new procedural memory starts at 0.5")

	// Record memory usage for a passing case
	err = sess.Store.RecordMemoryUsage(ctx, procID.ID, sess.ID, "tc-widget-1", memory.NormalizeTarget("/api/widgets/999"), 1)
	require.NoError(t, err)

	// Verify usage row exists and is unconsolidated
	var consolidatedAt sql.NullString
	err = sess.Store.DB().QueryRowContext(ctx,
		`SELECT consolidated_at FROM memory_usage WHERE procedural_id=? AND session_id=?`,
		procID.ID, sess.ID).Scan(&consolidatedAt)
	require.NoError(t, err)
	require.False(t, consolidatedAt.Valid, "usage should be unconsolidated before consolidate")

	// Create resumePhase with verdicts (mimics what examiner produces)
	resume := &resumePhase{
		session: sess,
		ctx:     ctx,
		verdicts: []examiner.FinalVerdict{
			{Status: examiner.StatusPass, StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "tc-widget-1", Target: "/api/widgets/999"}}},
		},
	}

	// Execute consolidate phase — should write episodic, apply EMA, run governance
	err = resume.executeConsolidatePhase()
	require.NoError(t, err)

	// Assert episodic row created
	var episodicCount int
	err = sess.Store.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_episodic WHERE session_id=? AND target=?`,
		sess.ID, memory.NormalizeTarget("/api/widgets/999")).Scan(&episodicCount)
	require.NoError(t, err)
	require.Equal(t, 1, episodicCount, "episodic row should be created")

	// Assert usage row got consolidated_at stamped
	err = sess.Store.DB().QueryRowContext(ctx,
		`SELECT consolidated_at FROM memory_usage WHERE procedural_id=? AND session_id=?`,
		procID.ID, sess.ID).Scan(&consolidatedAt)
	require.NoError(t, err)
	require.True(t, consolidatedAt.Valid, "usage should be consolidated after consolidate")
	require.NotEmpty(t, consolidatedAt.String, "consolidated_at should have timestamp")

	// Assert effectiveness EMA updated from 0.5 (pass signal = 1.0)
	var newEffectiveness float64
	err = sess.Store.DB().QueryRowContext(ctx,
		`SELECT effectiveness FROM memory_procedural WHERE id=?`, procID.ID).Scan(&newEffectiveness)
	require.NoError(t, err)
	require.NotEqual(t, 0.5, newEffectiveness, "effectiveness should update from default")
	require.Greater(t, newEffectiveness, 0.5, "pass signal should increase effectiveness above 0.5")

	// Assert no unconsolidated rows remain
	rows, err := sess.Store.UnconsolidatedUsage(ctx, sess.ID)
	require.NoError(t, err)
	require.Empty(t, rows, "all usage should be consolidated")
}

// newResumableSessionWithContract builds a Session bound to a real
// migrations-backed store, then saves a single-case plan and the given coverage
// contract so a subsequent Resume() skips Scout, reloads the plan, runs the
// Agent + Examiner, and (once Task 12 lands) reloads the contract to assess
// coverage. Stub LLM responses (contractJSON) let Resume complete without live
// API calls. Mirrors the scaffolding in TestSession_Resume_SkipsCompleted and
// reuses testStoreWithMigrations / testConfig / stubCoverageFn.
func newResumableSessionWithContract(t *testing.T, c *contract.Contract) (SessionConfig, *Session, func()) {
	t.Helper()
	s := testStoreWithMigrations(t)

	cfg := testConfig()
	scfg := SessionConfig{
		Mode:       ModeRun,
		Goal:       "resume coverage test",
		Config:     &cfg,
		Store:      s,
		Client:     llm.NewMockClient(map[string]string{"default": contractJSON()}),
		Logger:     zap.NewNop(),
		Gate:       nil,
		ProjectDir: ".",
		CoverageFn: stubCoverageFn(),
	}

	sess, err := NewSession(context.Background(), scfg)
	require.NoError(t, err)

	plan := map[string]any{
		"goal": "resume coverage test",
		"cases": []map[string]any{
			{"id": "tc-resume", "name": "pending case", "target": "/api/new", "method": "GET", "expectation": "ok", "priority": 0.8},
		},
		"project_url": "http://localhost:9999",
	}
	require.NoError(t, s.SavePlan(context.Background(), sess.ID, plan))
	require.NoError(t, s.SaveContract(context.Background(), sess.ID, c))

	cleanup := func() {
		sess.Close()
		_ = s.Close()
	}
	return scfg, sess, cleanup
}

// TestResume_AssessesCoverageWithLoadedContract proves the resume path reloads
// the persisted coverage contract and runs AssessCoverage against it, mirroring
// the run path. With measurement 10% and a 99% line gate, the objective gate
// must force Reached=false deterministically.
func TestResume_AssessesCoverageWithLoadedContract(t *testing.T) {
	cfg, sess, cleanup := newResumableSessionWithContract(t, &contract.Contract{
		Depth:        "standard",
		Scope:        []string{"internal/llm"},
		CoverageGate: contract.Gate{Module: "internal/llm", LineThreshold: 0.99},
	})
	defer cleanup()
	cfg.CoverageFn = func(context.Context, *Session) contract.CoverageMeasurement {
		return contract.CoverageMeasurement{Pct: 0.10, Unit: "line", Known: true}
	}
	// NewSession captured the original CoverageFn at construction time; mirror
	// the override onto the session so Resume observes the 10% measurement.
	sess.coverageFn = cfg.CoverageFn

	err := sess.Resume(context.Background())
	require.NoError(t, err)
	require.NotNil(t, sess.Contract, "resume must load the saved contract")
	require.NotNil(t, sess.Assessment, "resume must assess coverage when contract present")
	assert.False(t, sess.Assessment.Reached, "10% < 99% gate → not reached")
}
