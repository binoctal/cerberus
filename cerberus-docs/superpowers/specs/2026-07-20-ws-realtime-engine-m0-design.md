# WebSocket Realtime Engine (M0) — Design

**Date:** 2026-07-20
**Status:** Draft (brainstormed, revised after self-review)
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
received on the other — a full round-trip. The current executor dials-and-closes
per action and has no "wait for a message" primitive, so the flow is
unrepresentable.

A second, broader motivation: open-agents is only the first of many
websocket/realtime systems cerberus will face. The capability must not be a
one-off customization per system. This design therefore targets a **generic,
protocol-agnostic realtime engine** whose executor contains zero
system-specific code.

Two hard constraints from the existing architecture shape the design:

1. **A TestCase passes only via a successful action.** Case `passed` is driven
   by an action succeeding — through the ReAct loop
   (`execute_phases_react_loop.go:51`), the rule engine
   (`execute_phases_rule_engine.go:45,74`), or recovery
   (`execute_phases_recovery.go:49`). `finalizeResult`
   (`execute_phases_recovery.go:62`) sets only `failed`/`skipped`, never
   `passed`. The LLM has **no "declare done"** path: `steer`
   (`executor_steer.go:44`) returns exactly one action, never a completion
   signal.
2. **No cross-action connection state, no lifecycle hooks.** `grep` for
   `connStore`/`connPool`/`cookie`/`jar` in the agent package returns nothing.
   The `TypedExecutor` interface (`multi.go:17`) has only `Execute` — no
   `Close`/`Shutdown`. The `TestCase.Cleanup` field is declared but unused
   (dead). There is no session/case lifecycle hook to lean on.

A third finding shapes the assertion model:

3. **Cerberus has no runtime expression evaluator.** `invariant.check` strings
   in `project.yaml` (e.g. `response.status < 500`) are **never machine-evalued**;
   `scout.go:100` feeds them to the LLM as text and `examiner/judge.go:42` has
   the LLM judge them. They look like expressions but are natural-language
   prompts. M0 must not assume an evaluator exists.

## Goal

A **generic WS primitive engine (M0)** that lets the LLM orchestrate arbitrary
realtime protocols through ReAct, with no per-system customization. Success
criteria:

- Protocol-agnostic primitives: `WSConnect` / `WSSend` / `WSReceive` /
  `WSDisconnect`, all referencing connections by `connection_id` (no hardcoded
  role names or message fields).
- A single TestCase can run a multi-step realtime conversation (connect →
  send → receive → disconnect, including concurrent connections).
- Connections live for the duration of a case and are cleaned up automatically —
  no leaks, no interface changes, no lifecycle hooks.
- The executor contains **zero** open-agents-specific code; all protocol
  knowledge is carried by the LLM via prompts and examples.
- Validated by representing the open-agents permission-flow round-trip as one
  cerberus case.

## Non-Goals

- **M1 — protocol adaptation layer** (auth strategy, framing, configurable
  type-field path).
- **M2 — multi-role orchestration, timing & field assertions** (role
  abstraction, handshake sequences, message-order/window assertions,
  field-level `assert`).
- **M3 — declarative protocol descriptions & auto-adaptation** (protocol
  description files; Scout generating cases from descriptions/docs/captures).
- Non-WS realtime transports (SSE / WebRTC / raw TCP) and fully-encrypted
  undocumented binary protocols — out of scope, future extension.
- A general expression/JSONPath evaluator (cerberus has none today; M0 does not
  introduce one).
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

### D2 — Multi-step within a single TestCase; `decisive` flag + recovery guard

**Problem:** Constraint (1) means a case passes on a successful action. A
round-trip has multiple steps, but a case can pass only once.

**Decision:** split actions into **intermediate steps** and **decisive steps**.

- Extend `isNoopWait` → `isIntermediateStep`. `WSConnect` / `WSSend` /
  `WSDisconnect` are intermediate: success does **not** trigger `passed`; the
  ReAct loop continues steering the next step.
- `WSReceive` carries a `decisive` flag:
  - `decisive=true` (default): a message of the awaited `type` arrives →
    **case passed**.
  - `decisive=false`: arrival → continue steering (an intermediate checkpoint);
    timeout → action fails.
- An intermediate `WSReceive` timeout still sinks the case: the failed action
  drives recovery/retry, and exhausting attempts yields `finalizeResult = failed`.

**Default `decisive=true`** because most WS verifications are "wait until the key
reply arrives" (single decisive point). Multi-step conversations mark
intermediate receives `decisive=false`. *(Default revisitable after M0
dogfooding — see Open Questions.)*

**Recovery guard (critical).** Without protection, an intermediate step that
succeeds falls through to Phase 7 `tryRecovery` (`execute_phases_react_loop.go`),
which unconditionally calls `Recover(..., lastResult, ...)` (`recovery.go:49`)
using **failure** semantics (`"Failed action... Error: <summary>"`,
`recovery.go:55`). Fed a *successful* result, the LLM's behavior is undefined
and typically returns `Skip`, which:
- triggers one extra LLM call per intermediate step (token waste — 5 spurious
  calls in the permission-flow), and
- sets `recoverySkipped`, which makes `finalizeResult` mis-label a non-passing
  case as `StepSkipped` instead of `failed` (`execute_phases_recovery.go:63`).

Therefore Phase 7 must **skip recovery when an intermediate step succeeded**:
`if !isIntermediateStep(action) || !newResult.Success() { tryRecovery }`. The
existing `isNoopWait` path benefits from the same guard.

**Rejected — independent `WSAssert` action as the sole decisive step.** More
orthogonal, but `assert` must reference messages received by prior `receive`
actions, reintroducing cross-action message state — strictly more complex than
the `decisive` flag.

**Rejected — split the round-trip across multiple TestCases with `DependsOn`.**
Forces connections to outlive a single case, resurrecting the cross-case
lifecycle problem (D3 exists to avoid that) and ordering/timing fragility.

### D3 — Connection lifetime bound to per-case context

**Problem:** Constraint (2) — no lifecycle hooks, executor interface is
`Execute`-only.

**Decision:** each `WSConnect` registers a goroutine that closes the connection
on `ctx.Done()`. `executeStep` derives a per-case context
(`context.WithTimeout`, `execute_phases.go:40`) **when `PerCaseTimeout > 0`**;
case exit cancels it and closes all of that case's connections.

- Connection table: `connectionID → *Conn`, guarded by `sync.RWMutex`, held on
  the singleton executor.
- **`connection_id` is unique within a case** (executor generates a uuid when
  the LLM omits it; honors an LLM-supplied id only within that case's
  namespace). Parallel cases cannot collide on a shared id like `"conn1"`.
- Case context cancellation both closes conns and prunes the table.

**Dependency & caveat:** D3 relies on `PerCaseTimeout > 0`. The default config
sets it to **2 minutes** (`types.go:124`), which is fine for the
permission-flow but may be tight for long realtime sessions — callers tuning
that down to `0` lose per-case isolation. This is documented as a config
constraint, not a code dependency.

**No `TypedExecutor` interface change, no framework hook, no leak,
concurrency-safe** (parallel execution gives each case its own context).

### D4 — Generality: zero protocol code in the executor

The executor implements only the generic primitives. Anything system-specific —
authentication placement, message framing, business semantics — is supplied by
the LLM.

- Authentication is not first-class in M0: the LLM puts credentials into `url`
  query params / `headers` / `subprotocols` as the protocol demands (open-agents
  uses URL query tokens). M1 promotes this to a declared strategy.
- `project.yaml` gains **no** WS schema in M0. WS cases are LLM-driven (Scout
  does not generate WS cases today; that stays).

### D5 — Type-based matching; assertions via Examiner (no evaluator)

**Problem:** Constraint (3) — there is no runtime expression evaluator to reuse.

**Decision:** `WSReceive` matches a message by its **top-level `type` field**
(exact equality, e.g. waits until a message with `type ==
"permission:response"` arrives). M0 performs **no field-level assertion**;
content correctness (`payload.approved == true`) is judged by the case's
`Expectation` via the existing Examiner LLM path — exactly how HTTP action
success ("request returned 200") is separated from business correctness ("the
response body was valid") today.

- This avoids introducing an evaluator cerberus has never had, and stays
  consistent with the LLM-driven judgment model used by invariants and the
  Examiner.
- **M0 boundary:** assumes messages are JSON with a top-level `type` used for
  routing (open-agents and most JSON WS protocols satisfy this). Protocols
  whose routing key lives elsewhere (nested field, `event`/`action`, non-JSON)
  are **not supported in M0** — the configurable type-field path is M1's job.
- **Semantic implication:** `decisive` receive `passed` means *"the key message
  arrived"*, not *"its contents were correct"*. A case can pass the flow yet
  have the Examiner flag a content mismatch at session end. This matches
  cerberus's existing two-layer judgment (action success vs. expectation).

**Rejected — machine expression/JSONPath `match`+`assert` (original draft).**
Based on the false premise that an evaluator existed. Building one is scope
creep M0 should not take on; M2 may add field assertions if dogfooding demands.

## WS Primitive Actions

All reference connections by `connection_id`; no role names or protocol fields
are hardcoded.

| Action | Key fields | Semantics | Step class |
|---|---|---|---|
| `WSConnect` | `url`, `headers?`, `subprotocols?`, `connection_id?` | dial → store in table → bind to per-case ctx | intermediate |
| `WSSend` | `connection_id`, `message` | send on an established connection | intermediate |
| `WSReceive` | `connection_id`, `type`, `timeout?`, `decisive?` | wait for a message whose top-level `type` matches | decisive (when `decisive=true`) |
| `WSDisconnect` | `connection_id` | close + remove | intermediate |

- `WSReceive` scans the inbound stream until a matching `type` arrives or
  `timeout` hits. **Non-matching messages are not consumed silently**: they are
  appended to the result's evidence (so the Examiner sees the full conversation)
  and the read loop continues.
- `WSConnect` / `WSSend` are modified forms of today's actions (gain
  `connection_id`, no longer dial-and-close). `WSReceive` / `WSDisconnect` are
  new.

## Judgment Model

```
intermediate action (connect/send/disconnect, or receive decisive=false) succeeds
  → SKIP tryRecovery, ReAct continues steering (next primitive)
  → on failure: tryRecovery → retry → finalizeResult=failed if attempts exhausted

WSReceive decisive=true: awaited type arrives → case PASSED
                           timeout → action fail
```

The recovery guard (D2) is what makes multi-step work without spurious LLM calls
or mis-labeled skips.

## Connection Lifecycle

- `WSConnect`: dial, generate (or accept, case-namespaced) `connection_id`,
  store `*Conn`, spawn `go func() { <-ctx.Done(); conn.Close(...) }()`.
- `WSSend` / `WSReceive` / `WSDisconnect`: look up conn by `connection_id`;
  `WSReceive` reads with its own `timeout`; `WSDisconnect` closes + removes.
- Per-case ctx cancellation (normal exit, timeout, panic) closes every conn the
  case opened and prunes the table.

## Impact / Change List

**New:**
- `WSReceiveAction`, `WSDisconnectAction` types (`internal/types/actions_http.go`).
- Register both in `internal/types/actions_registry.go` (so `actionFromEnvelope`
  in `executor_steer.go:44` / `recovery.go:66` can parse LLM JSON into them);
  add deref groups in `internal/types/actions_deref_groups.go`.
- Connection table + `sync.RWMutex` on `WebSocketExecutor`.
- `isIntermediateStep` predicate.

**Modified:**
- `internal/head/agent/websocket.go`: rewrite `doConnect` (keep conn open,
  register ctx-cleanup, honor `connection_id`), rewrite `doSend` (reuse conn by
  id), add `doReceive` (read-loop by `type` + `timeout`, accumulate non-matches
  as evidence) and `doDisconnect`.
- `internal/head/agent/react_loop_helpers.go`: `isNoopWait` → generalized
  `isIntermediateStep` (covers WaitAction + WS intermediate actions).
- `internal/head/agent/execute_phases_react_loop.go`: (a) line 51 pass-gate uses
  `isIntermediateStep`; (b) Phase 7 gains the recovery guard
  (`!isIntermediateStep(action) || !newResult.Success()`).
- `internal/types/result_ws.go`: extend `WSResult` to carry the matched message
  + non-matching messages seen during the scan, as evidence.
- `internal/prompts/`: add WS primitive guidance + a worked example so the LLM
  knows the `connection_id` / `decisive` / `type` contract.
- `cerberus-docs/executors/websocket.md`: rewrite for persistent connections and
  the new primitives; fix the stale `nhooyr.io/websocket` reference (now
  `github.com/coder/websocket`).

**Unchanged:** `MultiExecutor` routing, Scout, `project.yaml` schema, the
Examiner (it already judges `Expectation` via LLM).

## Testing Strategy

- **Unit (local WS server via `httptest`/`coder/websocket` Accept):** each
  primitive in isolation — connect+persist, send-on-existing, receive match
  hit/miss/timeout, non-match accumulation, disconnect cleanup.
- **Lifecycle:** open N conns, cancel ctx → assert all closed and table empty;
  parallel cases → assert isolation; connection_id reuse across parallel cases
  does not collide.
- **Judgment:** intermediate-step success does **not** pass a case and does
  **not** invoke recovery; a `decisive=true` receive arrival passes; a
  `decisive=false` receive arrival continues; recovery is invoked only on actual
  failures.
- **Integration (open-agents):** with the open-agents API+realtime stack
  running, run the permission-flow case end-to-end (requires the stack up; gate
  behind an env flag so CI without it skips).

## Validation Case — open-agents permission-flow as one TestCase

Single case, `Expectation`: "bridge receives a `permission:response` message
with `payload.approved == true`". The LLM orchestrates within the case:

1. `WSConnect` bridge conn (`url` carries `?type=bridge&token=...&deviceId=...`)
2. `WSConnect` web conn (`?type=web&token=<jwt>`)
3. `WSSend` bridge: `{type:"permission:request", payload:{id, toolName, risk:"high", ...}}`
4. `WSReceive` web, `type="permission:request"`, `decisive=false`
   (intermediate — request reached web)
5. `WSSend` web: `{type:"permission:response", payload:{id, approved:true, ...}}`
6. `WSReceive` bridge, `type="permission:response"`, `decisive=true` → **case passed**
7. ctx exit closes both conns

The `approved==true` content check is **not** in the receive — it lives in
`Expectation` and is judged by the Examiner from evidence. No open-agents symbol
appears in the executor; only in the LLM's actions.

## Roadmap (M1–M3)

Each milestone is a separate spec → plan → implementation, started after M0 is
dogfooded. Listed for directional alignment, **not** detailed design.

- **M1 — Protocol adaptation layer.** Promote the protocol knowledge the LLM
  re-derives every run (auth strategy, framing, the configurable type-field
  path that lifts M0's "top-level `type`" assumption) into `project.yaml`
  declarations. Lowers token cost, raises determinism/reproducibility.
  *Trigger:* M0 dogfooding shows which protocol facts the LLM most often
  re-derives, and which protocols break the top-level-`type` assumption.
- **M2 — Multi-role, timing & field assertions.** Role abstraction, configurable
  handshake sequences, message-order/window assertions, and the field-level
  `assert` that M0 deliberately omits.
- **M3 — Declarative protocol descriptions & auto-adaptation.** A protocol
  description file format; Scout generating cases from descriptions/docs/captures.

## Open Questions

1. **`decisive` default.** `true` is chosen for M0; revisit after dogfooding
   whether multi-step conversations are common enough to flip the default.
2. **`connection_id` source.** Executor-generated uuid (case-namespaced) vs
   honoring an LLM-supplied id. Design favors "generate when absent, accept
   case-namespaced otherwise"; finalize in plan.
3. **Top-level `type` coverage.** M0's matching assumption covers JSON protocols
  routed by a top-level `type`. Confirm via M0 dogfooding how often real targets
  deviate (to size M1's type-field-path work).
