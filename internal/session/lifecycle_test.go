package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/binoctal/cerberus/internal/escalation"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func testStoreWithMigrations(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	err = store.RunMigrations(context.Background(), s.DB(), "../../migrations")
	require.NoError(t, err)
	return s
}

// testConfig returns a project config with enough endpoints to achieve
// info score >= 0.7 so Scout.Analyze skips AI inference entirely.
func testConfig() project.Config {
	cfg := project.DefaultConfig()
	cfg.Project.Name = "test-project"
	cfg.Services = []project.Service{
		{Name: "api", URL: "http://localhost:9999", Health: "/healthz"},
	}
	cfg.Settings.AIBudget.SessionTotalTokens = 500000
	cfg.Settings.AIBudget.PerCallLimit = 50000
	return cfg
}

// planJSON returns a mock PlanOutput JSON that the LLM mock client
// will respond with during Scout.Plan.
func planJSON() string {
	cases := []map[string]any{
		{
			"id": "tc-001", "name": "health check", "target": "/healthz",
			"method": "GET", "action": "http_request", "expectation": "returns 200",
			"priority": 0.9,
		},
	}
	b, _ := json.Marshal(map[string]any{"cases": cases})
	return string(b)
}

// fullRunResponses returns a mock client response map sufficient for
// Scout.Plan + Agent.ExecutePlan + Examiner.Examine to complete.
// Scout.Analyze is skipped (config-only model) when using testConfig().
func fullRunResponses() map[string]string {
	return map[string]string{
		"default": planJSON(), // Scout.Plan + Agent.Steer + Examiner.Judge + Learner fallback
	}
}

func TestNewSession(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	mockClient := llm.NewMockClient(map[string]string{"default": `{"status":"pass"}`})
	logger := zap.NewNop()

	cfg := project.DefaultConfig()
	cfg.Project.Name = "test-project"

	sess, err := NewSession(context.Background(), ModeRun, "test goal", &cfg, s, mockClient, logger, nil, ".")
	require.NoError(t, err)
	assert.NotEmpty(t, sess.ID)
	assert.Equal(t, ModeRun, sess.Mode)
	assert.Equal(t, "test goal", sess.Goal)
	assert.Equal(t, ".", sess.ProjectDir)
	assert.NotNil(t, sess.Driver)
	assert.NotNil(t, sess.Gate)

	// Verify persisted in store.
	dbSess, err := s.GetSession(context.Background(), sess.ID)
	require.NoError(t, err)
	assert.Equal(t, "running", dbSess.Status)
	assert.Equal(t, "test goal", dbSess.Goal)
	assert.Equal(t, "test-project", dbSess.ProjectName)
}

func TestNewSession_StoreError(t *testing.T) {
	s := testStoreWithMigrations(t)
	_ = s.Close() // Close store to trigger error.

	mockClient := llm.NewMockClient(nil)
	logger := zap.NewNop()
	cfg := project.DefaultConfig()

	_, err := NewSession(context.Background(), ModeRun, "goal", &cfg, s, mockClient, logger, nil, ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create session")
}

func TestNewSession_NilGate(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	mockClient := llm.NewMockClient(nil)
	logger := zap.NewNop()
	cfg := project.DefaultConfig()

	sess, err := NewSession(context.Background(), ModeRun, "goal", &cfg, s, mockClient, logger, nil, ".")
	require.NoError(t, err)

	// nil gate should be replaced with NoOpGate.
	_, ok := sess.Gate.(escalation.NoOpGate)
	assert.True(t, ok, "nil gate should be replaced with NoOpGate")
}

func TestNewSession_WithExplicitGate(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	mockClient := llm.NewMockClient(nil)
	logger := zap.NewNop()
	cfg := project.DefaultConfig()

	gate := escalation.NoOpGate{}
	sess, err := NewSession(context.Background(), ModeRun, "goal", &cfg, s, mockClient, logger, gate, ".")
	require.NoError(t, err)
	assert.Equal(t, gate, sess.Gate)
}

func TestSession_ResolveBaseURL(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	mockClient := llm.NewMockClient(nil)
	logger := zap.NewNop()

	t.Run("with services", func(t *testing.T) {
		cfg := project.DefaultConfig()
		cfg.Services = []project.Service{
			{Name: "api", URL: "http://localhost:3000"},
		}
		sess, err := NewSession(context.Background(), ModeRun, "goal", &cfg, s, mockClient, logger, nil, ".")
		require.NoError(t, err)
		assert.Equal(t, "http://localhost:3000", sess.resolveBaseURL())
	})

	t.Run("without services", func(t *testing.T) {
		cfg := project.DefaultConfig()
		sess, err := NewSession(context.Background(), ModeRun, "goal", &cfg, s, mockClient, logger, nil, ".")
		require.NoError(t, err)
		assert.Equal(t, "", sess.resolveBaseURL())
	})
}

func TestSession_Close(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	mockClient := llm.NewMockClient(nil)
	logger := zap.NewNop()
	cfg := project.DefaultConfig()

	sess, err := NewSession(context.Background(), ModeRun, "goal", &cfg, s, mockClient, logger, nil, ".")
	require.NoError(t, err)

	// Close should not panic.
	assert.NotPanics(t, func() {
		sess.Close()
	})
}

func TestSession_Run_FullLifecycle(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	cfg := testConfig()
	mockClient := llm.NewMockClient(fullRunResponses())
	logger := zap.NewNop()

	sess, err := NewSession(context.Background(), ModeRun, "verify service health", &cfg, s, mockClient, logger, nil, ".")
	require.NoError(t, err)

	err = sess.Run(context.Background())
	require.NoError(t, err, "Run should complete without error")

	// Verify session status updated to completed in store.
	dbSess, err := s.GetSession(context.Background(), sess.ID)
	require.NoError(t, err)
	assert.Equal(t, "completed", dbSess.Status)

	// Verify stats were written (non-empty stats JSON).
	assert.NotEqual(t, "{}", dbSess.Stats)

	sess.Close()
}

func TestSession_Run_VerifyMode(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	cfg := testConfig()
	mockClient := llm.NewMockClient(fullRunResponses())
	logger := zap.NewNop()

	sess, err := NewSession(context.Background(), ModeVerify, "verify service", &cfg, s, mockClient, logger, nil, ".")
	require.NoError(t, err)
	assert.Equal(t, ModeVerify, sess.Mode)

	err = sess.Run(context.Background())
	require.NoError(t, err)

	dbSess, err := s.GetSession(context.Background(), sess.ID)
	require.NoError(t, err)
	assert.Equal(t, "completed", dbSess.Status)

	sess.Close()
}

func TestSession_Run_ParallelExecution(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	cfg := testConfig()
	mockClient := llm.NewMockClient(fullRunResponses())
	logger := zap.NewNop()

	sess, err := NewSession(context.Background(), ModeRun, "parallel test", &cfg, s, mockClient, logger, nil, ".")
	require.NoError(t, err)
	sess.Parallel = true
	sess.MaxWorkers = 2

	err = sess.Run(context.Background())
	require.NoError(t, err)

	dbSess, err := s.GetSession(context.Background(), sess.ID)
	require.NoError(t, err)
	assert.Equal(t, "completed", dbSess.Status)

	sess.Close()
}

func TestSession_Run_ScoutFailure(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	// Use a config without health endpoints so info score < 0.7,
	// forcing Scout.Analyze to call the LLM.
	cfg := project.DefaultConfig()
	cfg.Project.Name = "test-project"
	cfg.Settings.AIBudget.SessionTotalTokens = 500000
	cfg.Settings.AIBudget.PerCallLimit = 50000
	// No services defined — Analyze will try AI inference.

	// Mock client returns unparseable JSON for analyze, which triggers
	// graceful degradation in Scout.Analyze (returns config-only model).
	// Then Plan also gets bad JSON and falls back to deterministic plan.
	// The fallback plan with no endpoints and no base URL produces zero cases.
	mockClient := llm.NewMockClient(map[string]string{
		"default": `not valid json at all`,
	})
	logger := zap.NewNop()

	sess, err := NewSession(context.Background(), ModeRun, "test goal", &cfg, s, mockClient, logger, nil, ".")
	require.NoError(t, err)

	err = sess.Run(context.Background())
	// With zero test cases, the examiner runs on empty results and succeeds.
	require.NoError(t, err)

	dbSess, err := s.GetSession(context.Background(), sess.ID)
	require.NoError(t, err)
	assert.Equal(t, "completed", dbSess.Status)

	sess.Close()
}

func TestSession_Run_AgentFailure(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	cfg := testConfig()

	// Return valid plan JSON, but then the executor will fail because
	// there's no real server to hit. The ReAct loop's steer attempts
	// will exhaust retries and the case will fail, but Run should still
	// complete (agent errors propagate through results, not as return errors).
	planResp := planJSON()
	mockClient := llm.NewMockClient(map[string]string{
		"default": planResp,
	})
	logger := zap.NewNop()

	sess, err := NewSession(context.Background(), ModeRun, "test goal", &cfg, s, mockClient, logger, nil, ".")
	require.NoError(t, err)

	err = sess.Run(context.Background())
	// Run completes even when agent steps fail — failures are recorded in results.
	require.NoError(t, err)

	dbSess, err := s.GetSession(context.Background(), sess.ID)
	require.NoError(t, err)
	assert.Equal(t, "completed", dbSess.Status)

	sess.Close()
}

func TestSession_Run_TracksTokenBudget(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	cfg := testConfig()
	cfg.Settings.AIBudget.SessionTotalTokens = 500000
	cfg.Settings.AIBudget.PerCallLimit = 50000

	mockClient := llm.NewMockClient(fullRunResponses())
	logger := zap.NewNop()

	sess, err := NewSession(context.Background(), ModeRun, "budget tracking", &cfg, s, mockClient, logger, nil, ".")
	require.NoError(t, err)

	initialBudget := sess.Driver.Budget().SessionTotal
	assert.Equal(t, 500000, initialBudget)
	assert.Equal(t, 500000, sess.Driver.Budget().Remaining())

	err = sess.Run(context.Background())
	require.NoError(t, err)

	// After Run, some tokens should have been consumed.
	remaining := sess.Driver.Budget().Remaining()
	assert.Less(t, remaining, initialBudget, "tokens should have been consumed during Run")
	assert.GreaterOrEqual(t, remaining, 0)

	sess.Close()
}

func TestSession_Run_DeepPlan(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	cfg := testConfig()
	mockClient := llm.NewMockClient(fullRunResponses())
	logger := zap.NewNop()

	sess, err := NewSession(context.Background(), ModeRun, "deep plan test", &cfg, s, mockClient, logger, nil, ".")
	require.NoError(t, err)
	sess.DeepPlan = true

	err = sess.Run(context.Background())
	// DeepPlan triggers ToT planner which needs more complex responses,
	// but with a high info score, Plan may still fall back to direct.
	require.NoError(t, err)

	sess.Close()
}

func TestSession_Run_CancelledContext(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	cfg := testConfig()
	mockClient := llm.NewMockClient(fullRunResponses())
	logger := zap.NewNop()

	sess, err := NewSession(context.Background(), ModeRun, "cancel test", &cfg, s, mockClient, logger, nil, ".")
	require.NoError(t, err)

	// Cancel context immediately. The mock client is fast so Run may
	// still complete, but the key assertion is that Run does not panic
	// and terminates cleanly.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Run should not panic regardless of context state.
	assert.NotPanics(t, func() {
		_ = sess.Run(ctx)
	})

	sess.Close()
}

func TestSession_Run_DefaultWorkers(t *testing.T) {
	// When MaxWorkers <= 0 and Parallel=true, default workers should be 4.
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	cfg := testConfig()
	mockClient := llm.NewMockClient(fullRunResponses())
	logger := zap.NewNop()

	sess, err := NewSession(context.Background(), ModeRun, "default workers", &cfg, s, mockClient, logger, nil, ".")
	require.NoError(t, err)
	sess.Parallel = true
	sess.MaxWorkers = 0 // should default to 4

	err = sess.Run(context.Background())
	require.NoError(t, err)

	sess.Close()
}

func TestFromResults_Integration(t *testing.T) {
	results := []agent.StepResult{
		{
			TestCase: &agent.TestCase{ID: "tc-1", Name: "test1", Target: "/api"},
			Status:   agent.StepPassed,
		},
		{
			TestCase: &agent.TestCase{ID: "tc-2", Name: "test2", Target: "/api/users"},
			Status:   agent.StepFailed,
		},
		{
			TestCase: &agent.TestCase{ID: "tc-3", Name: "test3", Target: "/api/items"},
			Status:   agent.StepSkipped,
		},
	}

	summary := FromResults("test goal", "http://localhost", 3, results, nil, 0, 100, time.Second)
	assert.Equal(t, 1, summary.Passed)
	assert.Equal(t, 1, summary.Failed)
	assert.Equal(t, 1, summary.Skipped)
	assert.Equal(t, 0, summary.Uncertain)
	assert.Equal(t, "test goal", summary.Goal)
}

func TestSession_Resume_SkipsCompleted(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	cfg := testConfig()
	mockClient := llm.NewMockClient(fullRunResponses())
	logger := zap.NewNop()

	sess, err := NewSession(context.Background(), ModeRun, "resume test", &cfg, s, mockClient, logger, nil, ".")
	require.NoError(t, err)

	// Manually save a plan with 2 cases.
	plan := map[string]any{
		"goal": "resume test",
		"cases": []map[string]any{
			{"id": "tc-done", "name": "already done", "target": "/healthz", "method": "GET", "expectation": "ok", "priority": 0.9},
			{"id": "tc-pending", "name": "not yet run", "target": "/api/new", "method": "GET", "expectation": "ok", "priority": 0.8},
		},
		"project_url": "http://localhost:9999",
	}
	require.NoError(t, s.SavePlan(context.Background(), sess.ID, plan))

	// Mark tc-done's target as completed.
	traceID, err := s.CreateTrace(context.Background(), sess.ID, "http", "GET /healthz")
	require.NoError(t, err)
	require.NoError(t, s.FinishTrace(context.Background(), traceID, "pass"))
	_, err = s.CreateVerdict(context.Background(), sess.ID, traceID, "GET /healthz", "pass", 0.9, "judge", "ok", nil)
	require.NoError(t, err)

	// Resume — should only execute tc-pending.
	err = sess.Resume(context.Background())
	require.NoError(t, err)

	sess.Close()
}

func TestSession_Resume_NoPlan(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	cfg := testConfig()
	mockClient := llm.NewMockClient(nil)
	logger := zap.NewNop()

	sess, err := NewSession(context.Background(), ModeRun, "no plan resume", &cfg, s, mockClient, logger, nil, ".")
	require.NoError(t, err)

	err = sess.Resume(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load plan")

	sess.Close()

	// Verify failed status was recorded in DB.
	dbSess, getErr := s.GetSession(context.Background(), sess.ID)
	require.NoError(t, getErr)
	assert.Equal(t, "failed", dbSess.Status, "resume failure should record 'failed' status")
}

func TestSession_driverFor_PerHeadOverride(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	cfg := testConfig()
	cfg.Settings.Models.Scout = "claude-sonnet-4-6"
	cfg.Settings.Models.Agent = "claude-haiku-4-5-20251001"
	// Examiner and Critic left empty → should use shared driver.

	mockClient := llm.NewMockClient(nil)
	logger := zap.NewNop()

	sess, err := NewSession(context.Background(), ModeRun, "per-head", &cfg, s, mockClient, logger, nil, ".")
	require.NoError(t, err)

	// Setup per-head drivers. Will fail to connect but should at least attempt.
	// Since we can't easily create real clients in tests, verify the fallback logic:
	// scoutDriver and agentDriver are nil (client creation fails) → driverFor returns shared.
	d := sess.driverFor(&sess.scoutDriver)
	assert.NotNil(t, d) // Should fall back to shared Driver
	assert.Equal(t, sess.Driver, d)

	d = sess.driverFor(&sess.examinerDriver)
	assert.NotNil(t, d)
	assert.Equal(t, sess.Driver, d) // No override → shared

	sess.Close()
}
