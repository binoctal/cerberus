# Coverage-Gap Repair Loop — Design Spec

> Feature: D1 (route coverage gaps into the in-session repair loop). Status: design
> spec, 2026-07-30. Depends on the Examiner→Scout repair loop (#3, shipped) and the
> coverage contract + real-line-coverage subsystem (shipped). Strategic basis and
> the fork/granularity rulings: see
> `2026-07-30-examiner-replan-autotest-coverage-thinking.md` (§6 decision record,
> §6.4 blocker finding).

## 1. Problem

The coverage contract gate (`Examiner.AssessCoverage`, `assess.go:18`) can force
`Assessment.Reached=false` when measured coverage is below
`Contract.CoverageGate.LineThreshold`. That verdict is **terminal today**: the
Examiner→Scout repair loop (`executeRepairLoop`, `run_phases_repair.go:24`) keys
eligibility only on per-case `Status==Fail && RedispatchHint!=HintNone`
(`eligibleFailures:99`) and never reads `Assessment`. So a session can finish
"coverage not reached" with no in-session recovery, even though the correction —
write more tests for uncovered code — is exactly what the pipeline can do.

### 1.1 The blocker that shapes this design (see thinking §6.4)

`coverageForSession` (`coverage.go:51`) measures **source-tree unit-test
coverage** over `ProjectDir` (`go test -coverprofile` / jest / pytest). Only
unit-test **files written into ProjectDir** raise it. Repair-loop products are
`TestCase`s (http/ws/process_exec) that exercise a running target — they enter
neither ProjectDir nor its unit-test suite, so they move the gate by **zero**.

Therefore coverage recovery cannot reuse the Scout→Agent→TestCase path. It must
**write unit-test files**, which is AutoTest's generator domain
(`autotest.processGap`, `autotest_gap.go:11`). D1 dispatches AutoTest for coverage
inputs; Scout→Agent stays the mechanism for fail-hints.

## 2. Goal

Close the coverage loop in-session: when the Agent-only gate yields
`Reached=false`, the repair loop drives AutoTest's generator (contract-prioritized
gaps) to write unit tests, re-measures, and repeats until the threshold is met or
a bound fires. Meeting the threshold marks the coverage contract **recovered** —
a distinct outcome; the original Agent `Reached=false` verdict is unchanged (D1
invariant: the gate judges the Agent's work alone, design 2026-07-17 D1).

## 3. Non-goals

- Do **not** flip the Agent-only gate verdict based on AutoTest files. Recovery is
  a separate outcome (`RepairedCoverage` track), not a retroactive Agent pass.
- Do **not** merge AutoTest's standalone Phase-4 pass (`executeAutoTestPhase`) into
  the loop. D1 dispatches AutoTest as a callable; Phase 4 keeps running afterward
  for opportunistic polish. Full unification (Option b) stays deferred (YAGNI).
- Do **not** redefine the gate's measurement semantics (still ProjectDir unit
  coverage). Option (II) from the blocker discussion is out of scope.
- Do **not** teach the per-case LLM judge to emit `coverage` — a single case cannot
  know session coverage. `coverage` is **session-synthesized** (§4.1).

## 4. Decisions (pinned)

| Decision | Choice |
|---|---|
| Loop owner | `executeRepairLoop` becomes a **two-strategy loop**: fail-hints → Scout+Agent (unchanged); coverage-hints → AutoTest dispatch (new). |
| Coverage trigger | `sess.Assessment.Reached==false` (Agent-only gate, measured once at Phase 3). |
| Coverage mechanism | Dispatch `AutoTest.RepairGaps(ctx, dir, gaps)` — writes+verifies unit tests, revert-on-no-gain, escalation-gated (AutoTest's existing safety, unchanged). |
| Hint category | Add `agent.HintCoverage`. **Session-synthesized only** — `parseRedispatchHint` accepts it for forward-compat, but the judge prompt is NOT taught to emit it. |
| Region unit | Reuse `autotest.CoverageGap{File, Func}` (thinking §7). Targeted-set key = `(File, Func)`. |
| Progress measure | Aggregate line-pct delta on a **separate `RepairedCoverage` track** (Agent+AutoTest), NOT per-target, NOT overwriting `Assessment`. |
| Recovered outcome | `RepairedCoverage.Pct >= LineThreshold` ⇒ coverage contract recovered (mirrors fail-repair recovered semantics). Agent `Reached=false` verdict stays. |
| Termination | Round cap (default 2, shared) + fail no-progress (`computeStuck`) + **coverage no-progress** (delta ≤ 0) + escalation gate + explicit budget backstop. |
| Gap ranking | `contract.Priorities[module]` × estimated gain (Go: uncovered `numStmts`; Node/Python: `1/TotalFuncs`). Replaces FIFO `MaxGaps` for the dispatch path. |

## 5. Data Model

### 5.1 `agent.HintCoverage` (new enum value)

`internal/head/agent/types.go`:
```go
HintCoverage RedispatchHint = "coverage" // session-synthesized: coverage gate not reached
```
`parseRedispatchHint` (`examiner/assembly.go:41`) adds `HintCoverage` to the
accepted set so a persisted/reloaded verdict round-trips; missing/unrecognized
still collapses to `HintNone`. **The judge tool schema and prompt are unchanged**
— `coverage` is never LLM-emitted; it is assigned by the session layer (§6.1).

### 5.2 Coverage eligibility — reuses `CoverageGap`, not `RepairInput`

Fail-repair and coverage-repair have **different executors** (Scout+Agent vs
AutoTest), so they do not share `RepairInput` (which is the Scout contract). The
coverage path carries `[]autotest.CoverageGap` directly to the dispatch. No change
to `scout.RepairInput`.

### 5.3 `Session.RepairedCoverage` (new field)

`internal/session/lifecycle_types.go`:
```go
// RepairedCoverage is the post-AutoTest-dispatch coverage (Agent + AutoTest
// tests), measured inside the coverage repair loop. Distinct from Assessment
// (Agent-only gate) so the Agent verdict is never overwritten by AutoTest files.
RepairedCoverage *contract.CoverageMeasurement
```
And a derived recovered flag for the summary/report:
```go
CoverageRecovered bool // RepairedCoverage.Pct >= Contract.CoverageGate.LineThreshold
```

## 6. Components

### 6.1 Coverage eligibility (`run_phases_repair.go`, new)

```go
func (rp *runPhase) eligibleCoverageGaps(targeted map[coverKey]bool) []autotest.CoverageGap
```
- Returns `nil` when `rp.session.Contract == nil` or `rp.session.Assessment.Reached`.
- Runs the provider: `report := provider.RunCoverage(ctx, ProjectDir)`; `gaps :=
  append(provider.Gaps(report), goNoTestFileGaps...)` (Go-only, mirrors
  `autotest_run.go:21-24`).
- Drops gaps already in `targeted` (dedup, absorbed #3) and gaps with empty `File`.
- Ranks by `contract.Priorities[moduleOf(gap.File)]` × estimated gain; returns top-N
  bounded by a per-round cap (default `MaxGaps`, 5).

`coverKey = struct{ File, Func string }` (Func="" ⇒ whole-file target).

### 6.2 AutoTest dispatch (`internal/autotest/`, new public entry)

```go
// RepairGaps generates + writes + verifies unit tests for the given explicit
// (contract-prioritized) gaps, reusing processGap. It skips AutoTest.Run's own
// discovery/ranking (the caller already selected gaps). Safety rails (revert on
// no-gain, destructive_risk escalation) are inherited from processGap unchanged.
func (a *AutoTest) RepairGaps(ctx context.Context, projectDir string, gaps []CoverageGap) (*AutoTestReport, error)
```
Implementation: run `RunCoverage` once for the `before` baseline, then loop
`processGap(ctx, gap, projectDir, before, rep)` per gap (the existing per-gap
pipeline, including `gate.Request("destructive_risk", …)` at `autotest_gap.go:43`
and the keep/revert decision). No new safety path; AutoTest's file-mutation state
stays internal to this call (the "repair never aborts the run" invariant holds —
errors return to the loop, which logs and breaks).

### 6.3 Repair-loop integration (`executeRepairLoop`)

Per round, after the existing fail-repair block:
```go
covGaps := rp.eligibleCoverageGaps(targeted)
if len(covGaps) > 0 {
    prev := rp.coverageBaseline()           // Assessment.CoveragePct round 1, else last RepairedCoverage
    at := rp.buildAutoTest()                // cov+gen+gate+writer, mode from Settings
    if _, err := at.RepairGaps(rp.ctx, rp.session.ProjectDir, covGaps); err != nil {
        rp.session.Logger.Warn("coverage repair dispatch failed; stopping", zap.Error(err))
        return nil
    }
    m := rp.session.lineCoverage(rp.ctx)    // re-measure (Agent + AutoTest now in ProjectDir)
    rp.session.RepairedCoverage = &m
    addAll(targeted, covGaps)
    if rp.session.Contract.CoverageGate.LineThreshold > 0 && m.Known && m.Pct >= threshold {
        rp.session.CoverageRecovered = true
        // threshold met: coverage axis done (fail axis may still need rounds)
    }
    if !m.Known || m.Pct-prev <= 0 {
        // coverage no-progress: stop the coverage axis
        break
    }
}
```
Round continuation requires `len(failEligible)>0 || (hasCoverageGap && !recovered
&& progress)`. Both axes share the round cap, escalation gate (§6.4), and budget.

### 6.4 Escalation gate + budget backstop (absorbed #2)

Before each round, consult the session's `escalation.Gate` (the same interface
AutoTest uses via `EscalationGateAdapter`, `gate_adapter.go:13`): a
`DecisionAbort`/`DecisionSkipCase` stops the loop. Add an explicit token-budget
check (`config.ResolveReplanBudget` or the existing driver budget) as a
first-class round gate, replacing today's implicit "DecideWithTools error path"
(`run_phases_repair.go:46`). This bounds the extra coverage measurements AutoTest
dispatch adds.

### 6.5 buildAutoTest (`run_phases_repair.go`, new shared helper)

Mirrors `buildAgentLoop`/`buildExaminer`: constructs an `*autotest.AutoTest` from
session config (provider via `NewCoverageProviderForLanguage`, generator per lang,
the session's escalation gate, the writer, `SafetyMode` from Settings). Reused by
the repair loop only (Phase 4 keeps its own construction for now).

### 6.6 Recovered wiring (summary / consolidate)

`internal/session/summary.go` / `run_phases_consolidate.go` (which already handle
fail-repair recovered via `Replaces`) gain the coverage analog: when
`CoverageRecovered`, the coverage contract is reported as `recovered` (same badge
as #4 / fail-repair). The Agent's `Assessment.Reached=false` is **not** flipped —
it is reported alongside as "Agent coverage X%, recovered to Y% via AutoTest."
This mirrors A1 Phase 2 / recovered-rendering: recovered marks the contract
covered, not the original verdict rewritten.

## 7. Control Flow

```
Run():
  Scout → Agent → Examiner (Phase 3)
    assessCoverageIfContract → sess.Assessment (Agent-only gate)   [unchanged]
  executeRepairLoop (Phase 3.1):
    for round in 1..maxRounds:
      gate/budget check                                            [new explicit]
      failEligible = eligibleFailures(stuck)                       [unchanged]
      covEligible = eligibleCoverageGaps(targeted)                 [new]
      if both empty: break
      if failEligible:  Scout.RepairPlan → ExecutePlan → Examine   [unchanged]
      if covEligible:   AutoTest.RepairGaps → lineCoverage →       [new]
                        RepairedCoverage; recovered? progress?
  Consolidate (Phase 3.5) — adds CoverageRecovered to recovered set [extended]
  AutoTest (Phase 4) — standalone polish, unchanged                [unchanged]
  buildSummary
```

## 8. Error Handling & Invariants

- **D1 invariant (load-bearing):** `sess.Assessment` is set once at Phase 3
  (Agent-only) and never overwritten by the repair loop. `RepairedCoverage` is a
  separate field. The Agent's coverage verdict is honest; recovery is additive.
- `RepairGaps` error / provider failure → `Known=false` → coverage axis stops
  (no-progress), loop falls through to consolidate. Never aborts the run.
- Escalation `destructive_risk` deny inside `processGap` → the gap is skipped
  (status `skipped`), not a loop abort (existing AutoTest behavior).
- Budget exhaustion → explicit gate stops the loop before the next round.
- On resume: `RepairedCoverage`/`CoverageRecovered` persist on the session row
  (extend the session schema/JSON like `Assessment`); the loop reads them and
  skips gaps already in `targeted` (idempotent, same discipline as fail-repair).

## 9. Out of Scope

- Option (b) full unification (absorb AutoTest Phase 4 into the loop).
- Per-target (per-function) coverage progress measurement (we measure aggregate;
  thinking §7.2).
- AutoTest Phase-4 dedup against the repair loop's targeted-set beyond the
  in-loop `targeted` set (a Phase-4-side read of the persisted targeted-set is a
  later coordination polish).
- Node/Python true line coverage (still function-level; design 2026-07-17 non-goal).
- Redefining the gate to measure runtime/service coverage (Option II, rejected).

## 10. Testing Strategy (TDD — failing test first; negative verification per case)

1. **`HintCoverage` round-trip** (`assembly_redispatch_test.go`): `"coverage"` →
   `HintCoverage`; missing → `HintNone`. Negative: judge prompt unchanged (a test
   asserting the schema has no `coverage` LLM-emit path is not needed — the
   decision is "not taught," verified by the prompt not mentioning it).
2. **`eligibleCoverageGaps`** (new unit test, pure via injected provider):
   - `Reached==true` → no gaps. `Contract==nil` → no gaps.
   - gaps ranked by priority; `targeted` gaps dropped; empty-`File` dropped.
   - Negative: disable the `!Reached` gate → gaps leak (RED).
3. **`AutoTest.RepairGaps`** (new, mock writer + stub gate):
   - happy path writes one test per gap, keeps on gain, reverts on no-gain.
   - `destructive_risk` deny → gap `skipped`, no file written, no abort.
   - Negative: disable keep/revert → a no-gain test is kept (RED).
4. **Coverage no-progress** (`session/repair_loop_test.go`, new): two rounds —
   round 1 writes tests (coverage rises); round 2 mock returns delta 0 → loop
   stops. Negative: remove the delta≤0 break → loop spins to round cap (RED, but
   bounded).
5. **Recovered outcome** (extend `consolidate_recovered_test.go`): dispatch raises
   `RepairedCoverage.Pct` ≥ threshold → `CoverageRecovered=true`; **and**
   `Assessment.Reached` stays `false` (D1 invariant). Negative: assert the Agent
   verdict is not flipped when recovery succeeds (RED if invariant violated).
6. **D1-invariant guard** (the core negative): after a successful coverage repair,
   `sess.Assessment.CoveragePct` equals the Phase-3 Agent-only value (unchanged),
   while `sess.RepairedCoverage.Pct` is higher. Disable the separate-track split
   (overwrite Assessment) → the Agent verdict changes (RED).
7. **Integration** (extend `reflexion_integration_test.go` pattern): one full
   round — `Reached=false` → AutoTest writes a test → re-measure → recovered; plus
   a no-progress stop. Mock provider/generator/writer; no real toolchain.

## 11. File Inventory (plan will finalize)

New:
- `internal/autotest/autotest_repair.go` (+ test) — `AutoTest.RepairGaps`.

Modified:
- `internal/head/agent/types.go` — `HintCoverage`.
- `internal/head/examiner/assembly.go` (+ test) — accept `coverage` in
  `parseRedispatchHint`.
- `internal/session/run_phases_repair.go` (+ test) — coverage eligibility,
  dispatch, re-measure, no-progress, explicit gate/budget; `buildAutoTest`.
- `internal/session/lifecycle_types.go` — `Session.RepairedCoverage`,
  `CoverageRecovered`.
- `internal/session/summary.go`, `run_phases_consolidate.go` (+ tests) — coverage
  recovered wiring (report badge; Agent verdict unchanged).
- `internal/session/lifecycle_run.go` — no phase-order change (dispatch is inside
  Phase 3.1).
- Session persistence (store/JSON) — persist `RepairedCoverage`/`CoverageRecovered`
  for resume idempotency.
- `internal/config/tot.go` — explicit `ResolveReplanBudget` (or reuse driver
  budget) for the round gate.
