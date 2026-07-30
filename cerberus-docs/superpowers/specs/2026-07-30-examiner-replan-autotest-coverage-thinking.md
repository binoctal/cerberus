# Examiner Re-planning Depth × AutoTest Coverage — Strategic Thinking

> Type: strategic thinking / direction proposal (NOT an implementation spec).
> Date: 2026-07-30.
> Scope: the junction between the Examiner re-dispatch loop (#3) and the AutoTest /
> coverage-contract subsystem. Both are built; this doc asks what to deepen next.
> Evidence: code survey (file:line cited) + design docs 2026-06-19, 2026-07-17,
> 2026-07-19, 2026-07-29.

## 0. TL;DR

Both subsystems' **base implementations already landed**. The Examiner→Scout repair
loop is fully wired (commits `610a17c`→`e9de9fc`); real line-coverage + contract
persistence landed (`CoverageMeasurement`, `SaveContract`, V010); the AutoTest
RunCoverage injected-runner refactor landed (`coverageRunner`, Default* runners).

The decisive finding is the **empty junction**: the two subsystems share only the
coverage-provider abstraction and have **zero control-flow edges in either
direction**. This produces the single highest-value deepening opportunity:

> **A coverage gap (`Assessment.Reached==false`) is a terminal verdict today —
> nothing acts on it. Yet "the fix is to write more tests," which is exactly what
> Scout+Agent do.** Routing coverage gaps into the repair loop closes the biggest
> open in-session loop.

## 1. Current state (verified, not assumed)

### Examiner re-dispatch loop (#3) — BUILT
- Trigger: `Status==Fail && RedispatchHint!=HintNone` only
  (`run_phases_repair.go:101`).
- Hint vocabulary: `endpoint_drift | auth | shape | none`
  (`internal/head/agent/types.go:29-32`) — **HTTP/endpoint-flavoured**.
- Loop: `executeRepairLoop` (`run_phases_repair.go:24`), default 2 rounds
  (`ResolveReplanMaxRounds`), no-progress guard (`computeStuck:130`), error →
  break (never aborts run).
- Phase slot: Phase 3.1, strictly **after** Examiner, **before** Consolidate and
  AutoTest (`lifecycle_run.go:55`).

### Coverage contract + AutoTest — BUILT
- Scout builds `Contract` (scope/depth/gate) + self-assess; persisted via
  `SaveContract` (V010) so resume can assess (`run_phases_scout.go`).
- Examiner `AssessCoverage` (`assess.go:18`): when `Known && Pct <
  LineThreshold` → `Reached=false` + a `coverage` gap is appended, overriding the
  LLM. Unknown coverage → gate skipped, LLM verdict stands (D4 fix).
- AutoTest gap-driven generation: `Run` (`autotest_run.go:10`) detects gaps →
  `Generate` → write → re-measure → keep iff coverage rose (`autotest_gap.go`).
- Phase slot: Phase 4, **after** Consolidate.

### The junction — EMPTY (the crux)
- `executeRepairLoop` references neither `Coverage` nor `AutoTest` nor
  `Assessment` (grep confirms zero). It keys only on per-case `Fail+hint`.
- `AssessCoverage`'s `coverage` gap and `Reached=false` go **nowhere actionable**
  — they render in the report, but trigger no re-plan.
- AutoTest's `AfterCoveragePct` never feeds the Examiner gate (intentional, D1 —
  the gate measures the Agent's tests alone). AutoTest runs strictly after
  re-dispatch.
- Net: **two parallel, disjoint coverage-improvement loops**:
  - Loop A (Examiner repair): fixes *endpoint failures* → re-judges *case verdicts*.
  - Loop B (AutoTest): fills *function gaps* → re-measures *coverage*.
  - Neither feeds the other; neither recovers a *coverage-gate* miss in-session.

## 2. The deepening directions (ranked)

### ★ D1 — Route coverage gaps into the repair loop (HIGHEST VALUE)
**Problem.** A session can finish `Reached=false` (line coverage below the
contract gate) and the repair loop will not act, because coverage gaps live on
`Assessment`, not on per-case `FinalVerdict`. The obvious correction — "generate
additional tests targeting uncovered code" — is exactly the Agent's job, yet the
gate fires *after* the repair loop's eligibility is computed and the loop is
done.

**Shape.** Introduce a `coverage` hint category (or a parallel trigger) so a
coverage-gate miss enters `executeRepairLoop`:
1. Examiner emits a coverage gap (already does) tagged as repair-eligible when
   `Known && Pct < threshold`.
2. `executeRepairLoop` gains a coverage-driven eligibility path: from
   `Assessment.Gaps` (kind `coverage`), derive synthetic `RepairInput`s whose
   "failed case" is the *uncovered region* (file/function from the coverage
   profile), hint `coverage`.
3. Scout `RepairPlan` emits targeted cases for the uncovered code; Agent runs
   them; **coverage is re-measured** (the re-judge step must call `AssessCoverage`
   again, not just per-case `Examine`); loop re-judges `Reached`.
4. Termination reuses the triple bound; no-progress = coverage delta ≤ 0 across a
   round.

**Why highest value.** This is the only direction that closes a *terminal
verdict* into an actionable loop — it converts "not reached" from a report
annotation into a repair target. It also gives the repair loop a non-endpoint
domain (the current hint set is endpoint-only by design §8).

**Cost / risk.** Re-judging coverage inside the loop means a 2nd
`coverageForSession` measurement per round (provider re-run). The repair loop
currently re-judges only per-case verdicts (`buildExaminer().Examine`); adding an
`AssessCoverage` re-call is a new edge. Must guard: coverage repair must not
double-count with AutoTest's own gap-filling (see D3 — these want to unify).

**Open design question (needs user input).** When both AutoTest (Phase 4) and a
coverage repair loop can generate tests for uncovered code, which owns it? Two
options: (a) the repair loop owns coverage *verdict recovery* and AutoTest owns
*opportunistic polish* afterward; (b) unify them (D3). This is the real fork.

### D2 — WS-aware hint categories (MEDIUM, sprint-aligned)
**Problem.** The hint vocabulary (`drift | auth | shape`) predates the entire WS
sprint. WS failures (handshake await mismatch, framing/protocol mismatch,
decisive-receive no-match, match-all item violation) get shoehorned into `shape`
or emit `none` and short-circuit — so the repair loop rarely engages on the
subsystem most recently built.

**Shape.** Add `handshake` / `ws_shape` (or a `ws_*` family) categories; teach
the judge prompt the WS failure modes; map them to Scout repair actions (adjust
await_type, fix message envelope, fix match criteria/type). The repair prompt
must know the failed step was a WS step (it has `TestCase.Steps`).

**Cost / risk.** Low mechanical risk (enum + prompt + parse), but real LLM-tuning
risk: WS failures are subtle and a mis-categorized hint sends Scout the wrong
correction signal. Needs negative verification that WS failures don't collapse to
`none`.

### D3 — Unify AutoTest gap-generation with the repair loop (HIGHER VALUE, HIGHER COST)
**Problem.** Loop A (Examiner repair) and Loop B (AutoTest) both "generate tests
to cover more," on disjoint state, in sequence, with no shared eligibility or
feedback. AutoTest cannot benefit from Examiner verdicts ("this target mattered")
and the Examiner cannot benefit from AutoTest's coverage delta.

**Shape.** Make AutoTest's gap-driven generation one *strategy* the repair loop
can dispatch for a `coverage` hint, instead of a separate Phase-4 pass. One loop,
one eligibility, one re-judge (verdicts + coverage together), one termination.

**Cost / risk.** This is the largest change — it restructures phase ordering and
merges two subsystems' control flow. D1 is a prerequisite (coverage must be in
the loop first). Recommend D1 first, D3 as the follow-on once D1's shape proves
out.

## 3. Smaller gaps (low-cost, independent)

- **Escalation gate in the repair loop.** Design §5.5 lists "gate says stop" as a
  budget backstop; not wired (`executeRepairLoop` ignores `escalation.Gate`).
  AutoTest already uses `EscalationGateAdapter`; the repair loop should too — a
  runaway repair round is currently bounded only by the round cap + implicit
  budget error.
- **Explicit token-budget backstop.** Today it's just the `DecideWithTools` error
  path (`run_phases_repair.go:46`); no `TokenBudget` API. Cheap to make
  first-class and lets D1's extra measurement rounds be budget-gated.
- **AutoTest does not run on resume** (`resume_phases_run.go` has no
  `executeAutoTestPhase`). Resume does `AssessCoverage`, so a resumed session can
  *judge* coverage but cannot *improve* it. Relevant only if D1/D3 make coverage
  repair resumable.
- **MaxGaps FIFO ranking** (`autotest_run.go:28`) and **per-test verify in
  parallel mode** (`autotest_gap.go:57`) — flagged "future" optimizations.

## 4. Recommendation

1. **D1 first** — route coverage gaps into the repair loop. Highest value (closes
   a terminal verdict), finite scope, and it forces the D1-vs-D3 design fork
   (coverage repair vs AutoTest ownership) to be decided with a working prototype
   rather than in the abstract.
2. Pair D1 with the **escalation gate + explicit budget backstop** (§3) so the
   extra measurement rounds are bounded.
3. **D3 second** — unify AutoTest into the loop, once D1's coverage-repair shape
   is validated.
4. **D2 independent / parallel** — WS hints are orthogonal to the coverage loop
   and sprint-aligned; can proceed anytime.

## 5. What needs a human decision before any code

- **The D1/D3 fork:** should coverage recovery be (a) owned by the Examiner
  repair loop with AutoTest as a later polish pass, or (b) a unified single loop?
  This changes phase ordering and is not safely auto-decided.
- **Coverage hint's "uncovered region" unit:** file? function? Go has line+func
  data; Node/Python are function-level only. The repair target granularity depends
  on it.
- Whether to touch phase ordering at all in this pass (D1 can be done *without*
  reordering if coverage is re-measured inside the existing Phase-3.1 loop).

Until these are answered, D1 is the recommended concrete next step and the others
stay parked.

## 6. Decision Record (2026-07-30)

### 6.1 The D1/D3 fork — DECIDED: Option (a) + selective absorption

Coverage recovery is **owned by the Examiner repair loop** (Option a); AutoTest
stays a Phase-4 polish pass. Full unification (Option b) is deferred as YAGNI
until duplication is demonstrated in practice. Rationale: the two mechanisms
produce different artifacts with different safety profiles (Scout repair = durable
semantic cases judged via executor; AutoTest = generated files, revert-on-no-gain,
escalation-gated `destructive_risk`). Merging entangles the verdict loop with file
mutation and breaks the "repair never aborts the run" invariant.

**Absorb from (b) into (a)** — take the value, avoid the cost:

| # | Absorbed practice | Cost | Verdict |
|---|---|---|---|
| 1 | Contract-prioritized gap ranking (replace FIFO `MaxGaps`) | pure fn | **in D1** |
| 2 | Escalation gate + explicit token-budget backstop in repair loop | small | **in D1** |
| 3 | Dedup coordination edge (covered-set shared repair↔AutoTest) | small | **in D1** |
| 4 | AutoTest coverage delta folded back into `Assessment` (final `AssessCoverage`) | 1 call | **in D1** |
| 5 | Repair loop *dispatches* AutoTest as a tool for a mechanical gap | medium | **Phase 2, gated** |

Item 5 is deferred because it conflicts with the D1 invariant "the gate measures
the Agent's tests only" (design 2026-07-17 D1): an in-loop AutoTest dispatch would
pollute the Agent-gate unless measured on a separate track. Open it only if
mechanical-gap coverage proves necessary after D1 ships, and then keep its
measurement off the Agent-gate.

**Not absorbed** (this IS the cost of b): merging AutoTest's file-mutation state
into verdict-loop ownership; strategy-selection as a first-class loop concern.

### 6.2 D1 concrete scope

1. Coverage gap → repair-loop eligibility: `Assessment.Gaps` (kind `coverage`)
   derives synthetic `RepairInput`s; re-judge calls `AssessCoverage` (Agent-only
   measurement, D1 invariant held).
2. AutoTest gap ranking → contract-priority-weighted.
3. Repair loop wired to `escalation.Gate` + explicit `TokenBudget` backstop.
4. Covered-set dedup + AutoTest delta folded into final `Assessment`.

### 6.3 "Uncovered region" granularity — DECIDED: reuse `CoverageGap{File, Func}`

(See §7 for the full reasoning.) The existing `autotest.CoverageGap{File, Func
string, Reason string}` already carries both granularities and is uniform across
languages, dissolving the file-vs-function dichotomy. Target = finest the provider
gives (Func when populated, File-only when empty); covered-set key = `(File, Func)`;
**progress measured at the aggregate gate (line-pct delta via `AssessCoverage`),
not per-target** — this keeps the Go-line-vs-Node/Python-function asymmetry out of
termination correctness.

## 7. Coverage hint "uncovered region" granularity: file vs function

The data model dissolves the binary. `autotest.CoverageGap` (types.go:57) is:

```go
type CoverageGap struct { File, Func string; Reason string }
```

— shared by Go / Node / Python providers, carrying BOTH file and function. Go's
`Gaps()` emits `File+Func` for uncovered functions and `File`-only for the
no-test-file case (coverage_go.go:103/133/136). So "file or function" is not a
language split to design around — it is a field that is sometimes populated.

### 7.1 Target granularity (what becomes a RepairInput / covered-set key)

**Recommendation: reuse `CoverageGap` as the repair target type.** Target = the
finest granularity the provider reports.

- **Function-level target** (when `Func` populated — Go uncovered-func, Node/Python
  func gaps):
  - **+** Precise behavioral target → focused, judgeable test; crisp no-progress
    attribution; **zero new concept** — reuses the existing AutoTest gap model.
  - **+** Language-uniform at the type level (the language difference lives only in
    the measurement unit, already handled by `CoverageMeasurement.Unit`).
  - **−** Node/Python function identity can be noisy (anonymous/minified methods) →
    unstable target. **Mitigation:** degrade to file-level when `Func` is empty or
    untrusted.
  - **−** More gaps than rounds allow → may not cover enough within the round cap.
    **Mitigation:** rank by contract-priority × estimated coverage gain.
- **File-level target** (when `Func` empty — no-test-file case, or noisy func):
  - **+** Stable, durable, unambiguous; resume/dedup-simple; matches how test files
    are actually produced.
  - **−** Coarse: Scout may re-cover covered code or emit a sprawling test; weaker
    behavioral signal → more LLM inference; harder to judge.

The `Func` field being sometimes-empty already implements the right degradation —
do not force a single granularity.

### 7.2 Measurement granularity (the orthogonal concern — where the asymmetry bites)

The contract gate is a **line** threshold (Go). Progress MUST be measured as the
aggregate line-pct delta (via `AssessCoverage`), **regardless of whether the target
was a file or a function.** Do NOT measure progress as "did this target flip to
covered":

- Per-target coverage status optimizes function count, not the gate's line metric.
- It is exactly where Go-line-vs-Node/Python-function asymmetry would corrupt
  termination: a Node/Python "covered function" is a coarser unit than a Go
  covered-line-block, so per-target progress would mean different things per
  language.
- Measuring at the aggregate gate sidesteps the asymmetry entirely — the loop
  stops when `Pct >= LineThreshold` (or no delta), uniformly.

### 7.3 Ranking when gaps exceed the round budget

Rank candidate gaps by `contract.Priorities[module]` × estimated coverage gain. Go
can estimate gain from the uncovered `numStmts` in the profile; Node/Python
approximate as `1/TotalFuncs`. This is the absorbed practice #1 in §6.2.
