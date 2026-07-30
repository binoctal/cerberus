# Coverage-Gap Repair Loop — Design Spec

> Feature: D1 (route coverage gaps into the in-session repair loop). Status: design
> spec, **rev 2 (post-review), 2026-07-30**. Depends on the Examiner→Scout repair
> loop (#3, shipped) and the coverage contract + real-line-coverage subsystem
> (shipped). Strategic basis: `2026-07-30-examiner-replan-autotest-coverage-thinking.md`
> (§6 decision record, §6.4 blocker finding).
>
> **Rev 2 fixes 10 issues from the adversarial review**, marked [Rn] below. Rev 1
> kept the strategic layer; the operational layer was underspecified.

## 1. Problem

The coverage contract gate (`Examiner.AssessCoverage`, `assess.go:18`) can force
`Assessment.Reached=false` when measured coverage is below
`Contract.CoverageGate.LineThreshold`. That verdict is **terminal today**: the
Examiner→Scout repair loop (`executeRepairLoop`, `run_phases_repair.go:24`) keys
eligibility only on per-case `Status==Fail && RedispatchHint!=HintNone`
(`eligibleFailures:99`) and never reads `Assessment`. So a session can finish
"coverage not reached" with no in-session recovery, even though the correction —
write more tests for uncovered code — is exactly what the pipeline can do.

### 1.1 The blocker that shapes this design (thinking §6.4)

`coverageForSession` (`coverage.go:51`) measures **source-tree unit-test
coverage** over `ProjectDir` (`go test -coverprofile` / jest / pytest). Only
unit-test **files written into ProjectDir** raise it. Repair-loop products are
`TestCase`s (http/ws/process_exec) that exercise a running target — they enter
neither ProjectDir nor its unit-test suite, so they move the gate by **zero**.

Therefore coverage recovery cannot reuse the Scout→Agent→TestCase path. It must
**write unit-test files**, which is AutoTest's generator domain. D1 dispatches
AutoTest for coverage inputs; Scout→Agent stays the mechanism for fail-hints.

## 2. Goal

Close the coverage loop in-session: when the Agent-only gate yields a **coverage
gap** (known measurement below threshold), the repair loop drives AutoTest's
generator to write unit tests, re-measures, and repeats until the threshold is met
or a bound fires. Meeting the threshold is recorded as a separate
`RepairedCoverage` measurement and a `CoverageRecovered` flag (observability —
§6.6); the original Agent `Reached=false` verdict is unchanged (D1 invariant).

## 3. Non-goals

- Do **not** flip the Agent-only gate verdict based on AutoTest files. Recovery is
  a separate measurement, not a retroactive Agent pass.
- Do **not** merge AutoTest's standalone Phase-4 pass into the loop. D1 dispatches
  AutoTest as a callable; Phase 4 keeps running afterward (with a D1-required
  exclusion — §6.7). Full unification (Option b) stays deferred.
- Do **not** redefine the gate's measurement semantics (still ProjectDir unit
  coverage). The gate's `Module` field is **not enforced** today (whole-project
  measurement — §9, pre-existing); D1 inherits this and does not fix it.
- Do **not** teach the per-case LLM judge to emit `coverage`. `coverage` is
  **session-synthesized** (§4, §5.1).
- Do **not** make coverage-repair resumable in v1 (§8). Resume re-measures but does
  not re-run the loop.

## 4. Decisions (pinned, rev 2)

| Decision | Choice |
|---|---|
| Loop owner | `executeRepairLoop` becomes a **two-strategy loop**: fail-hints → Scout+Agent (unchanged); coverage-hints → AutoTest dispatch (new). |
| Coverage trigger **[R1]** | Presence of a `Gap{Kind:"coverage"}` in `sess.Assessment.Gaps` — i.e. a **known** measurement below threshold. `!Known` (provider failure) and scope/pathtype gaps do **not** trigger. |
| Coverage mechanism **[R2]** | `AutoTest.RepairGaps(ctx, dir, before, gaps)` calls **`processGap` directly** per gap (bypasses `Run`/`executeSerial`/`executeParallel`), taking the shared `before` report as a parameter. |
| Measurement budget **[R3]** | **One** `RunCoverage` for `before` per round, shared by eligibility + dispatch; the keep/revert verify inside `processGap` is the remaining per-gap cost (bounded by a dispatch `MaxGaps` cap, default 3). Round cost ≈ `MaxGaps + 2` provider runs. |
| Hint category | Add `agent.HintCoverage`. **Session-synthesized only**; NOT added to `parseRedispatchHint`'s accepted LLM set (the LLM never emits it; persisted verdicts round-trip via JSON, not via the parser). |
| Region unit **[R5]** | Reuse `autotest.CoverageGap{File, Func}`. **`Func` is overloaded**: a `file:line` anchor (`foo.go:L42`) on Go's zero-cover path (`coverage_go.go:103`), a real function name on `NoTestFileGaps` (`:133`). The targeted-set key is the raw `(File, Func)` tuple exactly as emitted. |
| Gap ranking **[R8]** | **v1: by estimated gain only** (Go: zero-cover block count in `gap.File`, descending; Node/Python: uniform). Deterministic. Contract-priority ranking is **deferred** — it needs an undefined `moduleOf` mapping and bucket weights (§9). |
| DryRun **[R4]** | When `SafetyMode==DryRun`, the coverage axis is **skipped entirely** (AutoTest writes nothing → no progress possible). |
| Progress measure | Aggregate line-pct delta on a **separate `RepairedCoverage` track** (Agent+AutoTest), NOT per-target, NOT overwriting `Assessment`. |
| Recovered outcome **[R10]** | `RepairedCoverage.Pct >= LineThreshold` ⇒ `CoverageRecovered=true`. This is **observability-only** — it does NOT change the session verdict or any case verdict; the Agent `Reached=false` stays. It is a report annotation. |
| Termination | Round cap (default 2, shared) + fail no-progress (`computeStuck`) + **coverage no-progress** (delta ≤ 0) + escalation gate + explicit budget backstop. |
| Phase-4 safety **[R6]** | Phase 4 must (a) exclude gaps the repair loop already targeted, and (b) not abort on `!before.Pass` from pre-existing/repair-written test failures. Required in D1, not deferred. |

## 5. Data Model

### 5.1 `agent.HintCoverage` (new enum value)

`internal/head/agent/types.go`:
```go
HintCoverage RedispatchHint = "coverage" // session-synthesized: coverage gate not reached
```
**[R-fix]** The judge tool schema, judge prompt, and `parseRedispatchHint`
(`examiner/assembly.go:41`) are **unchanged**. `coverage` is never LLM-emitted and
never parsed from LLM output; it is assigned by the session layer when building
coverage eligibility. Persisted `FinalVerdict`s carry it via JSON struct tags.

### 5.2 Coverage eligibility — reuses `CoverageGap`, not `RepairInput`

Fail-repair and coverage-repair have **different executors** (Scout+Agent vs
AutoTest), so they do not share `RepairInput` (the Scout contract). The coverage
path carries `[]autotest.CoverageGap` directly to the dispatch. No change to
`scout.RepairInput`.

### 5.3 `Session.RepairedCoverage` / `CoverageRecovered` (new fields)

`internal/session/lifecycle_types.go`:
```go
// RepairedCoverage is the post-AutoTest-dispatch coverage (Agent + AutoTest
// tests), measured inside the coverage repair loop. Distinct from Assessment
// (Agent-only gate) so the Agent verdict is never overwritten by AutoTest files.
RepairedCoverage *contract.CoverageMeasurement
// CoverageRecovered is observability-only: RepairedCoverage met the threshold.
// It does NOT flip the Agent Assessment or any case verdict.
CoverageRecovered bool
```

## 6. Components

### 6.1 Coverage eligibility (`run_phases_repair.go`, new)

```go
func (rp *runPhase) coverageEligibility(targeted map[coverKey]bool, before *autotest.CoverageReport) []autotest.CoverageGap
```
- Returns `nil` unless `sess.Contract != nil` AND `sess.Assessment` contains a
  `Gap{Kind:"coverage"}` **[R1]**. (`!Known`/scope/pathtype-only `Reached=false`
  does not qualify.)
- **Reuses the round's shared `before` report** (§6.3) — does NOT run the provider
  itself **[R3]**. Derives gaps via `provider.Gaps(before)` + Go
  `NoTestFileGaps(ProjectDir)` (mirrors `autotest_run.go:21-24`).
- Drops gaps already in `targeted` (dedup) and gaps with empty `File`.
- Ranks by estimated gain (Go: zero-cover block count in `gap.File` from `before`;
  Node/Python: uniform) **[R8]**; returns top-N bounded by the dispatch cap
  (default 3).

`coverKey = struct{ File, Func string }` — `Func` is the raw emitted value (anchor
or name) **[R5]**; `Func==""` ⇒ whole-file target.

### 6.2 AutoTest dispatch (`internal/autotest/`, new public entry)

```go
// RepairGaps generates + writes + verifies unit tests for the given explicit
// gaps, calling processGap DIRECTLY with the caller-supplied before report. It
// bypasses Run/executeSerial/executeParallel (the caller already measured before
// and selected gaps). Safety rails (revert on no-gain, destructive_risk
// escalation at autotest_gap.go:43) are inherited from processGap unchanged.
func (a *AutoTest) RepairGaps(ctx context.Context, projectDir string, before *CoverageReport, gaps []CoverageGap) *AutoTestReport
```
**[R2]** Implementation: allocate `rep`, then `for _, g := range gaps { rep.Items =
append(rep.Items, a.processGap(ctx, g, projectDir, before, rep)) }`. Calling
`processGap` directly (not `executeSerial`) is deliberate: `executeSerial`
(`autotest_serial.go:11`) is a separate duplicated implementation and is the
*default* path (`MaxConcurrency:1`), while `processGap` is invoked only from
`executeParallel`. `RepairGaps` canonicalizes on `processGap` so the safety rails
the spec relies on are actually the ones in effect. Returns a report (no error —
per-gap failures become item statuses, as in `processGap`).

### 6.3 Repair-loop integration (`executeRepairLoop`) — one shared measurement/round

Per round, after the existing fail-repair block **[R3]**:
```go
if rp.hasCoverageGap() {                      // [R1] Kind=="coverage" gap present
    if rp.buildAutoTest().mode == autotest.SafetyDryRun {
        // [R4] DryRun writes nothing → coverage axis cannot progress; skip.
        goto skipCoverage
    }
    before := rp.session.lineCoverageReport(rp.ctx)   // ONE provider run (shared)
    gaps := rp.coverageEligibility(targeted, before)
    if len(gaps) > 0 {
        prev := rp.coverageBaseline()                  // Assessment.CoveragePct r1, else last RepairedCoverage
        rep := rp.buildAutoTest().RepairGaps(rp.ctx, rp.session.ProjectDir, before, gaps)
        after := rp.session.lineCoverage(rp.ctx)       // re-measure (Agent + AutoTest now on disk)
        rp.session.RepairedCoverage = &after
        addAll(targeted, gaps)
        rp.session.CoverageRecovered = after.Known && after.Pct >= threshold
        if !after.Known || after.Pct-prev <= 0 { break }   // coverage no-progress
    }
}
skipCoverage:
```
Provider runs this round: `1 (before) + len(gaps) (processGap verify) + 1 (after)`
≈ `MaxGaps + 2`. `lineCoverageReport` is a new helper returning the raw
`*CoverageReport` (today `lineCoverage` discards it to a `CoverageMeasurement` —
extend to optionally return the report so eligibility reuses it).

Round continuation requires `len(failEligible)>0 || (hasCoverageGap && !recovered
&& progress)`. Both axes share the round cap, escalation gate (§6.4), and budget.

### 6.4 Escalation gate + budget backstop (absorbed #2)

Before each round, consult the session's `escalation.Gate` (the interface AutoTest
uses via `EscalationGateAdapter`, `gate_adapter.go:13`): a
`DecisionAbort`/`DecisionSkipCase` stops the loop. Add an explicit token-budget
check (`config.ResolveReplanBudget` or the existing driver budget) as a
first-class round gate, replacing today's implicit "DecideWithTools error path"
(`run_phases_repair.go:46`).

### 6.5 buildAutoTest (`run_phases_repair.go`, new shared helper)

Constructs an `*autotest.AutoTest` from session config (provider via
`NewCoverageProviderForLanguage`, generator per lang, the session's escalation
gate, the writer, `SafetyMode` from Settings). Reused by the repair loop only.

### 6.6 Recovered wiring — observability only **[R10]**

`CoverageRecovered` is a **report annotation**, not a verdict change. The
summary/report renders "Agent coverage X% (not reached) → repaired to Y%
(recovered)"; the Agent's `Assessment.Reached=false` and every case verdict are
unchanged. There is no consolidate-level consumer that flips a session outcome —
this is explicit (fail-repair's recovered machinery is per-case-verdict and does
not extend to session-level coverage). If a future version wants recovery to
affect the terminal verdict, that is a separate decision with its own consumer.

### 6.7 Phase-4 interaction **[R6]** (required in D1)

Phase 4 (`executeAutoTestPhase`, `lifecycle_run.go:65`) runs after the loop and
calls `at.Run` (`run_phases_autotest.go:67`), which re-discovers ALL gaps and
aborts on `!before.Pass` (`autotest_run.go:18-20`). Two D1 requirements:
1. **Exclude targeted gaps:** Phase 4's gap selection must skip gaps in the
   repair loop's targeted-set (persisted on the session for this purpose). Without
   this, Phase 4 FIFO-reselects gaps the loop already covered/retried.
2. **Tolerate pre-existing failures:** a repair-loop-written test that fails to
   compile must not abort all Phase-4 polish. `at.Run`'s `!before.Pass` early
   return must degrade (warn + skip generation) rather than `return err`.

(Implementing #2 may touch `AutoTest.Run` itself; scope it as a small robustness
fix in D1 since D1 is what introduces loop-written tests that can fail.)

## 7. Control Flow

```
Run():
  Scout → Agent → Examiner (Phase 3)
    assessCoverageIfContract → sess.Assessment (Agent-only gate)   [unchanged]
  executeRepairLoop (Phase 3.1):
    for round in 1..maxRounds:
      gate/budget check                                            [new explicit]
      failEligible = eligibleFailures(stuck)                       [unchanged]
      hasCov = Assessment has Kind=="coverage" gap                 [new, R1]
      if both axes empty: break
      if failEligible:  Scout.RepairPlan → ExecutePlan → Examine   [unchanged]
      if hasCov && mode!=DryRun:                                   [R4]
         before = lineCoverageReport() (shared)                    [R3]
         gaps = coverageEligibility(targeted, before)
         RepairGaps(before, gaps) → lineCoverage → RepairedCoverage [R2]
         recovered? progress? (no-progress → break)
  Consolidate (Phase 3.5)                                          [unchanged]
  AutoTest (Phase 4) — excludes targeted gaps, tolerates !Pass     [R6]
  buildSummary — renders CoverageRecovered annotation              [R10]
```

## 8. Error Handling & Invariants

- **D1 invariant (load-bearing):** `sess.Assessment` is set once at Phase 3
  (Agent-only) and never overwritten by the repair loop. `RepairedCoverage` is a
  separate field. The Agent's coverage verdict is honest; recovery is additive and
  observability-only.
- `RepairGaps` per-gap failures become item statuses (`failed`/`reverted`/
  `skipped`); they never abort the loop or the run.
- Provider failure (`!Known`) → no coverage gap is synthesized → coverage axis
  does not trigger (§4 trigger) **[R1]**.
- Escalation `destructive_risk` deny inside `processGap` → gap `skipped`, not a
  loop abort.
- Budget exhaustion → explicit gate stops the loop before the next round.
- **Resume (v1): non-resumable for coverage repair.** `executeRepairLoop` runs
  only on `Session.Run` (`lifecycle_run.go:55`); `resume_phases_run.go` calls
  `assessCoverageIfContract` only — it does NOT re-run the loop **[R7]**. On
  resume, `RepairedCoverage`/`CoverageRecovered` are **persisted for reporting**
  (read back from the session row); the targeted-set is NOT persisted because it
  is not needed (re-measurement naturally drops now-covered gaps). A resumed
  session does not advance coverage further; if that is needed, wiring the loop
  into resume is a later item (§9).

## 9. Out of Scope

- Option (b) full unification (absorb AutoTest Phase 4 into the loop).
- Per-target coverage progress measurement (aggregate only; thinking §7.2).
- **Contract-priority gap ranking** **[R8]** — needs a defined `moduleOf(gap.File)`
  mapping and numeric bucket weights (`Priorities` is `map[string][]string`,
  bucket→modules, no ordinal). v1 ranks by estimated gain only.
- **Module-scoped gate** **[R9]** — `coverageForSession` measures whole `ProjectDir`;
  the gate's `Module` field is advisory. Pre-existing; D1 documents, does not fix.
- **AutoTest parallel-verify race** — `processGap` write/verify is unsynchronized
  under `MaxConcurrency>1` (pre-existing). D1's `RepairGaps` calls `processGap`
  sequentially over the gap list (no intra-call concurrency), so it does not
  introduce new exposure; documented.
- Node/Python true line coverage (still function-level; design 2026-07-17 non-goal).
- Redefining the gate to measure runtime/service coverage (Option II, rejected).
- Resumable coverage repair (§8).

## 10. Testing Strategy (TDD — failing test first; negative verification per case)

1. **`HintCoverage` enum + parser unchanged** (`assembly_redispatch_test.go`):
   `parseRedispatchHint("coverage")` still returns `HintNone` (NOT accepted — LLM
   never emits it). The enum value exists for session-synthesized verdicts and
   round-trips via JSON. Negative: asserting the parser does NOT accept it.
2. **Coverage trigger** (new unit test) **[R1]**:
   - `Assessment` with a `Kind=="coverage"` gap → eligibility proceeds.
   - `Reached==false` from scope gaps only (no coverage gap) → eligibility returns
     none. `!Known` → none. Negative: trigger on `!Reached` → over-fires (RED).
3. **`AutoTest.RepairGaps`** (new, mock writer + stub gate) **[R2]**:
   - calls `processGap` per gap with the passed-in `before` (assert it does NOT
     call `Run`/`executeSerial`).
   - happy path writes one test per gap, keeps on gain, reverts on no-gain.
   - `destructive_risk` deny → `skipped`, no file, no abort.
   - Negative: route through `executeSerial` instead → behavior diverges (RED).
4. **Provider-call budget** (new) **[R3]**: with N gaps, assert `RunCoverage` is
   called exactly `N+2` times per round (1 before + N verify + 1 after), not
   `1+1+N+1`. Negative: drop the shared-`before` reuse → over-count (RED).
5. **DryRun skip** (new) **[R4]**: `SafetyMode==DryRun` → coverage axis skipped,
   no dispatch, no misleading `RepairedCoverage`. Negative: remove the skip →
   dispatch runs and writes nothing (RED).
6. **coverKey with `file:line` anchors** (new) **[R5]**: Go zero-cover gaps
   (`Func=="foo.go:L42"`) are deduplicated by the raw `(File, Func)` tuple across a
   round. Negative: normalize Func → loses the anchor semantics (RED).
7. **Coverage no-progress** (`session/repair_loop_test.go`, new): round 1 raises
   coverage; round 2 delta 0 → stops. Negative: remove delta≤0 break → spins to
   cap (RED, bounded).
8. **Recovered = observability-only** (extend consolidate/summary tests) **[R10]**:
   dispatch raises `RepairedCoverage ≥ threshold` → `CoverageRecovered=true`, AND
   `Assessment.Reached` stays `false`, AND no case verdict changes. Negative:
   assert flipping a verdict on recovery → RED.
9. **D1-invariant guard** (core negative): after a successful repair,
   `sess.Assessment.CoveragePct` == Phase-3 Agent-only value (unchanged);
   `RepairedCoverage.Pct` higher. Overwrite Assessment → RED.
10. **Phase-4 exclusion + tolerance** (new) **[R6]**: after the loop targets gaps,
    Phase 4's gap set excludes them; and a loop-written test that fails
    compilation does not abort Phase 4 (warn+skip). Negative: no exclusion →
    reselect (RED); `!Pass` aborts → RED.
11. **Integration** (extend `reflexion_integration_test.go` pattern): one full
    round — coverage gap → AutoTest writes a test → re-measure → `CoverageRecovered`;
    plus a no-progress stop. Mock provider/generator/writer; no real toolchain.

## 11. File Inventory (plan will finalize)

New:
- `internal/autotest/autotest_repair.go` (+ test) — `AutoTest.RepairGaps` (direct
  `processGap` loop, shared `before`).

Modified:
- `internal/head/agent/types.go` — `HintCoverage` enum value (parser/prompt unchanged).
- `internal/session/run_phases_repair.go` (+ test) — `hasCoverageGap`,
  `coverageEligibility`, dispatch, shared-`before` re-measure, no-progress, explicit
  gate/budget, DryRun skip; `buildAutoTest`; `lineCoverageReport` helper.
- `internal/session/coverage.go` — `lineCoverageReport` (return raw report for
  reuse); keep `lineCoverage` returning the measurement.
- `internal/session/lifecycle_types.go` — `Session.RepairedCoverage`,
  `CoverageRecovered`.
- `internal/session/summary.go` / report — render the `CoverageRecovered`
  annotation (Agent X% → repaired Y%).
- `internal/session/lifecycle_run.go` — no phase-order change (dispatch is inside
  Phase 3.1).
- `internal/session/run_phases_autotest.go` + `internal/autotest/autotest_run.go`
  — Phase-4 gap exclusion vs the persisted targeted-set **[R6]**; `!before.Pass`
  degrades to warn+skip instead of `return err`.
- Session persistence (store/JSON) — persist `RepairedCoverage`/`CoverageRecovered`
  and the targeted-set (for Phase-4 exclusion) for the run path (not for resume —
  §8).
- `internal/config/tot.go` — explicit `ResolveReplanBudget` for the round gate.
