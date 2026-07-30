# WS-Aware Repair Loop — Implementation Plan (D2)

> Plan date: 2026-07-31. Implements the D2 design spec
> (`cerberus-docs/superpowers/specs/2026-07-31-ws-aware-repair-design.md`).
> Each task is TDD with a negative-verification test. `make check` EXIT 0 +
> clean tree after every task. Commit author `binoctal <binoctal@gmail.com>`,
> no Co-Authored-By, English commit messages.

## Sequencing

```
T1 (enum+parser) → T2 (judge schema) → T3 (judge prompt)
T4 (repair tool steps) → T5 (WS repair emission) → T6 (repair prompt)
T7 (eligibility) → T8 (integration) → T9 (negatives) → T10 (docs)
```
T1–T3 are the judge half (diagnosis); T4–T6 the Scout half (emission); T7–T9
verify the loop; T10 documents.

## T1 — WS hint enum + parser
**Files:** `internal/head/agent/types.go`; `internal/head/examiner/assembly.go`;
test in `assembly_redispatch_test.go`.
**Change:** add `HintHandshake/HintWsShape/HintWsMatch`; `parseRedispatchHint`
accepts them.
**Tests:** the 3 WS strings parse to their constants; `"handshake"` etc. accepted.
**Negative:** `"handshoke"` (typo) → `HintNone` (RED if wrongly accepted).

## T2 — Judge tool schema WS hints
**Files:** `internal/head/examiner/tools.go`; test.
**Change:** add the 3 WS values to the `redispatch_hint` enum.
**Tests:** schema enum contains all 7 values.
**Negative:** drop one → assertion fails (RED).

## T3 — Judge prompt WS failure modes
**Files:** `internal/head/examiner/prompts.go`; test.
**Change:** extend the fail-cause bullet with handshake/ws_shape/ws_match and
the step field each implicates.
**Tests:** prompt contains "handshake", "ws_shape", "ws_match".
**Negative:** remove a mode → assertion fails (RED).

## T4 — repair_case `steps` field
**Files:** `internal/head/scout/repair_plan.go` (`repairTools`); test.
**Change:** add optional `steps` array (items mirror TestStep fields) to
`repair_case`; required set is just `["replaces"]`.
**Tests:** `repair_case` schema has a `steps` array property; `replaces` required.
**Negative:** no `steps` property → assertion fails (RED).

## T5 — assembleRepair builds WS TestCase
**Files:** `internal/head/scout/repair_plan.go` (`repairCaseFromCall`,
`assembleRepair`); test.
**Change:** when a `repair_case` call has `steps`, build a TestCase with `Steps`
(parsed from the call) + `Action="ws_flow"` + `Target` from the first step url or
the original; HTTP fields ignored. No `steps` → HTTP path unchanged.
**Tests:** WS call → TestCase has Steps with action/type/asserts; HTTP call →
Target/Method/Body unchanged.
**Negative:** WS call dropping steps → no Steps (RED).

## T6 — Repair prompt WS guidance
**Files:** `internal/head/scout/repair_plan.go` (`buildRepairPrompt`,
`promptRepairSystem`); test.
**Change:** when a failure's case has Steps, include them and instruct WS repair
per hint (handshake→await type; ws_shape→send message; ws_match→receive
type/asserts/match_all); emit `steps` not path/method.
**Tests:** prompt built for a WS failure contains the failed Steps + "steps".
**Negative:** WS failure omitted from prompt → assertion fails (RED).

## T7 — WS case repair eligibility
**Files:** test in `internal/session/`.
**Change:** none (verify). `eligibleFailures` already passes WS cases.
**Tests:** a Fail verdict on a WS case (Steps) with a WS hint → eligible
RepairInput with the hint + case.
**Negative:** hint==none → not eligible (RED).

## T8 — WS repair integration
**Files:** test in `internal/head/scout/`.
**Change:** none (verify). Drive `RepairPlan` via `repairPlanFn`-style or
`assembleRepair` with a WS failure + ws_match hint; assert the replacement is a
WS TestCase with corrected Steps + Replaces.
**Tests:** end-to-end WS repair shape.
**Negative:** replacement lacks Steps → assertion fails (RED).

## T9 — Negative verification
**Files:** tests.
**Change:** none. HTTP repair unchanged; non-WS hints never produce steps;
unrecognized WS failure → none.

## T10 — Docs
**Files:** `cerberus-docs/executors/websocket.md`,
`cerberus-docs/failure-reason-classification.md`.
**Change:** document the WS repair hints + what each repairs.

## Cross-cutting
- **Four-surface checklist:** this feature touches all four (types, schema,
  prompt, parser) — T1–T3 cover them in lockstep; each surface has a test.
- **goimports gotcha:** `make fmt` after every task; `git status --short` clean.
- **Tests must go red:** every task's negative branch demonstrated before merge.
- **HTTP regression:** T5/T9 explicitly assert HTTP repair is unchanged.
