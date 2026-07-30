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
