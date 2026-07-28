# A1 Runtime WS-Flow Fallback (Phase 2) — Design

> Brainstormed 2026-07-28. Approach chosen by the user: **A — plan-time lazy
> fallback**, with **Recover** verdict semantics.
> Companion: plan `2026-07-28-ws-a1-runtime-fallback-plan.md` (to be written).
> Prior phase: `2026-07-28-ws-a1-unsound-fallback-design.md` (Phase 1, plan-time
> soundness gate — landed in `f6c46d2`).

## Background

A1 Phase 1 gates `covered`-marking on case soundness: an LLM `ws_flow` whose
every `ws_receive` type/alias is grounded (a declared handshake `await_type` or
a goal-named type) marks its connected roles covered; an unsound case does not,
so `WSCasesCovered` still emits the deterministic relay fallback for those
roles. That closes the **plan-time** hole — a broken LLM case can no longer
*suppress* the deterministic fallback for a role it strands.

A residual **runtime** hole remains: a *sound* LLM `ws_flow` (grounded receive)
marks its roles covered, so the deterministic relay case is dropped for them.
But "grounded at plan time" does not imply "the server actually sends it at run
time". A sound case can still fail when executed — the server is silent, auth
is wrong, or the matched frame's shape mismatches. When it fails, the role it
covered has **no** fallback in the plan (Phase 1 dropped it), so the role is
stranded with neither the LLM case nor the deterministic relay case proving it.

The session pipeline (`session/lifecycle_run.go:Run`) is strictly linear —
Scout → Agent → Examiner → consolidate — with **no in-session loop**: examiner
verdicts do not feed back to Scout for re-planning. `reflexionCfg` is
cross-session memory recall, not an in-session retry. So the fallback must be
**self-contained at the Agent execution layer**.

## Problem

A sound LLM `ws_flow` case that fails at execution leaves its covered roles
stranded: the deterministic relay case was suppressed (Phase 1 coexistence),
and the LLM case failed. The role is uncovered through no fault detectable at
plan time.

## Approach: plan-time lazy fallback + Recover

Scout emits, alongside a sound LLM `ws_flow` that covers a relay receiver role,
a **lazy deterministic fallback case** — a copy of the deterministic relay case,
marked bound to that LLM case, that the Agent does **not** execute by default.
The Agent activates it only when its bound primary case fails at execution.
Recover semantics: the primary case keeps its `fail` verdict (preserving the
failure signal); a passing fallback marks the role `recovered`, not `pass`.

This confines the change to Scout (emit) + Agent (activate). Examiner is
untouched — it just judges one more case. No session loop is introduced.

### 1. Data model

`agent.TestCase` gains one optional field that encodes the binding (existing
`DependsOn` is wrong: it means "run me only after my dependencies complete",
not "run me only if my primary failed"):

```go
// FallbackFor is the ID of the primary case this case is a lazy fallback for.
// Empty on normal cases. A non-empty FallbackFor marks the case as lazy: it is
// not executed by default; the Agent activates it only when its primary case
// fails at execution (A1 Phase 2).
FallbackFor string `json:"fallback_for,omitempty"`
```

`agent.StepResult` gains one field so a recovered outcome is distinguishable
from a primary pass in the results the Examiner and reports consume:

```go
// Recovered is true when this result is a lazy fallback case that ran because
// its primary case failed, and the fallback itself passed. The primary case's
// own result remains a fail; this marks the role recovered, not passed.
Recovered bool `json:"recovered,omitempty"`
```

Both fields are `omitempty`; zero values are the struct defaults, so existing
construction and serialization are unaffected.

### 2. Carry the covering case ID via a side table

Phase 1's `covered` is `map[svc]map[role]bool`, produced by `assemblePlan` and
threaded through `runAIPlanning` / `directPlan` / `executeDirectPlanning` /
`augmentPlan` / `appendExecutorCases` / `WSCasesCovered`. It records *whether*
a role is covered. Phase 2 also needs *which sound case* covered it, to bind
the lazy fallback. Retyping that map `bool → string` would touch 7+ function
signatures for no other gain, so Phase 2 introduces a **side table** built
alongside `covered`, carrying only the binding:

- `coveringCase map[svc]map[role]string` — role → ID of the sound LLM case that
  covered it (absent key = uncovered).

`covered` stays `bool`; every upstream producer signature is unchanged. Only
the `augmentPlan → appendExecutorCases → WSCasesCovered` chain gains the side
table as an extra parameter. `WSCasesCovered`'s public behavior is unchanged
for uncovered roles; covered roles now emit a lazy fallback instead of
dropping the deterministic case.

### 3. Plan-time emission (Scout)

In `WSCasesCovered` / `augmentPlan`, for each deterministic relay receiver
role (from `wsRelayCases`, the per-receiver relay case):

- **Uncovered** (`!covered[svc][receiver]`): emit the deterministic relay
  case as a normal case (Phase 1 behavior, unchanged).
- **Covered by a sound LLM case** (`coveringCase[svc][receiver] == <caseID>`):
  emit the deterministic relay case as a **lazy fallback** — same Steps/
  Expectation/Target/Service, with `FallbackFor = <caseID>` and `Priority = -1`
  (deprioritized, so the default execution path skips it until activated).

No deduplication hazard: a receiver is either covered (lazy fallback emitted)
or not (normal case emitted) — never both.

### 4. Agent scheduling (ExecutePlan)

**Execution path:** a lazy fallback is a `ws_flow` case with non-empty `Steps`
(the deterministic relay steps), so `executeStep` takes its Phase 0
deterministic branch (`if len(tc.Steps) > 0 → runSteps`, at
`execute_phases.go:62`) — no Steer LLM, no ReAct loop, no recovery. The
fallback's only LLM cost is the Examiner verdict on its result, and that
verdict is usually objective ("received the relayed signal"), so it takes the
Examiner's deterministic fast path too. This is why the fallback is cheap and
why neither the Agent nor the Examiner needs a new code path for it.

**Pre-scan (shared by serial + parallel):** at entry, build
`fallbacksByPrimary map[string][]*TestCase` from every case whose
`FallbackFor != ""`. These cases are not executed in the main loop and are not
recorded as `StepSkipped` — they are awaiting activation.

**Serial** (`executor_run.go:ExecutePlan`): after a primary case produces its
`result`, if `result.Status == StepFailed && !isEnvironmental(result)`, run
each `fallbacksByPrimary[tc.ID]` entry via `executeStep`, set
`fbResult.Recovered = (fbResult.Status == StepPassed)`, and append. The primary
case's own `fail` result is appended first and unchanged.

**Parallel** (`parallel_execute.go:ExecutePlan`): the main
`for i := range plan.Cases` loop skips `FallbackFor != ""` cases the same way
it skips `isDeprioritized` — registering them into `fallbacksByPrimary` instead
of dispatching them, so they never enter the dependency graph or acquire a
worker slot. In `executeAndStore`, after the primary case fails
(non-environmental), that same worker goroutine runs the primary's fallbacks
synchronously inline via `executeStep` and stores `results[fb.ID]`. A fallback
is bound to exactly one primary and runs only in that primary's worker, so
`results[fb.ID]` is written exactly once — no concurrent write — and
`collectResults` finds it by ID after `wg.Wait()`.

**Escalation interaction:** the fallback result does **not** participate in
`ExecutePlan`'s `consecutiveFailures` systemic-failure counter — the primary
case's `fail` already counted, and the fallback is a remedy, not an independent
failure that should compound toward a systemic abort. Budget checking
(`checkBudgetWarning`) runs at case boundaries in the serial loop; the fallback
executes inline right after its primary with no LLM call, so it is not a
separate budget checkpoint. The plan step pins the exact placement of the
fallback append relative to the primary's escalation check.

**Activation gate (narrowed):**
- `StepFailed` only. `StepSkipped` does not activate (a skipped case did not
  mark its roles covered — Phase 1 already emitted the normal fallback for
  them). `StepPassed`/`StepUncertain` do not activate.
- Exclude **environmental failure** (target unreachable): if the target is
  unreachable, a fallback cannot succeed either. `isEnvironmental(result)`
  reuses the existing `types.IsEnvironmentalFailure` (the same predicate the
  ReAct loop uses at `execute_phases_react_loop.go:51`) and the
  `buildFailedResultForUnreachableTarget` result shape — **no new `StepResult`
  field is needed**. (`environmentalSeen` stays internal to `stepExecution`;
  the signal is already encoded in the failed result the loop returns.)

### 5. Recover verdict semantics

- Primary case result: stays `StepFailed` (failure signal preserved for
  reflexion memory and report accuracy).
- Fallback result: judged transparently by the Examiner as an ordinary case
  (the Examiner code is unchanged); it is appended to `results` with
  `Recovered` set when it passed.
- Report layer can associate a recovered role by `result.TestCase.FallbackFor`
  (non-empty ⇒ this is a fallback) plus the primary's `fail`. Rendering that as
  "recovered" in human-facing output is out of scope here; Phase 2 only lands
  the data.

## Verification

- **Scout (TDD):** `covered` carries the covering case ID; a covered receiver
  yields a lazy fallback (`FallbackFor` bound, `Priority < 0`); an uncovered
  receiver yields a normal relay case; no receiver yields both. `make check`
  EXIT 0 is the hard gate.
- **Agent (TDD, fake executor — no live LLM):** serial `ExecutePlan` activates
  the fallback and sets `Recovered=true` on a non-environmental `StepFailed`;
  does not activate on `StepPassed` / `StepSkipped` / environmental `StepFailed`;
  pre-scan skips lazy cases without recording `StepSkipped`. Parallel path
  mirrors this. `make check` EXIT 0.
- **Live `cerberus run`** against the open-agents relay (the same dogfood
  target) is a *reference* run, not a gate: confirm a sound-but-failing LLM
  `ws_flow` leaves a recovered role rather than a stranded one. Non-deterministic
  (depends on the model emitting a sound case that then fails), so it is
  informational.

## Out of scope (explicit)

- **In-session Scout↔Agent↔Examiner loop** (approach C: examiner-driven
  re-dispatch). Deferred — the largest architectural change; Phase 2's plan-time
  lazy fallback removes the immediate stranding without it.
- **Agent-authored WS synthesis** (approach B: the Agent invents the fallback
  at run time). Rejected — duplicates Scout's deterministic generation and
  blurs the head boundary.
- **Non-WS runtime fallback.** Only WS relay receiver roles (the `wsRelayCases`
  set) get a lazy fallback.
- **Examiner changes.** The Examiner judges the fallback as an ordinary case;
  no new verdict path.
- **Recovered rendering in reports.** Phase 2 lands `Recovered` +
  `FallbackFor`; presenting "recovered" in human-facing output is a follow-up.
- **A1 self-handshake re-await** (a `ws_receive` whose type equals the
  connect step's own already-consumed handshake type). Carried over from
  Phase 1's out-of-scope; narrow, revisit if observed.

## Files

- `internal/head/agent/types.go` — `TestCase.FallbackFor`, `StepResult.Recovered`
- `internal/head/agent/executor_run.go` — serial pre-scan + activation
- `internal/head/agent/parallel_execute.go` (+ helpers) — parallel activation
- `internal/head/scout/ws_cases.go` — lazy fallback emission in `WSCasesCovered`
  (consumes the `coveringCase` side table)
- `internal/head/scout/assembly.go` — build `coveringCase` alongside `covered`
  (sound case ID per covered role)
- `internal/head/agent/executor_run_test.go` (or alongside) — activation tests
- `internal/head/scout/ws_cases_test.go` / `ws_relay_test.go` — emission tests
