# WS Deterministic Multi-Step Cases (TestCase.Steps) — Design

Status: Design (autonomous; approach A chosen 2026-07-23). Predecessors: M3-2
Scout WS-case skeleton (`7c7fce5`), quickwins 2+3 (`dc260a5`), minor cleanup
(`5d413cd`). Trigger: dogfood Finding 3 (Steer-LLM WS orchestration drift +
broken sequencing) and run-5 (device-ack fails — skeleton scope, no send step).

## Goal

Make a WebSocket `connect → send → receive → assert` sequence execute
**deterministically within a single test case** — no Steer-LLM improvisation of
the action sequence — sharing one connection. This closes the gap M3-2 left:
M3-2 made Scout emit WS case *skeletons* deterministically, but *execution* of
the connect/send/receive sequence is still Steer-LLM, which drifts (uses
`api_request` on a WS case) and breaks sequencing (connects twice, never sends).

## Why now

- M3-2 + F4 made WS cases reach `ws_connect` and a single `ws_receive` reliably,
  so the engine mechanics (dial, auth inject, handshake, field assert, framing,
  connection echo) are proven.
- The remaining failure (run-5 `device-ack`) is the skeleton scope: WSCases
  emits connect + receive but no **send** step, and even if it did, the
  connect/send/receive *ordering* is Steer-improvised per case. Deterministic
  multi-step execution is the root fix.

## Approach chosen: A — `TestCase.Steps` + a deterministic execution path

A case may carry an ordered `Steps []TestStep`. When present, the agent runs the
steps deterministically against the `WebSocketExecutor` within the case's own
context, **bypassing Steer for the action sequence**. All steps share the case's
`caseID`, so connections keyed `<caseID>:<connectionID>` are reused across steps
with **no change to the connection table or its per-case isolation**.

### Rejected alternative B — upgrade `DependsOn` to cross-case connection sharing

Rejected because:

1. It does **not** fix determinism. DependsOn-sharing only lets a receive case
   reuse a connect case's connection; Steer would still improvise each case's
   action, so the Finding-3 drift/broken-sequencing root cause remains.
2. It **breaks the per-case connection-isolation invariant** (a load-bearing
   cleanup/concurrency property: case-exit closes only that case's connections).
   Cross-case sharing needs a global connection namespace or
   connection-inheritance semantics, plus a lifecycle fix (a connect case's
   context closes its connection when the connect case ends).
3. The connection table already shares within a case, so A needs no
   connection-model change at all.

### Coexistence

Non-`Steps` cases are entirely unchanged: HTTP/process/code/file/wait/MCP/browser
cases keep using `RuleEngine.Match` → Steer fallback; ad-hoc WS cases without a
derived exchange keep using Steer. `Steps` is an opt-in path for declared-protocol
WS scenarios whose exchange Scout can derive from protocol + goal.

## Design

### `TestCase.Steps` + `TestStep`

Add to `TestCase` (`internal/head/agent/types.go`):

```go
Steps []TestStep `json:"steps,omitempty"`
```

Non-empty `Steps` ⇒ deterministic multi-step case (the single `Action`/`Body`
hint is ignored for execution; `Steps` drives it).

`TestStep` is a lightweight declarative form (reuses the string-`Body`
convention of `TestCase.Body`; no expression evaluator):

```go
type TestStep struct {
    Action       string         // "ws_connect"|"ws_send"|"ws_receive"|"ws_disconnect"
    ConnectionID string         // case-scoped connection ref; same id across steps ⇒ shared
    Role         string         // ws_connect only (selects protocol role + handshake)
    Body         string         // JSON params/payload hint, parsed per action type
    Asserts      map[string]any // ws_receive only; constrained path→value exact match
}
```

Rationale for a declarative `TestStep` (vs wrapping the existing typed action
structs `WSConnectAction`/…): the existing structs are execution-shaped
(`URL`, `Headers`, `Subprotocols`, …) and would push protocol-resolution details
into Scout's output. A declarative step lets the deterministic path resolve the
role/protocol/auth/handshake exactly as `doConnect` already does, keeping
protocol details out of the case data (the engine's core generality principle).

### Deterministic execution path

The per-case execution loop today is: `RuleEngine.Match(tc)` → on miss, Steer
LLM. Add a `Steps` branch evaluated first:

- `len(tc.Steps) > 0` → run the steps in order through `WebSocketExecutor` under
  the **case context** (same `caseID` for every step), producing an aggregated
  `ExecutorResult`; do not invoke Steer.
- else → existing path unchanged.

Sequential semantics:

1. `ws_connect` — dial + auth inject + handshake `await_type` read (exactly the
   current `doConnect` + handshake path). Connection stored at
   `<caseID>:<ConnectionID>`.
2. `ws_send` — write the `Body` payload on `ConnectionID`.
3. `ws_receive` — read one message of the awaited type on `ConnectionID` and
   evaluate `Asserts` (current `doReceive` field-assert path).
4. `ws_disconnect` (optional) — close.

A step failure short-circuits the case: a connect failure (dial/auth/unknown
role/handshake timeout) ⇒ no send/receive; a receive assert mismatch ⇒ case
fails at that step. The aggregated result records each step's outcome; the
**decisive verdict is the final `ws_receive` assert** (all prior steps OK). The
Examiner judges the full step chain as evidence (the #3 WS-aware judge rule
already requires a real upgraded exchange + matched receive to pass — a
`Steps` case that completes supplies exactly that).

Connection sharing is automatic: every step runs under the case's `caseID`, and
`store`/`lookup` key on `caseNamespace(ctx, ConnectionID)`; steps that cite the
same `ConnectionID` hit the same `wsEntry`. No connection-table change.

### Scout `WSCases` generation

For a declared-protocol service whose goal expresses a send/receive exchange,
emit **one case with `Steps`** instead of separate connect + receive cases:

- `ws_connect` step from the role (handshake `await_type`).
- `ws_send` step whose type is the **client-sent** type the #2 send-verb
  heuristic excludes from receive (symmetry: what #2 excludes as "client-sent"
  becomes the send input here).
- `ws_receive` step whose type is the following receive type, with `Asserts`
  derived from the goal (e.g. `payload.approved=true`).

Example — goal `"send device:command, verify device:ack approved=true"` with a
`web` role (handshake `devices:sync`):

```
Steps:
  - ws_connect  Role=web            ConnectionID=c1   (await devices:sync)
  - ws_send     ConnectionID=c1     Body={"type":"device:command"}
  - ws_receive  ConnectionID=c1     Asserts={"type":"device:ack","payload.approved":true}
```

Connect-only scenarios (handshake await with no derived send/receive exchange)
keep the existing single receive-case form. The exact split + `WSCases` test
updates are plan items.

### Constraints preserved

- **No expression evaluator**: `Asserts` are constrained path→value exact match
  (numeric-normalized), exactly as `doReceive`/`checkAsserts` today.
- `coder/websocket v1.8.14` only; **no new dependencies**.
- Connection table and per-case isolation unchanged.
- Determinism: sorted role iteration, no map-order nondeterminism in generation.

### Error handling

- Connect failure ⇒ case fails, later steps skipped, evidence names the failure
  point (no secret in the echoed URL — existing `stripQuery` hygiene).
- Receive assert mismatch ⇒ case fails at that step (matched message preserved).
- Case-context cancellation closes the shared connection via the existing
  `store()` cleanup goroutine.

### Testing

- `TestStep` parse/round-trip (YAML + JSON).
- Deterministic execution against an in-process WS test server: full
  connect→send→receive→assert **pass**; and each short-circuit failure mode
  (connect fail, assert mismatch) recorded. Reuse the dogfood target's shape
  (login + `/realtime` send/ack) as a regression harness.
- `WSCases`: emits `Steps` for an exchange goal; existing connect/handshake
  behavior preserved; deterministic order.
- All existing WS tests stay green (M0 fallback, handshake, framing, field
  asserts, connection echo, role carriers).

### Non-goals

- Not replacing Steer for non-WS or ad-hoc WS.
- Not adding expression/path-language assertions.
- Not changing connection isolation or the `DependsOn` ordering semantics.

## Open questions (resolve in the plan)

1. Exact hook point in the agent per-case execution loop (read during planning;
   likely where `RuleEngine.Match` is called and the Steer fallback begins).
2. Whether `WSCases` migrates *all* its output to `Steps` or keeps connect-only
   cases as today and adds `Steps` only for derived exchanges (lean: keep
   connect-only, add Steps for exchanges).
3. Handshake `await_type` representation: on the connect step (read-on-connect,
  current behavior) vs a separate leading `ws_receive` step.
4. Aggregated `ExecutorResult` shape for the Examiner (one result summarizing
  the chain vs per-step results).
