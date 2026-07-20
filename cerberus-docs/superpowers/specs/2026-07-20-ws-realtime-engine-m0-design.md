# WebSocket Realtime Engine (M0) — Design

**Date:** 2026-07-20
**Status:** Draft (brainstormed, pending review)
**Scope:** `internal/head/agent` (websocket executor, ReAct judgment), `internal/types` (WS actions + result), `internal/prompts` (WS primitive guidance), `cerberus-docs/executors/websocket.md`

## Background

Cerberus ships a WebSocket executor (`internal/head/agent/websocket.go`) with two
actions: `WSConnectAction` (dial + read one handshake message + **close**) and
`WSSendAction` (dial + write one + read one + **close**). As documented in
`cerberus-docs/executors/websocket.md`: *"Each action creates a new connection;
connections are not persisted between actions."* The executor is stateless — it
holds only a logger.

This shape cannot express real-world realtime flows. The trigger was attempting
to migrate `open-agents`' `permission-flow-real.spec.ts` into a cerberus case.
That flow requires **two concurrent authenticated connections** (a `bridge`
client and a `web` client), a message sent on one, routed by the server, and
received/asserted on the other — a full round-trip with content assertions
(`payload.approved == true`). The current executor dials-and-closes per action
and has no "wait for a matching message" or "assert message content" primitive,
so the flow is unrepresentable.

A second, broader motivation: open-agents is only the first of many
websocket/realtime systems cerberus will face. The capability must not be a
one-off customization per system. This design therefore targets a **generic,
protocol-agnostic realtime engine** whose executor contains zero
system-specific code.

Two hard constraints from the existing architecture shape the design:

1. **A TestCase passes only via a successful non-wait action.**
   `execute_phases_react_loop.go:51` returns `passed` the moment an action
   succeeds (`!isNoopWait(action)`). `finalizeResult`
   (`execute_phases_recovery.go:62`) **never** sets `passed` — only
   `failed`/`skipped`. There is no "LLM declares done" path; case success is
   action-driven.
2. **No cross-action connection state, no lifecycle hooks.** `grep` for
   `connStore`/`connPool`/`cookie`/`jar` in the agent package returns nothing.
   The `TypedExecutor` interface (`multi.go:17`) has only `Execute` — no
   `Close`/`Shutdown`. The `TestCase.Cleanup` field is declared but unused
   (dead). There is no session/case lifecycle hook to lean on.

## Goal

A **generic WS primitive engine (M0)** that lets the LLM orchestrate arbitrary
realtime protocols through ReAct, with no per-system customization. Success
criteria:

- Protocol-agnostic primitives: `WSConnect` / `WSSend` / `WSReceive` /
  `WSDisconnect`, all referencing connections by `connection_id` (no hardcoded
  role names or message fields).
- A single TestCase can run a multi-step realtime conversation (connect →
  send → receive → assert → disconnect, including concurrent connections).
- Connections live for the duration of a case and are cleaned up automatically —
  no leaks, no interface changes, no lifecycle hooks.
- The executor contains **zero** open-agents-specific code; all protocol
  knowledge is carried by the LLM via prompts and examples.
- Validated by representing the open-agents permission-flow round-trip as one
  cerberus case.

## Non-Goals

- **M1 — protocol adaptation layer** (auth strategy, framing, type-field path
  declared in `project.yaml`).
- **M2 — multi-role orchestration & timing assertions** (role abstraction,
  handshake sequences, message-order/window assertions).
- **M3 — declarative protocol descriptions & auto-adaptation** (protocol
  description files; Scout generating cases from descriptions/docs/captures).
- Non-WS realtime transports (SSE / WebRTC / raw TCP) and fully-encrypted
  undocumented binary protocols — out of scope, future extension.
- Reducing LLM token cost for protocol understanding (that is M1's purpose).

## Design Decisions

### D1 — Fine-grained primitives + LLM orchestration (not a composite action)

**Decision:** expose `connect`/`send`/`receive`/`disconnect` as independent
fine-grained actions; let the LLM orchestrate them in the existing ReAct loop.

**Rejected alternative — composite action** (a single `WSConversationAction`
holding a hardcoded steps list, executed internally by the executor). This would
be self-contained and deterministic, but **it violates generality**: every new
protocol would require rewriting a script, which is exactly the per-system
customization this design exists to eliminate. Fine-grained primitives plus LLM
understanding is what makes the engine generic — the LLM reads the protocol and
composes primitives on the fly.

**Trade-off accepted:** orchestration quality depends on the LLM, and per-run
token cost is higher than a declarative approach. This is the intentional price
of zero-config generality; M1 offsets it by persisting protocol knowledge.

### D2 — Multi-step within a single TestCase via a `decisive` flag

**Problem:** Constraint (1) means a case passes on the first successful
non-wait action. A round-trip has multiple verification points (web receives
request; bridge receives approved response), but a case can pass only once.

**Decision:** split actions into **intermediate steps** and **decisive steps**.

- Extend `isNoopWait` → `isIntermediateStep`. `WSConnect` / `WSSend` /
  `WSDisconnect` are intermediate: success does **not** trigger `passed`; the
  ReAct loop continues steering the next step.
- `WSReceive` carries a `decisive` flag:
  - `decisive=true` (default): `match` + `assert` pass → **case passed**.
  - `decisive=false`: `match` + `assert` pass → continue steering (an
    intermediate verification point); failure → action fails.
- An intermediate `WSReceive` failure still sinks the case: the failed action
  drives recovery/retry, and exhausting attempts yields `finalizeResult = failed`.

**Default `decisive=true`** because most WS verifications are "wait until the
key reply arrives and assert it" (single decisive point). Multi-step
conversations mark intermediate receives `decisive=false`. *(Default is
revisitable after M0 dogfooding — see Open Questions.)*

**Rejected alternative — independent `WSAssert` action as the sole decisive
step.** More orthogonal, but `assert` must reference messages received by prior
`receive` actions, reintroducing cross-action message state — strictly more
complex than the `decisive` flag.

**Rejected alternative — split the round-trip across multiple TestCases with
`DependsOn`.** This forces connections to outlive a single case, resurrecting
the cross-case lifecycle problem (D3 exists precisely to avoid that) and
ordering/timing fragility. Rejected.

### D3 — Connection lifetime bound to per-case context

**Problem:** Constraint (2) — no lifecycle hooks, executor interface is
`Execute`-only. How are connections cleaned up?

**Decision:** each `WSConnect` registers a goroutine that closes the connection
on `ctx.Done()`. Because `executeStep` derives a per-case context
(`context.WithTimeout`), case exit cancels the context and closes **all** of
that case's connections automatically.

- Connection table: `connectionID (uuid) → *Conn`, guarded by `sync.RWMutex`,
  held on the singleton executor. `WSConnect` generates the id (or honors an
  LLM-supplied one), stores the conn; `WSSend`/`WSReceive`/`WSDisconnect` look
  it up by id.
- Case context cancellation both closes conns (via the goroutine) and removes
  the table entries for that case (tracked per-context).
- **No `TypedExecutor` interface change, no framework hook, no leak,
  concurrency-safe** (parallel execution gives each case its own context).

### D4 — Generality: zero protocol code in the executor

**Decision:** the executor implements only the generic primitives. Anything
system-specific — authentication placement, message framing, the field that
identifies a message type, business semantics — is supplied by the LLM.

- Authentication is not a first-class concept in M0: the LLM puts credentials
  into `url` query params / `headers` / `subprotocols` as the protocol demands.
  (open-agents uses URL query tokens.) M1 promotes this to a declared strategy.
- `project.yaml` gains **no** WS schema in M0. WS cases are LLM-driven (Scout
  does not generate WS cases today; that stays).

### D5 — Match/assert via expressions over deserialized JSON (reuse invariant engine)

`WSReceive`'s `match` (which message to accept) and `assert` (field-level
verification) are **expression strings** evaluated against the deserialized
message JSON, e.g. `match="$.type == 'permission:request'"`,
`assert="$.payload.approved == true"`. No hardcoded field names.

This reuses cerberus's existing expression evaluation (the same source as
`invariants[].check` in `project.yaml`, e.g. `response.status < 500`). *Confirm
the exact engine and JSONPath-vs-dot-notation syntax during planning; if a
JSONPath dependency is needed, prefer one already in the module graph.*

## WS Primitive Actions

All reference connections by `connection_id`; no role names or protocol fields
are hardcoded.

| Action | Key fields | Semantics | Step class |
|---|---|---|---|
| `WSConnect` | `url`, `headers?`, `subprotocols?`, `connection_id` | dial → store in table → bind to per-case ctx | intermediate |
| `WSSend` | `connection_id`, `message` | send on an established connection | intermediate |
| `WSReceive` | `connection_id`, `match`, `assert?`, `timeout?`, `decisive?` | wait for a message matching `match`, optional `assert` | decisive (when `decisive=true`) |
| `WSDisconnect` | `connection_id` | close + remove | intermediate |

`WSConnect` / `WSSend` are modified forms of today's `WSConnectAction` /
`WSSendAction` (gain `connection_id`, no longer dial-and-close). `WSReceive` /
`WSDisconnect` are new.

## Judgment Model

```
intermediate action (connect/send/disconnect, or receive decisive=false) succeeds
  → ReAct continues steering (next primitive)
  → on failure: recovery / retry → finalizeResult=failed if attempts exhausted

WSReceive decisive=true: match+assert pass → case PASSED
                           match timeout / assert fail → action fail
```

## Connection Lifecycle

- `WSConnect`: dial, generate/accept `connection_id`, store `*Conn` in the
  table, spawn `go func() { <-ctx.Done(); conn.Close(...) }()`.
- `WSSend` / `WSReceive` / `WSDisconnect`: look up conn by `connection_id`;
  `WSReceive` reads with its own `timeout`; `WSDisconnect` closes + removes.
- Per-case ctx cancellation (normal exit, timeout, or panic) closes every conn
  the case opened and prunes the table.

## Impact / Change List

**New:**
- `WSReceiveAction`, `WSDisconnectAction` types (`internal/types/actions_http.go`
  alongside existing WS actions).
- Register both in `internal/types/actions_registry.go`; add deref groups in
  `internal/types/actions_deref_groups.go`.
- Connection table + `sync.RWMutex` on `WebSocketExecutor`.
- `isIntermediateStep` predicate.

**Modified:**
- `internal/head/agent/websocket.go`: rewrite `doConnect` (keep conn open,
  register ctx-cleanup, honor `connection_id`), rewrite `doSend` (reuse conn by
  id), add `doReceive` (read-loop with `match`/`assert`/`timeout`) and
  `doDisconnect`.
- `internal/head/agent/react_loop_helpers.go`: `isNoopWait` → generalized
  `isIntermediateStep` (covers WaitAction + WS intermediate actions).
- `internal/head/agent/execute_phases_react_loop.go:51`: use
  `isIntermediateStep` in the pass-gate.
- `internal/types/result_ws.go`: extend `WSResult` to carry the matched message
  and a match/assert outcome for evidence.
- `internal/prompts/`: add WS primitive guidance + a worked example so the LLM
  knows the `connection_id` / `decisive` contract.
- `cerberus-docs/executors/websocket.md`: rewrite to reflect persistent
  connections and the new primitives; fix the stale `nhooyr.io/websocket`
  reference (now `github.com/coder/websocket`).

**Unchanged:**
- `MultiExecutor` routing, Scout, `project.yaml` schema, the Examiner.

## Testing Strategy

- **Unit (local WS server via `httptest`/`coder/websocket` Accept):** each
  primitive in isolation — connect+persist, send-on-existing, receive with
  match hit/miss/timeout, assert pass/fail, disconnect cleanup.
- **Lifecycle:** open N conns, cancel ctx → assert all closed and table empty.
  Concurrent cases → assert isolation (per-case ctx).
- **Judgment:** an intermediate-step success does not pass a case; a
  `decisive=true` receive pass does; a `decisive=false` receive pass continues.
- **Integration (open-agents):** with the open-agents API+realtime stack
  running, run the permission-flow case end-to-end (requires the stack up; gate
  behind an env flag so CI without it skips).

## Validation Case — open-agents permission-flow as one TestCase

Single case, `Expectation`: "bridge receives `permission:response` with
`approved==true`". The LLM orchestrates within the case:

1. `WSConnect` bridge conn (`url` carries `?type=bridge&token=...&deviceId=...`)
2. `WSConnect` web conn (`?type=web&token=<jwt>`)
3. `WSSend` bridge: `{type:"permission:request", payload:{id, toolName, risk:"high", ...}}`
4. `WSReceive` web, `match="$.type=='permission:request'"`, `decisive=false`
   (intermediate — request reached web)
5. `WSSend` web: `{type:"permission:response", payload:{id, approved:true, ...}}`
6. `WSReceive` bridge, `match="$.type=='permission:response'"`,
   `assert="$.payload.approved==true"`, `decisive=true` → **case passed**
7. ctx exit closes both conns

No open-agents symbol appears in the executor; only in the LLM's actions.

## Roadmap (M1–M3)

Each milestone is a separate spec → plan → implementation, started after M0 is
dogfooded. Listed here for directional alignment, **not** detailed design.

- **M1 — Protocol adaptation layer.** Promote the protocol knowledge the LLM
  re-derives every run (auth strategy, framing, the type-field path) into
  `project.yaml` declarations. Lowers token cost, raises determinism/
  reproducibility. *Trigger to start:* M0 dogfooding shows which protocol facts
  the LLM most often re-derives.
- **M2 — Multi-role & timing.** Role abstraction, configurable handshake
  sequences, message-order/window assertions. Supports more complex multi-agent
  realtime topologies.
- **M3 — Declarative protocol descriptions & auto-adaptation.** A protocol
  description file format; Scout generating cases from descriptions/docs/captures.
  Gives a deterministic path for users who do not want to rely on LLM
  orchestration quality.

## Open Questions

1. **`decisive` default.** `true` is chosen for M0; revisit after dogfooding
   whether multi-step conversations are common enough to flip the default.
2. **Expression engine for `match`/`assert`.** Confirm the existing evaluation
   engine (shared with `invariants[].check`) and whether it supports
   JSONPath-style paths over nested JSON; resolve during planning.
3. **Connection id source.** LLM-supplied vs executor-generated uuid vs both
   (honor supplied, generate when absent). Design favors "both"; finalize in plan.
