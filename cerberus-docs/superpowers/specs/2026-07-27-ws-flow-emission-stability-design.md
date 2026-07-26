# ws_flow Emission Stability — Design

> Brainstormed 2026-07-27. Approach chosen by the user: **defense + guidance**.
> Companion: plan `2026-07-27-ws-flow-emission-stability-plan.md` (to be written).

## Background

The S2 tool-calling migration moved Scout's WebSocket choreography emission
from a single structured `ws_relay` intent to a sequence of tool calls:
`begin_case` (opens a case, `Action:"ws_flow"`) followed by `ws_connect` /
`ws_send` / `ws_receive` / `ws_disconnect` step tools, assembled by
`assemblePlan` into a multi-step `ws_flow` TestCase. This is more flexible
(arbitrary choreography) but relies on the LLM emitting a *sequence* after
`begin_case`.

## Problem

The 2026-07-26 dogfood (run after the `target_validate` ws_flow fix) showed
GLM's emission is **non-deterministic**:

- run-1 emitted a complete 6-step ws_flow (connect web + bridge → ws_send →
  ws_receive session:start → disconnect ×2).
- run-2 (same goal + config, minutes later) emitted an **empty** ws_flow case
  — a `begin_case` with **zero** following `ws_*` calls.

The empty case was created, executed as a no-op, and Examiner-skipped
(correctness 0.9, critique=true) — wasted Agent cycles and a confusing
verdict. See cccmemory `llm-ws-flow-emission-unstable`.

## Root cause

- `begin_case`'s schema requires only `{name, expectation}`; it does **not**
  enforce a following `ws_*` sequence. The LLM can legitimately stop after
  `begin_case`.
- The prompt (`internal/head/scout/prompts.go:75-85`) guides the sequence in
  prose ("emit begin_case followed by the ordered ws_connect/ws_send/
  ws_receive sequence"), but GLM does not always comply.
- `assemblePlan.flush()` (`internal/head/scout/assembly.go:23-38`) appends the
  open case even when `len(Steps)==0`, so an empty `begin_case` becomes an
  empty ws_flow case.
- Essential driver: **LLM non-determinism**. "Make GLM always emit complete"
  is not realistic; the fix is defense (empty case cannot survive) +
  guidance (raise the complete-emission rate).

## Approach: defense + guidance

Two complementary changes, scoped to the empty-ws_flow symptom.

### 1. assembly defense — drop empty ws_flow cases

In `assemblePlan.flush()`, before appending `open` to `cases`:

```go
if open.Action == "ws_flow" && len(open.Steps) == 0 {
    open = nil
    return // drop: a begin_case with no ws_* steps is not a real case
}
```

- **Pure function, no logging.** Observability comes from the unit test and
  the dogfood trace (empty ws_flow cases simply no longer appear).
- **Drop condition is strictly `len(Steps)==0`.** A ws_flow with steps but
  missing `ws_connect` (incomplete sequence) is **not** dropped — that is an
  execution-time failure judged by the Examiner, out of scope here.
- **Unaffected:** deterministic-detector ws_flow cases (always have steps);
  LLM-complete ws_flow; the `covered` map (an empty case has no `ws_connect`
  steps, so the covered-recording loop is a no-op regardless).
- **Test:** `TestAssemblePlan_DropsEmptyWSFlowCase` — a `begin_case` with no
  following `ws_*` call produces no ws_flow case in the result (and does not
  perturb the `covered` map). Contrast with a `begin_case` + `ws_*` sequence
  which still produces a populated ws_flow case.

### 2. prompt guidance — strengthen + example

In `promptPlanSystem` (`internal/head/scout/prompts.go`, the ws_* block around
lines 75-85), add:

- An explicit constraint: emit `begin_case` ONLY when immediately followed by
  a `ws_connect` (per role) + `ws_send`/`ws_receive` sequence. A `begin_case`
  with no following `ws_*` produces no case (the assembly drops it).
- A short, **generic** example of the sequence structure (connect each role →
  send → receive → disconnect), kept generic to avoid over-fitting a specific
  message type (so GLM does not blindly copy `session:start`).

Not unit-tested (prose); behavioral effect checked via the live gate trend.

## Verification

- **assembly: TDD** — failing test → fix → green; `make check` EXIT 0. This is
  the hard gate.
- **live gate `TestScoutRelayEmission_Live`:** rerun 1-2× as a *reference*
  (non-deterministic). Confirms no empty ws_flow appears post-fix and that
  complete emission still happens. Not a hard gate.

## Out of scope (explicit)

- **Schema-enforced single-tool ws_flow** (one tool emits the whole
  choreography). Rejected: would revert S2's `begin_case`+`ws_*` split and
  lose choreography flexibility.
- **Scout retry-until-complete.** Rejected: complex retry semantics + token
  cost + when-to-stop ambiguity.
- **ws_flow step-completeness validation** (e.g. missing `ws_connect`).
  Left to the executor / Examiner.
- **The deterministic `device:online` relay regression** (traced pass,
  verdict fail on 2026-07-26). Separate follow-up; see cccmemory
  `llm-ws-flow-emission-unstable`.

## Files

- `internal/head/scout/assembly.go` — `flush()` drops empty ws_flow
- `internal/head/scout/assembly_test.go` — new test (or alongside existing
  assembly tests)
- `internal/head/scout/prompts.go` — `promptPlanSystem` ws_* block: strengthen
  + generic example
