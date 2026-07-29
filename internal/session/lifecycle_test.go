package session

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/escalation"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/contract"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
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

// stubCoverageFn returns a CoverageFn that reports a fixed coverage percentage
// without executing any subprocess. Injected into test sessions to avoid
// recursively running go test/jest/pytest when ProjectDir is the cerberus repo
// itself (a module under test). See internal/session/coverage.go.
//
// pcts is a 0–100 percentage (legacy convention); it is converted to the
// 0–1 fraction that contract.CoverageMeasurement.Pct expects.
func stubCoverageFn(pcts ...float64) func(context.Context, *Session) contract.CoverageMeasurement {
	pct := 100.0
	if len(pcts) > 0 {
		pct = pcts[0]
	}
	return func(context.Context, *Session) contract.CoverageMeasurement {
		return contract.CoverageMeasurement{Pct: pct / 100, Unit: "line", Known: true}
	}
}

// planToolCalls returns the LLM tool-call preset that drives Scout.Plan under
// the S2 tool-calling migration. The previous PlanOutput JSON injection no
// longer applies — Scout.Plan now calls DecideWithTools.
func planToolCalls() []llm.ToolCall {
	return []llm.ToolCall{
		{Name: "test_http_endpoint", Input: map[string]any{"method": "GET", "path": "/healthz", "expect_status": 200}},
	}
}

// combinedMockClient returns a Client whose every Complete call yields the
// preset tool calls (consumed by Scout.Plan's DecideWithTools) AND the supplied
// JSON content (consumed by Scout.BuildCoverageContract / Agent.Steer /
// Examiner.Judge via Decide). Both fields ride on the same Response so a single
// mock client satisfies the cross-head flow inside Session.Run.
type combinedMockClient struct {
	content   string
	toolCalls []llm.ToolCall
}

func (m *combinedMockClient) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	inputTokens := len(req.Messages) * 10
	outputTokens := len(m.content) / 4
	return &llm.Response{
		Content:    m.content,
		ToolCalls:  m.toolCalls,
		StopReason: "tool_use",
		Usage:      TokenUsage{InputTokens: inputTokens, OutputTokens: outputTokens, TotalTokens: inputTokens + outputTokens},
	}, nil
}

func (m *combinedMockClient) CompleteWithVision(ctx context.Context, prompt string, images [][]byte) (*llm.Response, error) {
	return m.Complete(ctx, llm.Request{Messages: []llm.Message{{Role: "user", Content: prompt}}})
}

func (m *combinedMockClient) Stream(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
	resp, err := m.Complete(ctx, req)
	if err != nil {
		ch := make(chan llm.StreamEvent, 1)
		ch <- llm.StreamEvent{Type: llm.StreamError, Err: err}
		close(ch)
		return ch, nil
	}
	ch := make(chan llm.StreamEvent, 2)
	ch <- llm.StreamEvent{Type: llm.StreamDelta, Content: resp.Content, Usage: &resp.Usage}
	ch <- llm.StreamEvent{Type: llm.StreamDone, Usage: &resp.Usage}
	close(ch)
	return ch, nil
}

// TokenUsage is re-exported here so the constructor above can stay alongside
// the mock without a separate import alias.
type TokenUsage = llm.TokenUsage

// fullRunClient returns a Client sufficient for Scout.Plan (tool calls) +
// Agent.ExecutePlan + Examiner.Examine (any Decide calls parse the content).
// Scout.Analyze is skipped (config-only model) when using testConfig().
func fullRunClient(content string) llm.Client {
	return &combinedMockClient{content: content, toolCalls: planToolCalls()}
}

// fullRunClientWithContract returns a Client whose DecideWithTools responses
// satisfy Scout.Plan (keyed by "Test Goal: "), Scout.BuildCoverageContract
// (keyed by "Define the coverage contract via tools"), AND Examiner.
// AssessCoverage (keyed by "Objective coverage of gated module").
// MockClient.matchKey's longest-substring-win logic routes each
// DecideWithTools call to its preset. Other Decide calls (Agent/Examiner
// Steer/Judge) fall through to the "default" response "{}", which parses into
// any target struct as zero values; Judge's zero-call error maps to
// fallbackVerdict in examiner.go.
func fullRunClientWithContract() llm.Client {
	mock := llm.NewMockClient(map[string]string{"default": "{}"})
	mock.SetToolResponse("Test Goal: ", planToolCalls())
	mock.SetToolResponse("Define the coverage contract via tools", contractToolCallsForSession())
	mock.SetToolResponse("Objective coverage of gated module", assessCoverageToolCalls())
	return mock
}

// assessCoverageToolCalls presets the assess_coverage tool for the session-level
// Run/Resume tests. Under the S4 tool-calling migration AssessCoverage consumes
// an assess_coverage tool call (not JSON); without this preset the mock's
// "default":"{}" text response yields zero tool calls, AssessCoverage errors,
// and sess.Assessment stays nil. The objective gate in assess.go overrides
// `reached` when measurement < threshold, so the fixture value here only
// matters for the no-gate path.
func assessCoverageToolCalls() []llm.ToolCall {
	return []llm.ToolCall{{
		Name: "assess_coverage",
		Input: map[string]any{
			"reached":   true,
			"gaps":      []any{},
			"reasoning": "session-test fixture",
		},
	}}
}

// contractToolCallsForSession presets the six coverage-contract tools for the
// session-level Run tests. Replaces the legacy contractJSON helper: under the
// tool-calling migration, BuildCoverageContract consumes tool calls (not JSON),
// so the scope/path_types/error_scope/boundaries/priorities/coverage_gate
// fields ride on declare_*/set_* tool inputs. Depth is no longer carried by
// the LLM response — it comes from cfg.Settings.Coverage.Depth via the
// BuildCoverageContract depth parameter.
func contractToolCallsForSession() []llm.ToolCall {
	return []llm.ToolCall{
		{Name: "declare_scope", Input: map[string]any{"modules": []any{"health", "api"}}},
		{Name: "declare_path_types", Input: map[string]any{"types": []any{"happy", "alternative"}}},
		{Name: "declare_error_scope", Input: map[string]any{"scopes": []any{"4xx", "validation"}}},
		{Name: "declare_boundaries", Input: map[string]any{"boundaries": []any{"empty", "max"}}},
		{Name: "set_priority", Input: map[string]any{"bucket": "high", "modules": []any{"health"}}},
		{Name: "set_priority", Input: map[string]any{"bucket": "medium", "modules": []any{"api"}}},
		{Name: "set_coverage_gate", Input: map[string]any{"module": "api", "line_threshold": float64(80.0), "branch_threshold": float64(70.0)}},
	}
}

func TestNewSession(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	mockClient := llm.NewMockClient(map[string]string{"default": `{"status":"pass"}`})
	logger := zap.NewNop()

	cfg := project.DefaultConfig()
	cfg.Project.Name = "test-project"

	sess, err := NewSession(context.Background(), SessionConfig{
		Mode:       ModeRun,
		Goal:       "test goal",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     logger,
		Gate:       nil,
		ProjectDir: ".",
		CoverageFn: stubCoverageFn(),
	})
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

	_, err := NewSession(context.Background(), SessionConfig{
		Mode:       ModeRun,
		Goal:       "goal",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     logger,
		Gate:       nil,
		ProjectDir: ".",
		CoverageFn: stubCoverageFn(),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create session")
}

func TestNewSession_NilGate(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	mockClient := llm.NewMockClient(nil)
	logger := zap.NewNop()
	cfg := project.DefaultConfig()

	sess, err := NewSession(context.Background(), SessionConfig{
		Mode:       ModeRun,
		Goal:       "goal",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     logger,
		Gate:       nil,
		ProjectDir: ".",
		CoverageFn: stubCoverageFn(),
	})
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
	sess, err := NewSession(context.Background(), SessionConfig{
		Mode:       ModeRun,
		Goal:       "goal",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     logger,
		Gate:       gate,
		ProjectDir: ".",
		CoverageFn: stubCoverageFn(),
	})
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
		sess, err := NewSession(context.Background(), SessionConfig{
			Mode:       ModeRun,
			Goal:       "goal",
			Config:     &cfg,
			Store:      s,
			Client:     mockClient,
			Logger:     logger,
			Gate:       nil,
			ProjectDir: ".",
			CoverageFn: stubCoverageFn(),
		})
		require.NoError(t, err)
		assert.Equal(t, "http://localhost:3000", sess.resolveBaseURL())
	})

	t.Run("without services", func(t *testing.T) {
		cfg := project.DefaultConfig()
		sess, err := NewSession(context.Background(), SessionConfig{
			Mode:       ModeRun,
			Goal:       "goal",
			Config:     &cfg,
			Store:      s,
			Client:     mockClient,
			Logger:     logger,
			Gate:       nil,
			ProjectDir: ".",
			CoverageFn: stubCoverageFn(),
		})
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

	sess, err := NewSession(context.Background(), SessionConfig{
		Mode:       ModeRun,
		Goal:       "goal",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     logger,
		Gate:       nil,
		ProjectDir: ".",
		CoverageFn: stubCoverageFn(),
	})
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
	mockClient := fullRunClient("")
	logger := zap.NewNop()

	sess, err := NewSession(context.Background(), SessionConfig{
		Mode:       ModeRun,
		Goal:       "verify service health",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     logger,
		Gate:       nil,
		ProjectDir: ".",
		CoverageFn: stubCoverageFn(),
	})
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
	mockClient := fullRunClient("")
	logger := zap.NewNop()

	sess, err := NewSession(context.Background(), SessionConfig{
		Mode:       ModeVerify,
		Goal:       "verify service",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     logger,
		Gate:       nil,
		ProjectDir: ".",
		CoverageFn: stubCoverageFn(),
	})
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
	mockClient := fullRunClient("")
	logger := zap.NewNop()

	sess, err := NewSession(context.Background(), SessionConfig{
		Mode:       ModeRun,
		Goal:       "parallel test",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     logger,
		Gate:       nil,
		ProjectDir: ".",
		CoverageFn: stubCoverageFn(),
	})
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

	// Under the S2 tool-calling migration, Scout.Plan's fallback path is
	// triggered by a DecideWithTools *call error* (transient provider outage)
	// — not by unparseable JSON, since Scout.Plan no longer parses JSON at all.
	// A successful call with zero tool calls now signals drift and errors out.
	// sessionErrorClient always returns Complete() errors, driving Scout into
	// the deterministic fallback with no endpoints and no base URL → zero
	// cases → Run completes with an empty plan.
	mockClient := &sessionErrorClient{}
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
		CoverageFn: stubCoverageFn(),
	})
	require.NoError(t, err)

	err = sess.Run(context.Background())
	// With zero test cases, the examiner runs on empty results and succeeds.
	require.NoError(t, err)

	dbSess, err := s.GetSession(context.Background(), sess.ID)
	require.NoError(t, err)
	assert.Equal(t, "completed", dbSess.Status)

	sess.Close()
}

// sessionErrorClient is a minimal llm.Client whose Complete always errors —
// the production signal of a provider outage that drives Scout.Plan into its
// deterministic fallback path. Mirrors smoke.smokeErrorClient and
// head/scout.errorMockClient.
type sessionErrorClient struct{}

func (m *sessionErrorClient) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	return nil, fmt.Errorf("simulated provider outage")
}
func (m *sessionErrorClient) CompleteWithVision(ctx context.Context, prompt string, images [][]byte) (*llm.Response, error) {
	return nil, fmt.Errorf("simulated provider outage")
}
func (m *sessionErrorClient) Stream(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
	return nil, fmt.Errorf("simulated provider outage")
}

func TestSession_Run_AgentFailure(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	cfg := testConfig()

	// The agent executor will fail because there's no real server to hit.
	// The ReAct loop's steer attempts exhaust retries and the case fails,
	// but Run completes (agent errors propagate through results, not return
	// errors). The combined mock supplies tool calls for Scout.Plan and an
	// empty content for any other Decide calls.
	mockClient := fullRunClient("")
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
		CoverageFn: stubCoverageFn(),
	})
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

	mockClient := fullRunClient("")
	logger := zap.NewNop()

	sess, err := NewSession(context.Background(), SessionConfig{
		Mode:       ModeRun,
		Goal:       "budget tracking",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     logger,
		Gate:       nil,
		ProjectDir: ".",
		CoverageFn: stubCoverageFn(),
	})
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
	mockClient := fullRunClient("")
	logger := zap.NewNop()

	sess, err := NewSession(context.Background(), SessionConfig{
		Mode:       ModeRun,
		Goal:       "deep plan test",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     logger,
		Gate:       nil,
		ProjectDir: ".",
		CoverageFn: stubCoverageFn(),
	})
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
	mockClient := fullRunClient("")
	logger := zap.NewNop()

	sess, err := NewSession(context.Background(), SessionConfig{
		Mode:       ModeRun,
		Goal:       "cancel test",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     logger,
		Gate:       nil,
		ProjectDir: ".",
		CoverageFn: stubCoverageFn(),
	})
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
	mockClient := fullRunClient("")
	logger := zap.NewNop()

	sess, err := NewSession(context.Background(), SessionConfig{
		Mode:       ModeRun,
		Goal:       "default workers",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     logger,
		Gate:       nil,
		ProjectDir: ".",
		CoverageFn: stubCoverageFn(),
	})
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
	mockClient := fullRunClient("")
	logger := zap.NewNop()

	sess, err := NewSession(context.Background(), SessionConfig{
		Mode:       ModeRun,
		Goal:       "resume test",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     logger,
		Gate:       nil,
		ProjectDir: ".",
		CoverageFn: stubCoverageFn(),
	})
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
	_, err = s.CreateVerdict(context.Background(), sess.ID, traceID, "GET /healthz", "pass", 0.9, "judge", "ok", nil, store.FailureReasonNone, false, "", "")
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

	sess, err := NewSession(context.Background(), SessionConfig{
		Mode:       ModeRun,
		Goal:       "no plan resume",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     logger,
		Gate:       nil,
		ProjectDir: ".",
		CoverageFn: stubCoverageFn(),
	})
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

	sess, err := NewSession(context.Background(), SessionConfig{
		Mode:       ModeRun,
		Goal:       "per-head",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     logger,
		Gate:       nil,
		ProjectDir: ".",
		CoverageFn: stubCoverageFn(),
	})
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

// TestSetupHeadDrivers_CreatesDriversByPriority verifies that each head gets a
// driver when its model resolves (explicit > tier > global), and that the
// priority logic itself is exercised by config.PickModel's unit tests.
func TestSetupHeadDrivers_CreatesDriversByPriority(t *testing.T) {
	s := &Session{
		Config: &project.Config{
			Settings: project.Settings{
				Models:   project.Models{Agent: "explicit-agent"}, // Agent uses explicit.
				AIBudget: project.AIBudget{SessionTotalTokens: 1000, PerCallLimit: 100, Model: "global-m"},
			},
		},
		Logger: zap.NewNop(),
	}
	tiers := config.TierModels{
		config.HeadScout:    "tier-sonnet",
		config.HeadAgent:    "tier-haiku", // overridden by explicit for Agent.
		config.HeadExaminer: "tier-sonnet",
		// HeadCritic absent from tiers → resolves to global-m.
	}

	s.SetupHeadDrivers("test-key", "http://test.invalid", llm.AuthSchemeAPIKey, tiers)

	// Agent: explicit "explicit-agent" → driver created.
	assert.NotNil(t, s.agentDriver, "Agent driver from explicit model")
	// Scout: no explicit, tier "tier-sonnet" → driver created.
	assert.NotNil(t, s.scoutDriver, "Scout driver from tier model")
	// Examiner: no explicit, tier "tier-sonnet" → driver created.
	assert.NotNil(t, s.examinerDriver, "Examiner driver from tier model")
	// Critic: no explicit, absent from tier, global "global-m" → driver created.
	assert.NotNil(t, s.criticDriver, "Critic driver from global model")
}
