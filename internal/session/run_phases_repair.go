package session

import (
	"fmt"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/autotest"
	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/escalation"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/contract"
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

// executeRepairLoop closes the in-session repair loop with TWO independent
// axes (D1 spec §6.3, §7): a fail-hint axis (Examiner→Scout→Agent→re-judge,
// feature #3, unchanged) and a coverage axis (AutoTest dispatch, D1). Each round
// runs whichever axes have work. The loop is bounded by a round cap (default 2),
// the fail no-progress guard (computeStuck), coverage no-progress (delta ≤ 0),
// and the shared escalation gate / budget backstop (added in T7). Any error
// logs and breaks to Consolidate — repair never aborts the run.
func (rp *runPhase) executeRepairLoop() error {
	maxRounds := config.ResolveReplanMaxRounds(rp.session.Config.Settings)
	if rp.session.repairTargeted == nil {
		rp.session.repairTargeted = map[coverKey]bool{}
	}
	// coverageStalled is set when the coverage axis cannot make further progress
	// (no gaps, no delta, or AutoTest disabled/DryRun); it suppresses re-running
	// the coverage axis in later rounds while the fail axis may still continue.
	coverageStalled := false

	for round := 1; round <= maxRounds; round++ {
		if !rp.repairRoundAllowed(round) {
			break
		}
		// Recompute stuck each round from the latest verdicts. This is what
		// bounds duplicate processing across rounds: a target whose replacement
		// re-fails with the same hint becomes stuck and is dropped here.
		stuck := computeStuck(rp.verdicts)
		failEligible := rp.eligibleFailures(stuck)
		hasCov := rp.hasCoverageGap() && !coverageStalled && !rp.session.CoverageRecovered
		if len(failEligible) == 0 && !hasCov {
			break
		}
		rp.session.Logger.Info("repair round",
			zap.Int("round", round),
			zap.Int("fail_eligible", len(failEligible)),
			zap.Bool("coverage_axis", hasCov))

		if len(failEligible) > 0 {
			rp.runFailRepairAxis(failEligible)
		}
		if hasCov && !rp.runCoverageAxis(rp.session.repairTargeted) {
			coverageStalled = true
		}
	}
	return nil
}

// repairRoundAllowed is the explicit per-round gate (D1 spec §6.4): the loop
// stops when the token budget is exhausted, or when the escalation gate returns
// Abort/SkipCase (a human-in-the-loop abort point — NoOpGate continues in CLI
// mode). This replaces the implicit "DecideWithTools error path" budget backstop.
func (rp *runPhase) repairRoundAllowed(round int) bool {
	budget := rp.session.Driver.Budget()
	if budget.Exhausted() {
		rp.session.Logger.Info("repair loop: budget exhausted; stopping",
			zap.Int("remaining", budget.Remaining()))
		return false
	}
	decision := rp.session.Gate.Check(rp.ctx, escalation.Event{
		Type:      "budget_warning",
		Message:   fmt.Sprintf("repair loop round %d: %d tokens remaining", round, budget.Remaining()),
		SessionID: rp.session.ID,
	})
	if decision.Action == escalation.DecisionAbort || decision.Action == escalation.DecisionSkipCase {
		rp.session.Logger.Info("repair loop: escalation aborted the loop", zap.String("action", decision.Action))
		return false
	}
	return true
}

// replacements, run only those, re-judge, and merge. Errors log and stop THIS
// axis only (they no longer short-circuit the coverage axis). Spec §6.3 keeps
// the two axes independent per round.
func (rp *runPhase) runFailRepairAxis(eligible []scout.RepairInput) {
	replacements, err := rp.callRepairPlan(eligible)
	if err != nil {
		rp.session.Logger.Warn("repair plan failed; stopping fail-repair axis", zap.Error(err))
		return
	}
	if len(replacements) == 0 {
		rp.session.Logger.Info("repair plan produced no replacements; stopping fail-repair axis")
		return
	}

	// Append + persist so resume sees them. A SavePlan failure stops the axis —
	// otherwise PersistFinalVerdicts could write verdicts referencing cases not
	// in the plan.
	replacements = inheritClaims(replacements, rp.plan.Cases)
	rp.plan.Cases = append(rp.plan.Cases, replacements...)
	if err := rp.session.Store.SavePlan(rp.ctx, rp.session.ID, rp.plan); err != nil {
		rp.session.Logger.Warn("save plan (repair) failed; stopping fail-repair axis", zap.Error(err))
		return
	}

	// Execute only the replacements.
	loop := rp.buildAgentLoop()
	subPlan := &agent.TestPlan{Goal: rp.plan.Goal, Cases: replacements, ProjectURL: rp.plan.ProjectURL}
	repResults, err := loop.ExecutePlan(rp.ctx, subPlan, rp.session.ID)
	if err != nil {
		rp.session.Logger.Warn("repair execute failed; stopping fail-repair axis", zap.Error(err))
		return
	}

	// Re-judge only the replacement results; merge. The Examiner also runs
	// Reflexion (Learn) on the replacement results — those reflections ARE
	// persisted by Learn; accumulate the count so the summary's
	// ReflectionsStored is honest (it was previously discarded with _).
	repVerdicts, repReflections, err := rp.buildExaminer().Examine(rp.ctx, repResults, rp.session.ID, rp.session.Config.Project.Name)
	if err != nil {
		rp.session.Logger.Warn("repair re-judge failed; stopping fail-repair axis", zap.Error(err))
		return
	}
	rp.reflections += repReflections
	rp.results = append(rp.results, repResults...)
	rp.verdicts = append(rp.verdicts, repVerdicts...)
	if _, err := examiner.PersistFinalVerdicts(rp.ctx, rp.session.Store, rp.session.Logger, rp.session.ID, repVerdicts); err != nil {
		rp.session.Logger.Warn("persist repair verdicts failed", zap.Error(err))
	}
}

// runCoverageAxis runs one coverage-repair round (D1 spec §6.3): skip when
// AutoTest cannot write (DryRun/off/disabled) [R4]; measure the shared `before`
// (1 provider run), select eligible gaps, dispatch AutoTest.RepairGaps (N
// per-gap verify runs), re-measure (1 run) → set RepairedCoverage/
// CoverageRecovered + mark gaps targeted. Returns whether coverage progressed
// (delta > 0); false stalls the coverage axis for later rounds.
func (rp *runPhase) runCoverageAxis(targeted map[coverKey]bool) bool {
	mode := autotest.SafetyMode(rp.session.AutoTestSafety)
	if mode != autotest.SafetyAuto && mode != autotest.SafetyApprove {
		// DryRun/off/disabled: AutoTest writes nothing → coverage cannot
		// progress and must not produce a misleading RepairedCoverage ([R4]).
		return false
	}

	before, _ := rp.measureCoverageReport()
	gaps := rp.coverageEligibility(targeted, before)
	if len(gaps) == 0 {
		return false
	}

	prev := rp.coverageBaseline()
	at := rp.buildAutoTest()
	at.RepairGaps(rp.ctx, rp.session.ProjectDir, before, gaps)

	_, after := rp.measureCoverageReport()
	rp.session.RepairedCoverage = &after
	for _, g := range gaps {
		targeted[coverKey{File: g.File, Func: g.Func}] = true
	}

	threshold := 0.0
	if rp.session.Contract != nil {
		threshold = rp.session.Contract.CoverageGate.LineThreshold
	}
	// Observability-only: never flips the Agent Assessment or any case verdict.
	rp.session.CoverageRecovered = after.Known && after.Pct >= threshold

	return after.Known && after.Pct-prev > 0
}

// coverageBaseline is the coverage progress reference: round 1 uses the
// Agent-only Assessment.CoveragePct; later rounds use the last RepairedCoverage
// measurement (spec §6.3). All values are 0–1 fractions.
func (rp *runPhase) coverageBaseline() float64 {
	if rp.session.RepairedCoverage != nil {
		return rp.session.RepairedCoverage.Pct
	}
	if rp.session.Assessment != nil {
		return rp.session.Assessment.CoveragePct
	}
	return 0
}

// targetedGaps returns the gaps the coverage repair loop has dispatched this
// run, as CoverageGaps for AutoTest.ExcludeTargets. Phase 4 uses these to skip
// already-covered gaps (D1 spec §6.7).
func (rp *runPhase) targetedGaps() []autotest.CoverageGap {
	if len(rp.session.repairTargeted) == 0 {
		return nil
	}
	out := make([]autotest.CoverageGap, 0, len(rp.session.repairTargeted))
	for k := range rp.session.repairTargeted {
		out = append(out, autotest.CoverageGap{File: k.File, Func: k.Func})
	}
	return out
}

// measureCoverageReport runs the coverage provider ONCE and returns the raw
// report (for gap reuse) + measurement. It uses the coverageProvider seam when
// set (tests — shared with buildAutoTest so the per-round RunCoverage cost is
// observable); otherwise the session language provider via lineCoverageReport.
func (rp *runPhase) measureCoverageReport() (*autotest.CoverageReport, contract.CoverageMeasurement) {
	if rp.coverageProvider != nil {
		report, err := rp.coverageProvider.RunCoverage(rp.ctx, rp.session.ProjectDir)
		if err != nil || report == nil {
			return nil, contract.CoverageMeasurement{Known: false}
		}
		return report, measurementFromReport(report)
	}
	return rp.session.lineCoverageReport(rp.ctx)
}

// buildAutoTest constructs the AutoTest used by the coverage repair dispatch
// (spec §6.5): provider + generator from the session language (or the injected
// test seams), the session escalation gate, the FS writer, and SafetyMode from
// settings. Reused by the repair loop only.
func (rp *runPhase) buildAutoTest() *autotest.AutoTest {
	mode := autotest.SafetyMode(rp.session.AutoTestSafety)
	provider := rp.coverageProvider
	if provider == nil {
		provider = coverageProviderForSession(rp.session)
	}
	gen := rp.autotestGenerator
	if gen == nil {
		gen = generatorForLanguage(detectLanguage(rp.session.ProjectDir),
			rp.session.driverFor(&rp.session.scoutDriver), rp.session.Logger)
	}
	return autotest.NewAutoTest(provider, gen,
		autotest.NewEscalationGateAdapter(rp.session.Gate), nil, mode, rp.session.Logger)
}

// generatorForLanguage builds the language-specific test generator. Shared by
// the coverage repair dispatch (buildAutoTest) and the Phase-4 AutoTest.
func generatorForLanguage(lang string, driver *ai.Driver, logger *zap.Logger) autotest.TestGenerator {
	switch lang {
	case "node":
		return autotest.NewNodeTestGenerator(driver)
	case "python":
		return autotest.NewPythonTestGenerator(driver)
	default: // "go" or fallback
		return autotest.NewGoTestGenerator(driver, logger)
	}
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
		if !isRepairable(tc) {
			// The repair_case tool can only emit HTTP or WS shapes; skip case
			// types it cannot correctly replace (process_exec, code, browser,
			// ...) rather than produce a broken HTTP-shaped replacement.
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

// isRepairable reports whether the repair_case mechanism can produce a valid
// corrected replacement for this case type: a plain HTTP case (Method set,
// Action empty) or a WebSocket flow (Steps). Other executor types
// (process_exec/build, code_*, browser, file, mcp_call, wait, navigate) have no
// corresponding repair_case shape — attempting to repair them would silently
// produce a broken HTTP-shaped replacement, so they must be skipped.
func isRepairable(tc *agent.TestCase) bool {
	if tc == nil {
		return false
	}
	if len(tc.Steps) > 0 {
		return true // WebSocket flow
	}
	return tc.Method != "" && tc.Action == "" // plain HTTP
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
	if before == nil {
		// No report (provider failure) → no selectable gaps. Also guards the
		// production provider.Gaps(nil) path from a nil-Profile dereference.
		return nil
	}
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
// empty File, ranks by estimated gain via autotest.RankByGain (Go: zero-cover
// block count in gap.File descending; Node/Python: uniform — stable discovery
// order), and caps at the dispatch MaxGaps. Pure over (targeted, before). See
// D1 spec §6.1, §4 [R5][R8].
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
	cand = autotest.RankByGain(cand, before)
	if defaultCoverageDispatchGaps > 0 && len(cand) > defaultCoverageDispatchGaps {
		cand = cand[:defaultCoverageDispatchGaps]
	}
	return cand
}

// inheritClaims copies claim bindings from the original case onto repair-loop
// replacements (Replaces) and lazy fallbacks (FallbackFor). A repaired case
// must keep proving the promise the original was proving, or claims
// reconciliation silently loses evidence. An explicit binding on the new case
// always wins.
func inheritClaims(newCases []agent.TestCase, originals []agent.TestCase) []agent.TestCase {
	if len(newCases) == 0 {
		return newCases
	}
	byID := make(map[string]agent.TestCase, len(originals))
	for _, o := range originals {
		byID[o.ID] = o
	}
	for i := range newCases {
		nc := &newCases[i]
		if len(nc.Claims) > 0 {
			continue
		}
		orig := nc.Replaces
		if orig == "" {
			orig = nc.FallbackFor
		}
		if orig == "" {
			continue
		}
		if o, ok := byID[orig]; ok && len(o.Claims) > 0 {
			nc.Claims = append([]string(nil), o.Claims...)
		}
	}
	return newCases
}
