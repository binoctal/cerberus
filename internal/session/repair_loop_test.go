package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
)

// TestExecuteRepairLoop_OneRound: given verdicts with one actionable failure
// (HintEndpointDrift) and a repairPlanFn seam returning a deterministic
// replacement, the loop runs exactly one repair round: it requests a
// replacement (Replaces="tc-1"), appends it to the persisted plan, runs it,
// re-judges, and merges the replacement verdict into rp.verdicts.
func TestExecuteRepairLoop_OneRound(t *testing.T) {
	ctx := context.Background()
	rp, cleanup := newTestRunPhase(t)
	defer cleanup()

	rp.session.ID = "sess-repair-1"
	// Insert session row for FK constraint on verdict persistence.
	_, err := rp.session.Store.DB().ExecContext(ctx,
		`INSERT INTO sessions (id, mode, status, goal, project_name, coverage_pct, stats, started_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		rp.session.ID, "run", "running", "test goal", "test-project", 0.0, "{}")
	require.NoError(t, err)

	// Seed an actionable failure verdict.
	rp.verdicts = []examiner.FinalVerdict{
		{
			Status:         examiner.StatusFail,
			RedispatchHint: agent.HintEndpointDrift,
			StepResult: agent.StepResult{
				TestCase: &agent.TestCase{ID: "tc-1", Target: "/users", Method: "GET", Service: "api"},
			},
		},
	}
	// Seed the plan so the loop can append replacements.
	rp.plan = &agent.TestPlan{Goal: "test goal"}

	// Seam: override Scout.RepairPlan with a deterministic replacement.
	rp.repairPlanFn = func(_ context.Context, _ string, _ []repairInput) ([]agent.TestCase, error) {
		return []agent.TestCase{{
			ID:       "repair-tc-1",
			Target:   "/v2/users",
			Method:   "GET",
			Service:  "api",
			Replaces: "tc-1",
		}}, nil
	}

	require.NoError(t, rp.executeRepairLoop())

	// (a) A replacement case (Replaces != "") was appended to the persisted plan.
	var persisted agent.TestPlan
	require.NoError(t, rp.session.Store.LoadPlan(ctx, rp.session.ID, &persisted))
	var sawReplacementInPlan bool
	for _, tc := range persisted.Cases {
		if tc.Replaces == "tc-1" {
			sawReplacementInPlan = true
		}
	}
	require.True(t, sawReplacementInPlan, "replacement case must be appended to persisted plan")

	// (b) A replacement verdict is merged into rp.verdicts.
	var sawReplacementVerdict bool
	for _, v := range rp.verdicts {
		if v.StepResult.TestCase != nil && v.StepResult.TestCase.Replaces == "tc-1" {
			sawReplacementVerdict = true
		}
	}
	require.True(t, sawReplacementVerdict, "replacement verdict must be merged into rp.verdicts")
}

// TestExecuteRepairLoop_NoEligible_NoOp: with no actionable hints, the loop is
// a no-op — existing runs are unaffected.
func TestExecuteRepairLoop_NoEligible_NoOp(t *testing.T) {
	rp, cleanup := newTestRunPhase(t)
	defer cleanup()

	rp.verdicts = []examiner.FinalVerdict{
		{Status: examiner.StatusPass, StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "tc-ok"}}},
	}
	rp.plan = &agent.TestPlan{Goal: "g"}
	before := len(rp.verdicts)

	require.NoError(t, rp.executeRepairLoop())
	require.Len(t, rp.verdicts, before, "no-op when no actionable failures")
}
