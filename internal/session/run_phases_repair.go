package session

import (
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/binoctal/cerberus/internal/head/scout"
	"github.com/binoctal/cerberus/internal/memory"
)

// repairInput mirrors scout.RepairInput for the repairPlanFn seam signature.
// Keeping a session-local alias avoids leaking scout as a field-level type
// while letting tests construct inputs without importing scout.
type repairInput = scout.RepairInput

// executeRepairLoop closes the in-session Examiner->Scout loop (feature #3):
// each round, collect failures with an actionable RedispatchHint, ask Scout for
// targeted replacements (Replaces), run only those, and re-judge. Bounded by a
// round cap (default 2), the no-progress guard (Task 5; see computeStuck), and
// the inherited token-budget backstop — a DecideWithTools that exhausts the
// budget returns an error, RepairPlan returns (nil, err), and the loop breaks
// via the error branch below (no new budget API). Any error logs and breaks to
// Consolidate — repair never aborts the run.
func (rp *runPhase) executeRepairLoop() error {
	maxRounds := config.ResolveReplanMaxRounds(rp.session.Config.Settings)

	for round := 1; round <= maxRounds; round++ {
		// Recompute stuck each round from the latest verdicts. This is what
		// bounds duplicate processing across rounds: a target whose replacement
		// re-fails with the same hint becomes stuck and is dropped here.
		stuck := computeStuck(rp.verdicts)
		eligible := rp.eligibleFailures(stuck)
		if len(eligible) == 0 {
			break
		}
		rp.session.Logger.Info("repair round", zap.Int("round", round), zap.Int("eligible", len(eligible)))

		replacements, err := rp.callRepairPlan(eligible)
		if err != nil {
			rp.session.Logger.Warn("repair plan failed; stopping repair loop", zap.Error(err))
			return nil
		}
		if len(replacements) == 0 {
			rp.session.Logger.Info("repair plan produced no replacements; stopping")
			return nil
		}

		// Append + persist so resume sees them. A SavePlan failure must break —
		// otherwise PersistFinalVerdicts could write verdicts referencing cases
		// not in the plan.
		rp.plan.Cases = append(rp.plan.Cases, replacements...)
		if err := rp.session.Store.SavePlan(rp.ctx, rp.session.ID, rp.plan); err != nil {
			rp.session.Logger.Warn("save plan (repair) failed; stopping repair loop", zap.Error(err))
			return nil
		}

		// Execute only the replacements.
		loop := rp.buildAgentLoop()
		subPlan := &agent.TestPlan{Goal: rp.plan.Goal, Cases: replacements, ProjectURL: rp.plan.ProjectURL}
		repResults, err := loop.ExecutePlan(rp.ctx, subPlan, rp.session.ID)
		if err != nil {
			rp.session.Logger.Warn("repair execute failed; stopping repair loop", zap.Error(err))
			return nil
		}

		// Re-judge only the replacement results; merge.
		repVerdicts, _, err := rp.buildExaminer().Examine(rp.ctx, repResults, rp.session.ID, rp.session.Config.Project.Name)
		if err != nil {
			rp.session.Logger.Warn("repair re-judge failed; stopping repair loop", zap.Error(err))
			return nil
		}
		rp.results = append(rp.results, repResults...)
		rp.verdicts = append(rp.verdicts, repVerdicts...)
		if _, err := examiner.PersistFinalVerdicts(rp.ctx, rp.session.Store, rp.session.Logger, rp.session.ID, repVerdicts); err != nil {
			rp.session.Logger.Warn("persist repair verdicts failed", zap.Error(err))
		}
	}
	return nil
}

// callRepairPlan routes through the repairPlanFn seam when set (tests), else
// builds a real Scout head and calls RepairPlan.
func (rp *runPhase) callRepairPlan(eligible []repairInput) ([]agent.TestCase, error) {
	if rp.repairPlanFn != nil {
		return rp.repairPlanFn(rp.ctx, rp.session.Goal, eligible)
	}
	scoutHead := scout.NewScout(rp.session.driverFor(&rp.session.scoutDriver), rp.session.Store, rp.session.Config, rp.session.Logger)
	return scoutHead.RepairPlan(rp.ctx, rp.session.Goal, eligible)
}

// eligibleFailures collects Fail verdicts with an actionable hint whose target
// is not marked stuck. Returns RepairInput for Scout.
func (rp *runPhase) eligibleFailures(stuck map[string]bool) []scout.RepairInput {
	var out []scout.RepairInput
	for _, v := range rp.verdicts {
		if v.Status != examiner.StatusFail || v.RedispatchHint == agent.HintNone {
			continue
		}
		tc := v.StepResult.TestCase
		if tc == nil {
			continue
		}
		if stuck[memory.NormalizeTarget(tc.Target)] {
			continue
		}
		out = append(out, scout.RepairInput{Case: *tc, Hint: v.RedispatchHint, Reasoning: v.Reasoning})
	}
	return out
}

// computeStuck returns the set of normalized targets that have made no progress:
// a replacement (Replaces != "") that re-failed with the SAME RedispatchHint as
// its predecessor. A changed hint (e.g. drift->auth) is progress and is NOT
// stuck. The predecessor is found by walking Replaces to the prior verdict.
// Pure (no receiver state) so it can be unit-tested without the LLM/executor
// harness.
func computeStuck(verdicts []examiner.FinalVerdict) map[string]bool {
	byCaseID := map[string]examiner.FinalVerdict{}
	for _, v := range verdicts {
		if v.StepResult.TestCase != nil {
			byCaseID[v.StepResult.TestCase.ID] = v
		}
	}
	stuck := map[string]bool{}
	for _, v := range verdicts {
		tc := v.StepResult.TestCase
		if tc == nil || tc.Replaces == "" || v.Status != examiner.StatusFail || v.RedispatchHint == agent.HintNone {
			continue
		}
		prev, ok := byCaseID[tc.Replaces]
		if !ok {
			continue
		}
		if prev.RedispatchHint == v.RedispatchHint {
			stuck[memory.NormalizeTarget(tc.Target)] = true
		}
	}
	return stuck
}
