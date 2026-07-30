# Coverage-Gap Repair Loop — Implementation Plan

> Plan date: 2026-07-30. Implements the D1 design spec rev 2
> (`cerberus-docs/superpowers/specs/2026-07-30-coverage-gap-repair-loop-design.md`).
> Each task is TDD-sized with a negative-verification test. Tasks are ordered by
> dependency: data model → dispatch primitive → eligibility → loop integration →
> guards → phase-4 safety → reporting. `make check` (fmt+lint+test -race) EXIT 0 +
> clean tree after every task. Commit author `binoctal`, no Co-Authored-By.

## Sequencing

```
T1 (data model) ──► T2 (RepairGaps) ──► T4 (lineCoverageReport) ──► T5 (eligibility)
                                                                   │
T3 (HintCoverage, independent) ◄──────────────────────────────────┘
T5 ──► T6 (loop integration) ──► T7 (gate+budget) ──► T8 (phase-4 safety) ──► T9 (report+persistence) ──► T10 (integration)
```

## T1 — Session coverage-repair fields (data model)

**Files:** `internal/session/lifecycle_types.go` (+ test).
**Change:** add `Session.RepairedCoverage *contract.CoverageMeasurement` and
`Session.CoverageRecovered bool` with doc comments (observability-only — spec §5.3,
§6.6).
**Test:** struct field presence + zero-value (no behavior yet).
**DoD:** compiles; `make check` green. No negative-verdict needed (pure data).

## T2 — `AutoTest.RepairGaps` (the dispatch primitive) [R2]

**Files:** `internal/autotest/autotest_repair.go` (+ `autotest_repair_test.go`).
**Change:** `func (a *AutoTest) RepairGaps(ctx, projectDir string, before *CoverageReport, gaps []CoverageGap) *AutoTestReport` — allocates `rep`, loops
`a.processGap(ctx, g, projectDir, before, rep)` per gap. Bypasses `Run`/
`executeSerial`/`executeParallel` (canonicalizes on `processGap`).
**Tests (mock writer + stub gate + stub coverage):**
- happy: 2 gaps → 2 `processGap` calls, statuses `written`/`reverted` per keep rule.
- passes the caller-supplied `before` (does NOT call `RunCoverage` for baseline).
- `destructive_risk` deny → `skipped`, no write.
- **negative:** assert it does NOT route through `executeSerial` (e.g. spy: with
  `MaxConcurrency:1`, `RepairGaps` still runs `processGap`, not the serial path) —
  flip to serial dispatch → RED.
**DoD:** `RepairGaps` reuses `processGap` directly; safety rails inherited; tests green + negative red.

## T3 — `agent.HintCoverage` enum (independent) [R1]

**Files:** `internal/head/agent/types.go` (+ `examiner/assembly_redispatch_test.go`).
**Change:** add `HintCoverage RedispatchHint = "coverage"`. **Do not** touch
`parseRedispatchHint`, the judge tool schema, or the judge prompt.
**Test:** extend `assembly_redispatch_test.go` to assert `parseRedispatchHint("coverage") == HintNone` (NOT accepted — LLM never emits it); the enum value exists for
JSON round-trip of session-synthesized verdicts.
**DoD:** enum added; parser still rejects `coverage`; green.

## T4 — `lineCoverageReport` helper [R3]

**Files:** `internal/session/coverage.go` (+ test).
**Change:** add `func (s *Session) lineCoverageReport(ctx) (*autotest.CoverageReport, contract.CoverageMeasurement)` returning BOTH the raw report (for gap reuse) and
the measurement. Refactor `lineCoverage`/`coverageForSession` to share the provider
run. Today `coverageForSession` discards the report — keep a path that retains it.
**Tests:** injected stub provider returns a fixed report + measurement; both
returned; `lineCoverage` still returns just the measurement (no regression).
**DoD:** one provider run yields both report + measurement; existing
`lineCoverage` callers unchanged; green.

## T5 — Coverage eligibility [R1, R3, R5, R8]

**Files:** `internal/session/run_phases_repair.go` (+ `repair_loop_test.go`).
**Change:** pure functions
- `func (rp *runPhase) hasCoverageGap() bool` — true iff `sess.Assessment` has a
  `Gap{Kind:"coverage"}`.
- `func (rp *runPhase) coverageEligibility(targeted map[coverKey]bool, before *autotest.CoverageReport) []autotest.CoverageGap` — `provider.Gaps(before)` + Go
  `NoTestFileGaps`, drop `targeted`/empty-File, rank by estimated gain (Go:
  zero-cover block count in `gap.File`; Node/Python: uniform), cap at dispatch
  `MaxGaps` (default 3).
- `coverKey = struct{ File, Func string }` (raw `Func`, anchor or name).
**Tests (pure, injected report):**
- coverage-gap present → gaps returned; scope-only `Reached=false` → none;
  `!Known` → none. **negative:** trigger on `!Reached` → over-fires (RED).
- `targeted` gaps dropped; empty-File dropped; ranking deterministic.
- Go `Func=="foo.go:L42"` anchors keyed raw. **negative:** normalize Func → RED.
**DoD:** eligibility correct + deterministic; green + negatives red.

## T6 — Repair-loop integration (the coverage axis) [R3, R4]

**Files:** `internal/session/run_phases_repair.go` (+ test).
**Change:** in `executeRepairLoop`, after the fail-repair block per round, add the
coverage axis (spec §6.3): `hasCoverageGap` → DryRun skip → shared
`lineCoverageReport` → `coverageEligibility` → `RepairGaps` → re-measure
`lineCoverage` → set `RepairedCoverage`/`CoverageRecovered` → no-progress break.
Round continuation condition updated. `buildAutoTest()` helper (spec §6.5).
**Tests (mock autotest via a `repairCoverageFn` seam, like `repairPlanFn`):**
- one round: coverage gap → dispatch → coverage rises → `CoverageRecovered` +
  `RepairedCoverage` set; `Assessment` UNCHANGED (D1 invariant).
- DryRun → axis skipped, no dispatch. **negative:** remove skip → dispatch runs (RED).
- no-progress: delta 0 → break. **negative:** remove break → spins to cap (RED).
- provider-call count: N gaps ⇒ `RunCoverage` == N+2/round. **negative:** drop
  shared-before reuse → over-count (RED).
**DoD:** coverage axis wired, bounded, invariant held; green + negatives red.

## T7 — Escalation gate + explicit budget backstop (absorbed #2)

**Files:** `internal/session/run_phases_repair.go` (+ test); `internal/config/tot.go`.
**Change:** before each round, consult the session `escalation.Gate`
(`DecisionAbort`/`DecisionSkipCase` → stop) and an explicit budget check
(`ResolveReplanBudget`, or reuse the driver budget). Replaces the implicit
"DecideWithTools error path" at `run_phases_repair.go:46`.
**Tests:** gate Abort → loop stops before next round; budget exhausted → stops.
**negative:** remove gate → loop ignores Abort (RED).
**DoD:** loop bounded by gate+budget independent of round cap; green + negative red.

## T8 — Phase-4 safety [R6]

**Files:** `internal/session/run_phases_autotest.go`, `internal/autotest/autotest_run.go` (+ tests).
**Change:**
1. Phase-4 gap selection excludes the persisted targeted-set (read from the session
   row written in T9).
2. `AutoTest.Run`'s `!before.Pass` early return degrades to warn + skip generation
   (not `return err`) so a loop-written test that fails compilation doesn't abort
   all polish.
**Tests:**
- after the loop targets gaps, Phase 4's gap set excludes them. **negative:** no
  exclusion → reselect (RED).
- a failing pre-existing test → Phase 4 warns+skips, no `err`. **negative:** abort
  → RED.
**DoD:** Phase 4 coexists with loop-written tests safely; green + negatives red.

## T9 — Reporting + run-path persistence [R7, R10]

**Files:** `internal/session/summary.go` + report helpers; session store/JSON.
**Change:**
- Render the `CoverageRecovered` annotation: "Agent coverage X% (not reached) →
  repaired to Y% (recovered)". Observability-only — no verdict/exit-code change.
- Persist `RepairedCoverage`/`CoverageRecovered` + the targeted-set on the session
  row (for Phase-4 exclusion in T8 and for the report). **Not** for resume (spec §8).
**Tests:** report renders the annotation when `CoverageRecovered`; `Assessment.Reached` still shown false. **negative:** annotation flips a verdict → RED.
**DoD:** report honest; persistence round-trips on the run path; green.

## T10 — End-to-end integration

**Files:** extend `internal/session/reflexion_integration_test.go` pattern.
**Change:** one full round with mock provider/generator/writer/gate — coverage gap
→ `RepairGaps` writes a test → re-measure rises → `CoverageRecovered`; plus a
no-progress stop path. No real toolchain.
**DoD:** green; demonstrates the closed loop end-to-end.

## Cross-cutting

- **Four-surface checklist:** D1 adds no new ws_* action field, so the
  Scout-prompt/judge/config/Steps surfaces do not apply. `HintCoverage` is
  session-synthesized (not LLM-authored) — verified by T3's "parser rejects it" test.
- **D1 invariant** (Agent `Assessment` never overwritten) is the load-bearing
  guard; T6 + T9 both carry a negative test for it.
- **goimports gotcha:** `make fmt` after each task; verify `git status --short`
  clean; land a chore fmt commit if dirty.
- **Tests must go red:** every task's negative branch is demonstrated before merge.
