package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/binoctal/cerberus/internal/project"
)

// TestRedispatchLoop_EndToEnd proves the in-session Examiner->Scout repair loop
// (feature #3) end-to-end through real production code:
//   - OneRound_Recovered: an actionable failure (HintEndpointDrift) drives
//     executeRepairLoop, which asks the Scout seam for a Replaces replacement,
//     runs it through the REAL agent loop (rule engine -> HTTP against a test
//     server), re-judges through the REAL examiner, and the original target is
//     counted as Recovered (Recovered=1, Failed=0).
//   - SameHintRefail_Stops: when a replacement re-fails with the SAME hint as
//     its predecessor, computeStuck (real production code, invoked inside
//     executeRepairLoop) drops it before another round can request a replacement.
//
// The Agent and Examiner are REAL; only the LLM client is mocked. Scout.RepairPlan
// is driven through the repairPlanFn seam (Task 4) for deterministic output.
func TestRedispatchLoop_EndToEnd(t *testing.T) {
	t.Run("OneRound_Recovered", func(t *testing.T) {
		// REAL target server: the agent's rule engine builds
		// HTTPAction{URL: srv.URL + "/users"}; a 200 response makes the executor
		// return Success -> StepPassed without consuming the LLM.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		ctx := context.Background()
		rp, cleanup := newTestRunPhase(t)
		defer cleanup()

		rp.session.ID = "sess-redispatch-e2e"
		// Bound the loop to one round — the recovery contract is single-round.
		rp.session.Config.Settings.ReplanMaxRounds = 1
		// Wire the service URL so the rule engine (buildAgentLoop) targets the
		// test server for Service="api".
		rp.session.Config.Services = []project.Service{{Name: "api", URL: srv.URL}}

		// Insert session row for FK constraint on verdict/plan persistence.
		_, err := rp.session.Store.DB().ExecContext(ctx,
			`INSERT INTO sessions (id, mode, status, goal, project_name, coverage_pct, stats, started_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
			rp.session.ID, "run", "running", "test goal", "test-project", 0.0, "{}")
		require.NoError(t, err)

		primary := &agent.TestCase{ID: "tc-1", Target: "/users", Method: "GET", Service: "api"}

		// Seed the initial examination output: one actionable failure tagged
		// endpoint_drift. This is the upstream contract for executeRepairLoop
		// (same seeding pattern as repair_loop_test.go).
		rp.verdicts = []examiner.FinalVerdict{{
			Status:         examiner.StatusFail,
			RedispatchHint: agent.HintEndpointDrift,
			StepResult:     agent.StepResult{TestCase: primary},
		}}
		// Seed the primary's own step result so the recovered summary has the
		// full primary+replacement result pair to tally.
		rp.results = []agent.StepResult{{TestCase: primary, Status: agent.StepFailed}}
		rp.plan = &agent.TestPlan{Goal: "test goal"}

		// Seam: deterministic Scout.RepairPlan — emits one replacement targeting
		// the drifted endpoint, declaring Replaces="tc-1".
		rp.repairPlanFn = func(_ context.Context, _ string, _ []repairInput) ([]agent.TestCase, error) {
			return []agent.TestCase{{
				ID:       "repair-tc-1",
				Target:   "/users",
				Method:   "GET",
				Service:  "api",
				Replaces: "tc-1",
			}}, nil
		}

		require.NoError(t, rp.executeRepairLoop())

		// (1) Exactly one replacement case persisted in the plan (one round).
		var persisted agent.TestPlan
		require.NoError(t, rp.session.Store.LoadPlan(ctx, rp.session.ID, &persisted))
		var persistedReplacements []agent.TestCase
		for _, tc := range persisted.Cases {
			if tc.Replaces == "tc-1" {
				persistedReplacements = append(persistedReplacements, tc)
			}
		}
		require.Len(t, persistedReplacements, 1, "one replacement persisted (single round)")
		require.Equal(t, "repair-tc-1", persistedReplacements[0].ID)

		// (2) The replacement step result is StepPassed (real agent execution).
		var repResult *agent.StepResult
		for i := range rp.results {
			if rp.results[i].TestCase != nil && rp.results[i].TestCase.Replaces == "tc-1" {
				repResult = &rp.results[i]
			}
		}
		require.NotNil(t, repResult, "replacement step result merged into rp.results")
		require.Equal(t, agent.StepPassed, repResult.Status, "replacement executed by real agent -> pass")

		// (3) The replacement verdict merged and is pass (real examiner judgment).
		var repVerdict *examiner.FinalVerdict
		for i := range rp.verdicts {
			if rp.verdicts[i].StepResult.TestCase != nil && rp.verdicts[i].StepResult.TestCase.Replaces == "tc-1" {
				repVerdict = &rp.verdicts[i]
			}
		}
		require.NotNil(t, repVerdict, "replacement verdict merged into rp.verdicts")
		require.Equal(t, examiner.StatusPass, repVerdict.Status, "real examiner re-judged replacement -> pass")

		// (4) The recovered summary: original target reclassified OUT of Failed
		// into Recovered; replacement is not an independent unit. This is the
		// real FromResults aggregation consumed by reports.
		summary := FromResults("test goal", srv.URL, 1, rp.results, rp.verdicts, 0, 0, 0)
		require.Equal(t, 1, summary.Recovered, "primary recovered by passed replacement")
		require.Equal(t, 0, summary.Failed, "primary reclassified out of Failed")
		require.Equal(t, 0, summary.Passed, "replacement is not an independent pass unit")
		require.Equal(t, 1, summary.TotalCases, "replacement is not an independent unit")
	})

	// SameHintRefail_Stops: a replacement that re-fails with the SAME
	// RedispatchHint as its predecessor is dropped by computeStuck (real
	// production code invoked inside executeRepairLoop) before another round can
	// request a replacement. The loop is allowed 2 rounds but must run NONE.
	t.Run("SameHintRefail_Stops", func(t *testing.T) {
		rp, cleanup := newTestRunPhase(t)
		defer cleanup()

		rp.session.ID = "sess-redispatch-stuck"
		// Allow two rounds — the guard must still prevent any repair work.
		rp.session.Config.Settings.ReplanMaxRounds = 2

		// Seed the post-round-1 state: primary fail(drift) + replacement
		// re-fail(drift, Replaces=tc-1). computeStuck sees the same hint on
		// both and marks the target stuck.
		rp.verdicts = []examiner.FinalVerdict{
			{
				Status:         examiner.StatusFail,
				RedispatchHint: agent.HintEndpointDrift,
				StepResult: agent.StepResult{
					TestCase: &agent.TestCase{ID: "tc-1", Target: "/users", Method: "GET"},
				},
			},
			{
				Status:         examiner.StatusFail,
				RedispatchHint: agent.HintEndpointDrift, // same hint -> no progress
				StepResult: agent.StepResult{
					TestCase: &agent.TestCase{ID: "repair-tc-1", Target: "/users", Method: "GET", Replaces: "tc-1"},
				},
			},
		}
		rp.plan = &agent.TestPlan{Goal: "test goal"}

		// If the guard fails, the loop would call Scout; the fail flag catches it.
		scoutCalled := false
		rp.repairPlanFn = func(_ context.Context, _ string, _ []repairInput) ([]agent.TestCase, error) {
			scoutCalled = true
			return nil, nil
		}

		require.NoError(t, rp.executeRepairLoop())

		// computeStuck dropped the stuck target -> eligibleFailures is empty ->
		// the loop broke before requesting any replacement.
		require.False(t, scoutCalled, "no repair round should run: same-hint re-fail is stuck")
		require.Len(t, rp.verdicts, 2, "verdict set unchanged (no new replacement merged)")
	})
}
