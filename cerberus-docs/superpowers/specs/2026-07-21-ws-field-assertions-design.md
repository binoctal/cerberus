# WebSocket Realtime Engine (M2) — Field-Level Assertions (Design)

**Date:** 2026-07-21
**Status:** Design (autonomous brainstorm; pending spec review)
**Scope:** `internal/head/agent/` (`doReceive` assertion evaluation, shared JSON path walker, prompts), `internal/types/` (`WSReceiveAction.Assert`), `cerberus-docs/executors/websocket.md`
**Depends on:** M0/M1 — receive matching (`extractTypePath`, `doReceive`), `WSResult`

## Background & Motivation

M0/M1 receive matches a message by its routing key (top-level `type`, or the
declared `type_path`) and returns it; **content** is judged by the Examiner LLM
against the free-text case expectation. The steer prompt states this explicitly:
*"Content assertions (e.g. payload.approved) are judged from the received
message by the Examiner against the test case expectation — ws_receive only
confirms the message arrived."*

That works, but it leaves content verification non-deterministic (an LLM call
per judgment) and imprecise as a steering signal: when the Agent receives
`{type: "approval", payload: {approved: false}}`, it learns only that an
"approval" message arrived — not that the approval was *denied*. The Agent
cannot cheaply branch on `payload.approved` during its steering loop, and the
case's pass/fail depends on the Examiner noticing the denied flag in prose.

Field-level assertions add a **deterministic, executor-side** content check: a
`ws_receive` may declare `assert: { payload.approved: true }`, and the executor
verifies those field values when the message arrives. This gives the Agent a
precise, immediate signal and makes content verification deterministic.

### Grounding (static, not runtime)

As with M0/M1/M2-roles, no dogfooding was run; the motivation is inferred
statically from the open-agents protocol (messages carry decision-bearing
payloads like `payload.approved`, `payload.role`) and from the existing
steer-prompt statement above.

## Goal

Let a `ws_receive` optionally assert that specific fields in the matched
message equal specific values, checked deterministically by the executor —
while preserving M1 behavior for receives that declare no assertions.

Success criteria:

- A `ws_receive` with `assert: {payload.approved: true}` that matches a message
  whose `payload.approved` is `true` succeeds (as today).
- The same receive matching a message whose `payload.approved` is `false` fails
  (`OK=false`) with an error naming the path, the expected value, and the actual
  value; the matched message is still returned as evidence.
- A `ws_receive` without `assert` behaves exactly as M1 (regression-green).
- Assertions are **constrained equality** (path → expected value): no operators,
  no expression engine (M0 Constraint 3 preserved).
- A `decisive` receive with a failing assertion fails (the decisive verification
  step failed), consistent with how a decisive receive that times out fails.

## Non-Goals

- **A general expression/evaluator engine** — assertions are `{path: value}`
  equality only; no `!=`, `>`, `contains`, boolean logic, or arithmetic (M0
  Constraint 3). The roadmap shorthand `assert payload.approved == true` denotes
  equality, not an expression.
- **Array indexing / wildcard paths** — v1 walks JSON **object** dotted paths
  (matching `extractTypePath`). Array indexing (`items[0].id`) defers to a later
  sub-project if a real target needs it.
- **Assertions on connect/handshake evidence or HTTP bodies** — WS receive only
  this sub-project. (The role handshake already gates connect success via
  `await_type`.)
- **Restructuring the case `Expectation` or extending `objectiveVerdict`** —
  assertions live on the action and surface via the receive's `Success()`, not
  in the judgment layer. Examiner/expectation-side assertions are a larger,
  separate change and not warranted yet.
- **Soft/non-failing assertions** — an assertion is a hard check; a failing
  assertion fails the receive. Soft observation is achieved by omitting `assert`
  and letting the Examiner judge (unchanged M1 path).

## Design Decisions

### D1 — Assertion is an optional field on `WSReceiveAction` (executor-side)

A `ws_receive` may carry `assert: {<dotted-path>: <expected-value>, ...}`. After
the executor matches a message by type (M1 behavior, unchanged), it evaluates
each assertion: walk the dotted path in the matched JSON message and compare the
leaf to the expected value. **All assertions must hold for the receive to
succeed.** A failed assertion makes the receive `OK=false`.

This is a graceful enhancement: `assert` absent → the receive is arrival-only,
byte-identical to M1.

**Rejected — Examiner/Expectation-side assertions (extend `objectiveVerdict`).**
Would require restructuring the free-text `Expectation` into structured
assertions and changing the judgment core; larger, riskier, and gives the Agent
no steering-loop signal. The action-side form is smaller, deterministic, and
feeds the Agent a precise pass/fail it can branch on.

**Rejected — a separate `ws_assert` action.** Adds an action type and a
two-step dance (receive, then assert against "the last message") with state to
track. Putting `assert` on `ws_receive` is simpler and checks the exact message
that matched, with no ambiguity about which message is asserted.

### D2 — Constrained equality (no evaluator)

Assertions are `{path: expected}` equality. The path is a dotted JSON object
path (e.g. `payload.approved`, `data.user.role`). The expected value is a JSON
scalar (`bool`, `string`, `number`, or `null`). Comparison is deep equality with
**numeric normalization**: JSON decodes all numbers to `float64`, so `5`
(expected, YAML/JSON int) and `5.0` (actual, decoded) must compare equal. No
other coercion.

This preserves M0 Constraint 3: there is no expression language, only
path-walk + equality, implemented by a small built-in (D3).

### D3 — Reuse and generalize the M1 path walker

M1's `extractTypePath(data, path)` walks a dotted path and returns the leaf as a
string (for routing-key matching). Generalize it: extract a new
`extractPath(data, path) (any, bool)` that returns the leaf as `any` (any JSON
value, or `(_, false)` if the path is absent or traverses a non-object). Refactor
`extractTypePath` to call `extractPath` and assert the leaf is a string (its
current contract is unchanged). Assertions use `extractPath`. DRY: one path
walker, two callers.

### D4 — A failed assertion fails the receive (consistent signal)

When any assertion does not hold, `doReceive` returns
`WSResult{OK:false, Err:"receive: assert <path>: expected <v>, got <actual>", MatchedMessage: <matched>, SeenMessages: <seen>, ...}`.
The matched message is still returned (evidence preserved), and non-matching
messages accumulated so far are still in `SeenMessages`. The error names the
**first** failing assertion encountered (assertions evaluated in deterministic
order — see Schema).

For a **decisive** receive, a failed assertion fails the decisive verification
step → the case fails, exactly as a decisive receive that times out. For a
non-decisive receive, a failed assertion is a failed intermediate step → the
Agent's recovery/retry path. This is the existing semantics for a failed
receive; assertions simply add a new reason a receive can fail, with a precise
message.

### D5 — Backward compatibility / M1 fallback

`assert` absent (or empty) → `doReceive` is byte-identical to M1. No new result
fields are required: assertion pass/fail is conveyed via the existing `OK` /
`Err` / `MatchedMessage`. The `WSResult` schema is unchanged.

## Schema

`WSReceiveAction` (`internal/types/actions_http.go`) gains:

```go
// Assert optionally declares field-level equality checks evaluated against the
// matched message after type matching. Each key is a dotted JSON object path
// (e.g. "payload.approved"); each value is the expected scalar. All assertions
// must hold for the receive to succeed; a failed assertion fails the receive
// with a message naming the path, expected, and actual value. Empty/absent
// means no assertions (M1 arrival-only behavior). Constrained equality only —
// no expression engine.
Assert map[string]any `json:"assert,omitempty"`
```

`map[string]any` mirrors the existing `role.Params map[string]string` style and
reads naturally as LLM-emitted JSON:
`ws_receive {connection_id, type:"approval", assert:{payload.approved: true}}`.

**Evaluation order:** Go map iteration is non-deterministic, so for a
deterministic "first failing assertion" error message, `doReceive` evaluates
assertions in **sorted key order**. (Single-assertion receives — the common
case — are unaffected.)

## Executor Changes

**`doReceive`** (`internal/head/agent/websocket.go`), after the type match
succeeds (the branch that currently returns `OK:true, MatchedMessage, SeenMessages`):

1. If `len(a.Assert) == 0` → return as today (M1).
2. Else, for each path in sorted key order: `actual, ok := extractPath(data, path)`;
   if `!ok` → assertion fail (path absent); else if `!valueEqual(actual, expected)`
   → assertion fail (value mismatch). On the first failure, return
   `WSResult{OK:false, Err:"receive: assert <path>: expected <expected>, got <actual-or-missing>", MatchedMessage: string(data), SeenMessages: seen, Latency: ...}`.
3. If all pass → return `WSResult{OK:true, MatchedMessage, SeenMessages, ...}` (as today).

**`extractPath`** (`internal/head/agent/ws_protocol.go`): generalized walker
returning `any` (D3). `extractTypePath` refactored to
`v, ok := extractPath(data, path); s, ok2 := v.(string); return s, ok && ok2`
(its contract — non-string leaf does not match — is preserved).

**`valueEqual`** (`internal/head/agent/websocket.go`): deep equality with numeric
normalization. If both operands are numeric (`reflect` kind), compare as
`float64`; otherwise `reflect.DeepEqual`. Handles JSON `float64` vs YAML `int`
for the same logical number.

## Validation

Extend `WSReceiveAction.Validate()` (`internal/types/actions_http.go`):
- Each assertion path key must be non-empty.
- (No constraint on expected values — any JSON value is valid. An expected
  `null` asserts the field is **present-and-null** (a distinct case from an
  absent key, which `extractPath` reports as `(_, false)` — see Open
  Questions).)

## Prompt & Docs

- `prompts.go` steer prompt (single raw-string literal, inline edit): extend the
  `ws_receive` bullet to list `assert?`; revise the "content judged by Examiner"
  bullet to state that `assert` makes a content check **deterministic**
  (executor-side, path→value equality; omit `assert` to let the Examiner judge,
  as in M1). Note: a failed `assert` fails the receive (and the case if
  `decisive`).
- `cerberus-docs/executors/websocket.md`: document `assert` under the receive
  action — the field, evaluation (after type match, all must hold, sorted order),
  failure semantics (receive fails, matched message still evidence), the
  constrained-equality/no-evaluator constraint, numeric normalization, and the
  M1 fallback.

## Testing

Table-driven, mirroring M1/M2-roles:
- `extractPath` unit tests: nested object path returns leaf; absent path →
  `(_, false)`; non-object mid-path → `(_, false)`; non-string leaf
  (number/bool/array/object) is returned as `any` and `valueEqual`
  deep-compares it. `extractTypePath` still returns string-only (regression).
- `doReceive` assertion tests:
  - single assert, value matches → `OK:true`, `MatchedMessage` set.
  - single assert, value mismatch → `OK:false`, err names path/expected/actual,
    `MatchedMessage` still set, `SeenMessages` accumulated.
  - assert path absent → `OK:false` ("got <missing>").
  - multiple asserts, all pass → `OK:true`.
  - multiple asserts, one fails → `OK:false` naming it; (sorted-order
    determinism: with two failing asserts, the lexicographically-first path is
    reported — assert that exact path).
  - numeric equality: expected `5` (int) vs actual `5` (float64) → pass.
  - bool / string value types.
  - no `assert` → M1 behavior (regression).
- `WSReceiveAction.Assert` JSON round-trip (`ws_actions_test.go`).
- Prompt test: `ws_receive` bullet mentions `assert`; the revised content bullet
  is accurate.
- Validation: empty path key rejected.

## Impact / Change List

**New:** none (extends existing files).
**Modified:**
- `internal/types/actions_http.go` — `WSReceiveAction.Assert` + Validate.
- `internal/head/agent/ws_protocol.go` — `extractPath`; `extractTypePath` refactor.
- `internal/head/agent/websocket.go` — `doReceive` assertion evaluation; `valueEqual`.
- `internal/head/agent/prompts.go` — `ws_receive` + content bullets.
- `cerberus-docs/executors/websocket.md` — `assert` documentation.

**Unchanged:** receive type-matching, decisive/intermediate judgment, the M1
arrival-only path, `WSResult` schema, the Examiner.

## Open Questions

1. **`null` expected values.** Does `assert: {payload.note: null}` mean "the
   field is JSON null" or "the field is absent"? `extractPath` returns a present
   `nil` for an explicit JSON null and `(_, false)` for an absent key. v1
   treats `null` expected as "present-and-null" (absent remains a separate
   "missing" failure). If dogfooding needs "absent" semantics, add it later.
2. **Dogfooding** remains deferred (same as M0/M1/M2-roles). Whether the LLM
   emits useful `assert` fields (and whether deterministic content checks change
   case outcomes) is validated by dogfooding / M3.
3. **Array paths.** Deferred (Non-Goal) — revisit if a real target's payloads
   require indexing into arrays.
