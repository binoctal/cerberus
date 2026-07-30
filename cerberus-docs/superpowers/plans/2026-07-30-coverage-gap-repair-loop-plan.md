# Coverage-Gap Repair Loop — Implementation Plan (rev 2)

> Plan date: 2026-07-30, **rev 2 (post-review)**. Implements the D1 design spec
> rev 2 (`cerberus-docs/superpowers/specs/2026-07-30-coverage-gap-repair-loop-design.md`).
> Each task is TDD-sized with a negative-verification test. `make check`
> (fmt+lint+test -race) EXIT 0 + clean tree after every task. Commit author
> `binoctal <binoctal@gmail.com>`, no Co-Authored-By, English commit messages.
>
> **Rev 2 changes:** T8 simplified to gap-exclusion only (the `!before.Pass`
> change was redundant — processGap's keep rule reverts failing generated tests
> before they persist); T8↔T9 dependency dissolved (targeted-set is in-memory on
> the session, not persisted); N+2 provider-cost accounting clarified across two
> provider instances; stub-coverage-provider shape specified.

## Sequencing

```
T1 (session fields) ──► T2 (RepairGaps) ──► T4 (lineCoverageReport) ──► T5 (eligibility)
T3 (HintCoverage, independent) ◄────────────────────────────────────┘
T5 ──► T6 (loop integration) ──► T7 (gate+budget) ──► T8 (phase-4 exclusion) ──► T9 (report+persistence) ──► T10 (integration)
```
T8 reads the in-memory targeted-set that T6 stores on the session (same Run, same
object) — no persistence dependency, so T8/T9 are freely ordered.

## Shared test harness (introduced in T2, reused T6/T10)

`processGap` keep/revert and the per-round provider-call count need a **call-counting stub coverage provider** that returns controlled reports. Shape:
```go
type stubCov struct {
    reports []*autotest.CoverageReport // returned in order per RunCoverage call
    calls   int
}
func (s *stubCov) RunCoverage(ctx, dir) (*CoverageReport, error) { r := s.reports[s.calls]; s.calls++; return r, nil }
func (s *stubCov) Gaps(*CoverageReport) []CoverageGap             { ... }
```
T2 builds the AutoTest with this stub as its `coverage` field; T6/T10 inject it
via the `coverageFn`/`repairCoverageFn` seams.

## T1 — Session coverage-repair fields (data model)

**Files:** `internal/session/lifecycle_types.go`.
**Change:** add `Session.RepairedCoverage *contract.CoverageMeasurement` and
`Session.CoverageRecovered bool` (doc comments: observability-only — spec §5.3,
§6.6). Also an unexported in-memory `repairTargeted map[coverKey]bool` (set by T6,
read by T8 — NOT persisted).
**Check [R7]:** the session is JSON-serialized; verify no exact-JSON round-trip test
breaks from the new zero-value fields (run the existing session-persistence tests).
**DoD:** compiles; `make check` green (incl. existing persistence tests).

## T2 — `AutoTest.RepairGaps` (the dispatch primitive) [R2, R3]

**Files:** `internal/autotest/autotest_repair.go` (+ `autotest_repair_test.go`).
**Change:**
```go
// before is the CALLER-SUPPLIED baseline (the session's lineCoverageReport) —
// RepairGaps does NOT run its own baseline, eliminating a redundant provider run.
func (a *AutoTest) RepairGaps(ctx context.Context, projectDir string, before *CoverageReport, gaps []CoverageGap) *AutoTestReport
```
Body: `rep := &AutoTestReport{}; for _, g := range gaps { rep.Items = append(rep.Items, a.processGap(ctx, g, projectDir, before, rep)) }; return rep`. Calls `processGap`
**directly** (bypasses `Run`/`executeSerial`/`executeParallel`).
**Tests (stubCov + mock writer + stub gate):**
- happy: 2 gaps → 2 `processGap` calls; statuses `written`/`reverted` per the keep
  rule (`after.Pass && pct(after) > pct(before)`). stubCov returns a higher-pct
  `after` for gap#1 (keep) and a flat `after` for gap#2 (revert).
- passes the caller's `before`; **does NOT call `RunCoverage` for a baseline**
  (assert `stubCov.calls == len(gaps)` — only the per-gap verify runs, no extra
  baseline). This is the [R3] sharing guarantee.
- `destructive_risk` deny → `skipped`, no write.
- **negative:** implement via `executeSerial` instead → `stubCov.calls` differs /
  path diverges (RED).
**DoD:** reuses `processGap` directly; no own-baseline; safety rails inherited; green + negative red.

## T3 — `agent.HintCoverage` enum (independent) [R1]

**Files:** `internal/head/agent/types.go` (+ `examiner/assembly_redispatch_test.go`).
**Change:** add `HintCoverage RedispatchHint = "coverage"`. **Do not** touch
`parseRedispatchHint` (it is `switch + default`, `assembly.go:41` — no exhaustive-lint issue), the judge tool schema, or the judge prompt.
**Test:** assert `parseRedispatchHint("coverage") == HintNone` (NOT accepted — LLM
never emits it; the enum is for session-synthesized verdicts that round-trip via JSON).
**DoD:** enum added; parser still rejects `coverage`; green.

## T4 — `lineCoverageReport` helper [R3]

**Files:** `internal/session/coverage.go` (+ test).
**Change:** add `func (s *Session) lineCoverageReport(ctx) (*autotest.CoverageReport, contract.CoverageMeasurement)` returning BOTH the raw report (for gap reuse) and the
measurement. Refactor so the provider runs once and both are derived. Keep
`lineCoverage` returning only the measurement (no regression to
`assessCoverageIfContract`). Handle the error/no-report path: return
`(nil, CoverageMeasurement{Known:false})`; callers tolerate a nil report.
**Tests:** injected stub provider returns a fixed report + measurement; both
returned; `lineCoverage` unchanged. Nil-report path → `(nil, Known:false)`.
**DoD:** one provider run → both report + measurement; existing callers unchanged; green.

## T5 — Coverage eligibility [R1, R3, R5, R8]

**Files:** `internal/session/run_phases_repair.go` (+ `repair_loop_test.go`).
**Change:** pure functions
- `func (rp *runPhase) hasCoverageGap() bool` — true iff `sess.Contract != nil &&
  sess.Assessment != nil` AND `Assessment.Gaps` contains `Kind=="coverage"` [R5
  nil-guard].
- `func (rp *runPhase) coverageEligibility(targeted map[coverKey]bool, before *autotest.CoverageReport) []autotest.CoverageGap` — `provider.Gaps(before)` + Go
  `NoTestFileGaps(ProjectDir)`, drop `targeted`/empty-`File`, rank by estimated gain
  (Go: zero-cover block count in `gap.File` from `before.Profile`; Node/Python:
  uniform) [R8], cap at dispatch `MaxGaps` (default 3).
- `coverKey = struct{ File, Func string }` — raw `Func` (anchor or name) [R5].
**Tests (pure, injected report):**
- coverage-gap present → gaps; scope-only `Reached=false` → none; `!Known` → none;
  `Contract==nil`/`Assessment==nil` → none. **negative:** trigger on `!Reached` →
  over-fires (RED).
- `targeted` gaps dropped; empty-File dropped; ranking deterministic.
- Go `Func=="foo.go:L42"` keyed raw. **negative:** normalize Func → RED.
**DoD:** correct + deterministic; green + negatives red.

## T6 — Repair-loop integration (the coverage axis) [R3, R4]

**Files:** `internal/session/run_phases_repair.go` (+ test).
**Change:** in `executeRepairLoop`, per round after the fail-repair block, add the
coverage axis (spec §6.3): `hasCoverageGap` → DryRun skip → shared
`lineCoverageReport` → `coverageEligibility` → `RepairGaps(before, gaps)` →
re-measure `lineCoverage` → set `RepairedCoverage`/`CoverageRecovered` + append to
`repairTargeted` → no-progress break. Update round-continuation. Add helpers:
`buildAutoTest()` (spec §6.5) and `coverageBaseline()` (round-1 = `Assessment.CoveragePct`, else last `RepairedCoverage.Pct`).
**Cost model [R3, explicit]:** per round = **1 session-provider run** (the shared
`before`, from `lineCoverageReport`) + **N AutoTest-provider runs** (processGap's
per-gap verify) + **1 session-provider run** (the after re-measure) = **N+2 across
two provider instances**. T2's `before` parameter is what avoids a 3rd
AutoTest-baseline run.
**Tests (mock via a `repairCoverageFn` seam, like `repairPlanFn`; stubCov):**
- one round: coverage gap → dispatch → coverage rises → `CoverageRecovered` +
  `RepairedCoverage` set; `Assessment` UNCHANGED (D1 invariant).
- DryRun → axis skipped, no dispatch. **negative:** remove skip → dispatch runs (RED).
- no-progress: delta 0 → break. **negative:** remove break → spins to cap (RED).
- provider-call count: N gaps ⇒ total `RunCoverage` == N+2 (session provider 2 +
  AutoTest provider N). **negative:** drop shared-before (AutoTest runs its own
  baseline) → N+3 (RED).
**DoD:** coverage axis wired, bounded, invariant held; green + negatives red.

## T7 — Escalation gate + explicit budget backstop (absorbed #2)

**Files:** `internal/session/run_phases_repair.go` (+ test); `internal/config/tot.go`.
**Change:** before each round, consult `rp.session.Gate`
(`DecisionAbort`/`DecisionSkipCase` → stop) and an explicit budget check
(`ResolveReplanBudget`, or reuse the driver budget). Replaces the implicit
"DecideWithTools error path" at `run_phases_repair.go:46`.
**Tests:** gate Abort → stops before next round; budget exhausted → stops.
**negative:** remove gate → ignores Abort (RED).
**DoD:** loop bounded by gate+budget independent of the round cap; green + negative red.

## T8 — Phase-4 gap exclusion (in-memory) [R6, rev 2: simplified]

**Files:** `internal/session/run_phases_autotest.go` (+ test).
**Change:** Phase-4 gap selection (`at.Run` → `rep.Gaps`) skips gaps in the
session's in-memory `repairTargeted` set (populated by T6). Pass the targeted-set
into the AutoTest construction or filter `rep.Gaps` after discovery.
**Rev-2 scope cut:** do **NOT** change `AutoTest.Run`'s `!before.Pass` behavior.
processGap's keep rule (`!after.Pass → revert`, `autotest_gap.go`) already reverts
failing generated tests before they persist, so loop-written tests never break
Phase 4's baseline. The `!before.Pass` abort is pre-existing behavior for
pre-existing failures, out of scope.
**Tests:** after the loop targets gaps, Phase 4's gap set excludes them.
**negative:** no exclusion → reselect (RED).
**DoD:** Phase 4 doesn't redo the loop's gaps; green + negative red.

## T9 — Reporting + run-path persistence [R7, R10, rev 2]

**Files:** `internal/session/summary.go` + report helpers; session store/JSON.
**Change:**
- Render the `CoverageRecovered` annotation: "Agent coverage X% (not reached) →
  repaired to Y% (recovered)". Observability-only — no verdict/exit-code change.
- Persist `RepairedCoverage`/`CoverageRecovered` on the session row **for the
  report**. **Rev-2:** do NOT persist `repairTargeted` (it is in-memory, run-path
  only; resume does not re-run the loop — spec §8).
**Tests:** report renders the annotation when `CoverageRecovered`; `Assessment.Reached` still shown false; round-trips on the run path.
**negative:** annotation flips a verdict → RED.
**DoD:** report honest; persistence round-trips; green.

## T10 — End-to-end integration

**Files:** extend `internal/session/reflexion_integration_test.go` pattern.
**Change:** one full round with stubCov + mock generator/writer/gate (via the T6
seams) — coverage gap → `RepairGaps` writes a test → re-measure rises →
`CoverageRecovered`; plus a no-progress stop path. No real toolchain.
**DoD:** green; demonstrates the closed loop end-to-end.

## Cross-cutting

- **Four-surface checklist:** N/A — D1 adds no ws_* action field and no
  LLM-authored hint (`HintCoverage` is session-synthesized, verified by T3).
- **D1 invariant** (Agent `Assessment` never overwritten) is load-bearing; T6 + T9
  each carry a negative test.
- **goimports gotcha:** `make fmt` after every task; `git status --short` clean;
  land a chore fmt commit if dirty.
- **Tests must go red:** every task's negative branch demonstrated before merge.
- **Resume:** coverage repair is non-resumable in v1 (spec §8) — do not wire the
  loop into `resume_phases_run.go`.
