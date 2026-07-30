package session

import (
	"sort"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/autotest"
	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/binoctal/cerberus/internal/head/scout"
	"github.com/binoctal/cerberus/internal/memory"
)

// coverKey identifies a coverage gap by its raw (File, Func) tuple exactly as
// emitted by the coverage provider. Func is overloaded: a file:line anchor
// (foo.go:L42) on Go's zero-cover path, or a function name on the no-test-file
// path. It is keyed raw (never normalized) so anchors round-trip. See D1 spec
// §6.1 (region unit, [R5]).
type coverKey struct {
	File, Func string
}

// defaultCoverageDispatchGaps caps how many coverage gaps the repair loop
// dispatches AutoTest for per round (spec §4, decision table; v1 default 3).
const defaultCoverageDispatchGaps = 3

// repairInput mirrors scout.RepairInput for the repairPlanFn seam signature.
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
	// A case that has a replacement (Replaces chain) is shadowed: only the
	// latest replacement in the chain is eligible for the next round (spec
	// §5.3). This prevents re-emitting the original after a hint-change and
	// avoids duplicate TestCase IDs / redundant Agent work.
	replacedIDs := map[string]bool{}
	for _, v := range rp.verdicts {
		if tc := v.StepResult.TestCase; tc != nil && tc.Replaces != "" {
			replacedIDs[tc.Replaces] = true
		}
	}
	var out []scout.RepairInput
	for _, v := range rp.verdicts {
		if v.Status != examiner.StatusFail || v.RedispatchHint == agent.HintNone {
			continue
		}
		tc := v.StepResult.TestCase
		if tc == nil {
			continue
		}
		if replacedIDs[tc.ID] {
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

// hasCoverageGap reports whether the session has a KNOWN coverage gap: an
// Assessment carrying a Gap{Kind:"coverage"}. Scope/pathtype gaps and a !Known
// measurement (provider failure) do NOT qualify — the coverage axis can only
// recover a known shortfall (D1 spec §4 trigger, [R1]). Nil Contract/Assessment
// ⇒ false.
func (rp *runPhase) hasCoverageGap() bool {
	sess := rp.session
	if sess.Contract == nil || sess.Assessment == nil {
		return false
	}
	for _, g := range sess.Assessment.Gaps {
		if g.Kind == "coverage" {
			return true
		}
	}
	return false
}

// coverageGaps discovers candidate coverage gaps from the shared before report.
// It uses the coverageGapFn seam when set (tests inject deterministic gaps);
// otherwise it builds the language provider and calls Gaps(before) plus Go
// NoTestFileGaps(ProjectDir), mirroring autotest_run.go gap discovery.
func (rp *runPhase) coverageGaps(before *autotest.CoverageReport) []autotest.CoverageGap {
	if rp.coverageGapFn != nil {
		return rp.coverageGapFn(before)
	}
	provider := coverageProviderForSession(rp.session)
	gaps := provider.Gaps(before)
	if gcp, ok := provider.(*autotest.GoCoverageProvider); ok {
		gaps = append(gaps, gcp.NoTestFileGaps(rp.session.ProjectDir)...)
	}
	return gaps
}

// coverageEligibility selects coverage gaps to dispatch AutoTest for this round.
// It drops gaps already targeted (dedup by raw (File,Func)) and gaps with an
// empty File, ranks by estimated gain (Go: zero-cover block count in gap.File
// descending; Node/Python: uniform — stable discovery order), and caps at the
// dispatch MaxGaps. Pure over (targeted, before). See D1 spec §6.1, §4 [R5][R8].
func (rp *runPhase) coverageEligibility(targeted map[coverKey]bool, before *autotest.CoverageReport) []autotest.CoverageGap {
	all := rp.coverageGaps(before)
	var cand []autotest.CoverageGap
	for _, g := range all {
		if g.File == "" {
			continue
		}
		if targeted[coverKey{File: g.File, Func: g.Func}] {
			continue
		}
		cand = append(cand, g)
	}
	goLang := detectLanguage(rp.session.ProjectDir) == "go"
	sort.SliceStable(cand, func(i, j int) bool {
		if !goLang {
			return false // uniform: keep stable discovery order
		}
		return zeroCoverBlocks(cand[i].File, before) > zeroCoverBlocks(cand[j].File, before)
	})
	if defaultCoverageDispatchGaps > 0 && len(cand) > defaultCoverageDispatchGaps {
		cand = cand[:defaultCoverageDispatchGaps]
	}
	return cand
}

// zeroCoverBlocks counts profile entries in before for file that have Count==0
// — Go's estimated-gain signal (more zero-cover blocks ⇒ more recoverable).
func zeroCoverBlocks(file string, before *autotest.CoverageReport) int {
	if before == nil {
		return 0
	}
	n := 0
	for _, ln := range before.Profile {
		if ln.File == file && ln.Count == 0 {
			n++
		}
	}
	return n
}
