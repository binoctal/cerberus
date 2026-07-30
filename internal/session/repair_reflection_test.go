package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
)

// TestExecuteRepairLoop_AccumulatesRepairReflections: the repair re-judge runs
// Reflexion (Learn) on the replacement results; those reflections are persisted
// by Learn, and their count is accumulated into rp.reflections (it was
// previously discarded with _) so the summary's ReflectionsStored is honest.
func TestExecuteRepairLoop_AccumulatesRepairReflections(t *testing.T) {
	s, err := store.New(":memory:")
	require.NoError(t, err)
	require.NoError(t, store.RunMigrations(context.Background(), s.DB(), "../../migrations"))
	defer func() { _ = s.Close() }()

	cfg := project.DefaultConfig()
	cfg.Project.Name = "test-project"
	// Preset a reflection tool call for the learner prompt only.
	mock := llm.NewMockClient(map[string]string{"default": `{"status":"pass"}`})
	mock.SetToolResponse("Generate reflections", []llm.ToolCall{
		{Name: "report_reflection", Input: map[string]any{
			"type": "success", "diagnosis": "endpoint_drift fixed by correcting the path",
			"strategy": "retry with the corrected endpoint path", "condition_pattern": "GET /users",
			"category": "general_failure",
		}},
	})

	sess, err := NewSession(context.Background(), SessionConfig{
		Mode: ModeRun, Goal: "g", Config: &cfg, Store: s, Client: mock,
		Logger: zap.NewNop(), Gate: nil, ProjectDir: ".",
	})
	require.NoError(t, err)

	rp := &runPhase{
		session:  sess,
		ctx:      context.Background(),
		verdicts: []examiner.FinalVerdict{},
	}
	rp.session.ID = "sess-repair-refl"
	_, err = s.DB().ExecContext(context.Background(),
		`INSERT INTO sessions (id, mode, status, goal, project_name, coverage_pct, stats, started_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		rp.session.ID, "run", "running", "g", "test-project", 0.0, "{}")
	require.NoError(t, err)

	rp.verdicts = []examiner.FinalVerdict{
		{Status: examiner.StatusFail, RedispatchHint: agent.HintEndpointDrift,
			StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "tc-1", Target: "/u", Method: "GET", Service: "api"}}},
	}
	rp.plan = &agent.TestPlan{Goal: "g"}
	rp.repairPlanFn = func(_ context.Context, _ string, _ []repairInput) ([]agent.TestCase, error) {
		return []agent.TestCase{{ID: "repair-tc-1", Target: "/v2/u", Method: "GET", Service: "api", Replaces: "tc-1"}}, nil
	}
	rp.session.Config.Settings.ReplanMaxRounds = 1

	require.NoError(t, rp.executeRepairLoop())

	assert.GreaterOrEqual(t, rp.reflections, 1,
		"the repair round's reflection count is accumulated into rp.reflections (not discarded)")
}
