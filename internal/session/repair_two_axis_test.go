package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/autotest"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
)

// TestExecuteRepairLoop_BothAxesInOneRound: D1's core two-axis design — a fail
// hint (HTTP) and a coverage gap BOTH have work in the same round. The fail
// axis dispatches Scout+Agent (a replacement is appended + re-judged) and the
// coverage axis dispatches AutoTest (RepairedCoverage set), independently, in
// one round. The Agent Assessment stays unchanged (D1 invariant).
func TestExecuteRepairLoop_BothAxesInOneRound(t *testing.T) {
	rp, cleanup := coverageAxisSetup(t) // Contract+coverage gap, AutoTestSafety=auto, Assessment.CoveragePct=0.5
	defer cleanup()

	// Insert the session row so the fail-axis verdict persistence (FK) passes.
	rp.session.ID = "sess-two-axis"
	_, err := rp.session.Store.DB().ExecContext(context.Background(),
		`INSERT INTO sessions (id, mode, status, goal, project_name, coverage_pct, stats, started_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		rp.session.ID, "run", "running", "g", "test-project", 0.0, "{}")
	require.NoError(t, err)

	// Fail axis: an actionable HTTP failure.
	rp.verdicts = []examiner.FinalVerdict{
		{Status: examiner.StatusFail, RedispatchHint: agent.HintEndpointDrift,
			StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "tc-1", Target: "/u", Method: "GET", Service: "api"}}},
	}
	rp.repairPlanFn = func(_ context.Context, _ string, _ []repairInput) ([]agent.TestCase, error) {
		return []agent.TestCase{{ID: "repair-tc-1", Target: "/v2/u", Method: "GET", Service: "api", Replaces: "tc-1"}}, nil
	}

	// Coverage axis seams.
	rp.coverageGapFn = func(_ *autotest.CoverageReport) []autotest.CoverageGap {
		return []autotest.CoverageGap{{File: "a.go", Func: "a.go:L1", Reason: autotest.ReasonZeroCover}}
	}
	cov := &rpStubCov{reports: []*autotest.CoverageReport{
		lineReport(50), // coverage before
		lineReport(80), // processGap verify (gain)
		lineReport(80), // coverage after
	}}
	rp.coverageProvider = cov
	rp.autotestGenerator = &rpStubGen{dir: rp.session.ProjectDir}

	rp.session.Config.Settings.ReplanMaxRounds = 1
	require.NoError(t, rp.executeRepairLoop())

	// Fail axis ran: the replacement was appended to the plan.
	var sawReplacement bool
	for _, c := range rp.plan.Cases {
		if c.Replaces == "tc-1" {
			sawReplacement = true
		}
	}
	assert.True(t, sawReplacement, "fail axis dispatched a replacement")

	// Coverage axis ran: RepairedCoverage set + recovered.
	require.NotNil(t, rp.session.RepairedCoverage)
	assert.True(t, rp.session.CoverageRecovered, "80% >= 70% gate")

	// D1 invariant: Agent Assessment unchanged by either axis.
	assert.Equal(t, 0.5, rp.session.Assessment.CoveragePct)
	assert.False(t, rp.session.Assessment.Reached)

	// Both axes' signals are now visible to the report's failure-hints breakdown.
	// (The coverage axis has no per-case verdict; the fail axis contributes
	// endpoint_drift via the original verdict.)
	require.NotNil(t, rp.verdicts)
}
