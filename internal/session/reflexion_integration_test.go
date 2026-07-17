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
	"github.com/binoctal/cerberus/internal/memory"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
)

// TestReflexionTwoSessionLoop tests the reflexion loop across two sessions:
// Session 1: consolidate writes episodic + procedural memories
// Session 2: can retrieve episodic memories by target (what Scout.buildEpisodicContext uses)
func TestReflexionTwoSessionLoop(t *testing.T) {
	ctx := context.Background()

	// === Session 1: Run and Consolidate ===

	// Create in-memory store
	s1, err := store.New(":memory:")
	require.NoError(t, err)
	err = store.RunMigrations(context.Background(), s1.DB(), "../../migrations")
	require.NoError(t, err)
	defer func() { _ = s1.Close() }()

	cfg1 := project.DefaultConfig()
	cfg1.Project.Name = "test-project"

	mockClient1 := llm.NewMockClient(map[string]string{"default": `{"status":"pass"}`})
	logger1 := zap.NewNop()

	sess1, err := NewSession(context.Background(), SessionConfig{
		Mode:       ModeRun,
		Goal:       "test authentication flow",
		Config:     &cfg1,
		Store:      s1,
		Client:     mockClient1,
		Logger:     logger1,
		Gate:       nil,
		ProjectDir: ".",
		CoverageFn: func(context.Context, *Session) contract.CoverageMeasurement {
			return contract.CoverageMeasurement{Pct: 1.0, Unit: "line", Known: true}
		},
	})
	require.NoError(t, err)
	sess1.ID = "sess-reflexion-1"

	_, err = sess1.Store.DB().ExecContext(ctx,
		`INSERT INTO sessions (id, mode, status, goal, project_name, coverage_pct, stats, started_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		sess1.ID, "run", "running", "test authentication flow", "test-project", 0.0, "{}")
	require.NoError(t, err)

	// Create verdicts simulating test execution
	rp1 := &runPhase{
		session: sess1,
		ctx:     ctx,
		verdicts: []examiner.FinalVerdict{
			{Status: examiner.StatusPass, StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "tc-1", Target: "POST /api/auth/login"}}},
			{Status: examiner.StatusFail, StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "tc-2", Target: "GET /api/users"}}},
		},
	}

	// Execute consolidate phase
	err = rp1.executeConsolidatePhase()
	require.NoError(t, err)

	// Assert episodic memories written
	var episodicCount int
	err = sess1.Store.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_episodic WHERE session_id=?`, sess1.ID).Scan(&episodicCount)
	require.NoError(t, err)
	require.Equal(t, 2, episodicCount, "session 1 should write 2 episodic rows")

	// Verify episodic content
	var episodic store.EpisodicMemory
	err = sess1.Store.DB().QueryRowContext(ctx,
		`SELECT id, session_id, target, status, COALESCE(duration_ms,0) FROM memory_episodic
		 WHERE session_id=? AND target=? LIMIT 1`,
		sess1.ID, memory.NormalizeTarget("POST /api/auth/login")).Scan(
		&episodic.ID, &episodic.SessionID, &episodic.Target, &episodic.Status, &episodic.DurationMs)
	require.NoError(t, err)
	require.Equal(t, sess1.ID, episodic.SessionID)
	require.Equal(t, memory.NormalizeTarget("POST /api/auth/login"), episodic.Target)
	require.Equal(t, "pass", episodic.Status)

	// Create a procedural memory (simulating Learn reflection)
	procID, err := sess1.Store.StoreProceduralWithType(ctx,
		"check-auth-headers", memory.NormalizeTarget("GET /api/users"), "check authentication headers",
		"test-project", "auth", "failure", []float64{0.1, 0.2}, "test-model")
	require.NoError(t, err)
	require.NotZero(t, procID)

	// Verify procedural created
	var proc store.ProceduralMemory
	err = sess1.Store.DB().QueryRowContext(ctx,
		`SELECT id, condition, action, type FROM memory_procedural WHERE id=?`,
		procID.ID).Scan(&proc.ID, &proc.Condition, &proc.Action, &proc.Type)
	require.NoError(t, err)
	require.Equal(t, "check authentication headers", proc.Action)
	require.Equal(t, "failure", proc.Type)

	// === Session 2: Query Episodic Memory ===

	// In a real scenario, session 2 would use the same database file.
	// Here we test the retrieval mechanism that Scout.buildEpisodicContext uses.

	// Assert episodic memories are retrievable by target (this is what buildEpisodicContext does)
	episodicMemories, err := sess1.Store.GetEpisodicByTarget(ctx, memory.NormalizeTarget("POST /api/auth/login"), 10)
	require.NoError(t, err)
	require.Len(t, episodicMemories, 1, "should retrieve 1 episodic memory for the target")
	require.Equal(t, sess1.ID, episodicMemories[0].SessionID)
	// Note: method is normalized to lowercase
	require.Equal(t, "pass", episodicMemories[0].Status)

	// Assert procedural memory is retrievable
	procLookup, err := sess1.Store.GetProceduralByExactKey(ctx, "test-project", memory.NormalizeTarget("GET /api/users"), "check authentication headers")
	require.NoError(t, err)
	require.NotNil(t, procLookup)
	require.Equal(t, procID.ID, procLookup.ID)
}

// TestReflexionProceduralWithEmbedding tests that procedural memories with embeddings
// are stored correctly and can be retrieved.
func TestReflexionProceduralWithEmbedding(t *testing.T) {
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
	sess.ID = "sess-procedural-embedding"

	_, err = sess.Store.DB().ExecContext(ctx,
		`INSERT INTO sessions (id, mode, status, goal, project_name, coverage_pct, stats, started_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		sess.ID, "run", "running", "test goal", "test-project", 0.0, "{}")
	require.NoError(t, err)

	// Create procedural memory with embedding
	testEmbedding := []float64{0.1, 0.2, 0.3, 0.4}
	procID, err := sess.Store.StoreProceduralWithType(ctx,
		"verify-jwt", memory.NormalizeTarget("POST /api/auth/login"), "verify JWT token",
		"test-project", "auth", "success", testEmbedding, "test-model")
	require.NoError(t, err)
	require.NotZero(t, procID)

	// Verify procedural has embedding
	var embeddingJSON string
	err = sess.Store.DB().QueryRowContext(ctx,
		`SELECT embedding FROM memory_procedural WHERE id=?`, procID.ID).Scan(&embeddingJSON)
	require.NoError(t, err)
	require.NotEmpty(t, embeddingJSON, "procedural should have embedding")

	// Verify procedural is retrievable
	procLookup, err := sess.Store.GetProceduralByExactKey(ctx, "test-project", memory.NormalizeTarget("POST /api/auth/login"), "verify JWT token")
	require.NoError(t, err)
	require.NotNil(t, procLookup)
	require.Equal(t, procID.ID, procLookup.ID)
	require.Equal(t, "verify JWT token", procLookup.Action)
}

// TestReflexionEndToEnd simulates a minimal reflexion loop:
// 1. Run session with verdicts
// 2. Consolidate writes episodic for all verdicts including skip
// 3. Procedural memory is created (simulating Learn)
// 4. Verify both memory types exist and are queryable
func TestReflexionEndToEnd(t *testing.T) {
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
		Goal:       "test API endpoints",
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
	sess.ID = "sess-reflexion-e2e"

	_, err = sess.Store.DB().ExecContext(ctx,
		`INSERT INTO sessions (id, mode, status, goal, project_name, coverage_pct, stats, started_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		sess.ID, "run", "running", "test API endpoints", "test-project", 0.0, "{}")
	require.NoError(t, err)

	// Simulate test execution with mixed results
	rp := &runPhase{
		session: sess,
		ctx:     ctx,
		verdicts: []examiner.FinalVerdict{
			{Status: examiner.StatusPass, StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "tc-1", Target: "GET /api/health"}}},
			{Status: examiner.StatusFail, StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "tc-2", Target: "POST /api/users"}}},
			{Status: examiner.StatusSkip, StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "tc-3", Target: "DELETE /api/admin"}}},
		},
	}

	// Execute consolidate
	err = rp.executeConsolidatePhase()
	require.NoError(t, err)

	// Verify episodic memories for all verdicts (skip is also recorded)
	var episodicCount int
	err = sess.Store.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_episodic WHERE session_id=?`, sess.ID).Scan(&episodicCount)
	require.NoError(t, err)
	require.Equal(t, 3, episodicCount, "should record pass, fail, and skip")

	// Create procedural memory from the failure
	procID, err := sess.Store.StoreProceduralWithType(ctx,
		"check-auth", memory.NormalizeTarget("POST /api/users"), "check authentication before creating",
		"test-project", "auth", "failure", []float64{0.5, 0.6}, "test-model")
	require.NoError(t, err)

	// Verify the reflexion loop components exist
	// 1. Episodic memory exists for all verdicts
	episodicMemories, err := sess.Store.GetEpisodicByTarget(ctx, memory.NormalizeTarget("POST /api/users"), 5)
	require.NoError(t, err)
	require.Len(t, episodicMemories, 1, "should find episodic memory for failed endpoint")
	require.Equal(t, "fail", episodicMemories[0].Status)

	// 2. Procedural memory exists
	procLookup, err := sess.Store.GetProceduralByExactKey(ctx, "test-project", memory.NormalizeTarget("POST /api/users"), "check authentication before creating")
	require.NoError(t, err)
	require.NotNil(t, procLookup)
	require.Equal(t, procID.ID, procLookup.ID)

	// 3. A second session could retrieve both (simulating cross-session learning)
	// In production, Scout.buildEpisodicContext would query GetEpisodicByTarget
	// and Recovery would query GetProceduralByExactKey for embedding similarity
}
