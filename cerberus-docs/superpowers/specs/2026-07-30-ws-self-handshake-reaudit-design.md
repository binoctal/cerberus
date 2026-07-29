# WS Self-Handshake Re-Await (LLM-Authored) — Design

> Status: design (autonomous). Date: 2026-07-30.
> A "Phase 1 narrow leftover" from the A1 WS unsound/runtime-fallback work. Documents: `cerberus-docs/superpowers/specs/2026-07-28-ws-a1-unsound-fallback-design.md:124-126`, `2026-07-28-ws-a1-runtime-fallback-design.md:209-211`.

## Problem

A `ws_connect` step auto-awaits and **consumes** its role's `Handshake.AwaitType` frame (`internal/head/agent/websocket.go:360-414`, drained in `readMatching`). The **deterministic** scout emitter defends against a later `ws_receive` of that same type (`internal/head/scout/ws_cases.go:145-169`, `sanitizeTypeID(typ) == handshakeID → continue`). The **LLM-authored** emitter does NOT: `internal/head/scout/assembly.go` builds `ws_receive` steps verbatim, and the soundness checker `llmWSFlowSound` reports the type sound (it is grounded), so the case marks the role covered and suppresses the deterministic fallback.

Result: an LLM-authored case of the shape
```
ws_connect connection_id=C role=R
ws_receive connection_id=C type=<R's Handshake.AwaitType>
```
passes planning soundness, marks R covered, then at runtime the receive times out (the connect already consumed the only frame of that type). The role is stranded with no fallback.

## Fix

Mirror the deterministic emitter's exclusion at assembly time. In `assembly.go`'s `flush()` (or a helper it calls), sanitize `open.Steps` before the case is appended:

1. Walk `open.Steps`. For each `ws_connect` with a non-empty `Role` whose declared role (`svcProtos[open.Service].Roles[step.Role]`) has a non-empty `Handshake.AwaitType`, record `sanitizeTypeID(awaitType)` keyed by `step.ConnectionID`.
2. Drop any `ws_receive` step on a recorded connection_id whose `sanitizeTypeID(Type)` OR any `sanitizeTypeID(Alias)` matches the recorded await type — the connect's auto-await already proved it; the frame is gone.
3. Keep all other receives. If all receives are dropped, the case naturally collapses to a connect-only `ws_flow`, which `llmWSFlowSound` already accepts as trivially sound (`ws_cases.go` "A case with no ws_receive … is trivially sound") — the connect alone proves connect+handshake, so the role IS covered.

No executor change, no protocol-schema change. Reuses `sanitizeTypeID` (`ws_cases.go:527-530`).

## Behavior

- LLM emits `ws_connect(R) + ws_receive(handshake_type)`: the redundant receive is dropped; case becomes connect-only; role covered by connect.
- LLM emits `ws_connect(R) + ws_receive(handshake_type) + ws_receive(other_type)`: only the handshake_type receive is dropped; the valid receive stays.
- A receive whose `Aliases` include the handshake type is also dropped (it would match the consumed frame).
- Deterministic path unchanged.

## Testing

- New/extended assembly test: feed `begin_case + ws_connect(role=R) + ws_receive(type=<R.AwaitType>)` → assert the persisted case has NO ws_receive of that type (connect-only), and the role is still marked covered.
- Variant: add a second `ws_receive(other_type)` → only the handshake receive is dropped.
- Variant: a receive whose `Aliases` includes the handshake type → dropped.
- `make check` EXIT 0, clean tree.

## Out of scope

- The optional-relay race in `wsRelayCases` (`ws_cases.go:196-243`): if a peer connects during the connect's optional-handshake await window, the connect consumes the signal and a later receive of the same signal times out. This is a race in the deterministic relay path, a different seam (executor-level: suppress the auto-handshake await when the same case later receives the same type on the same connection), and explicitly out of the "Phase 1 narrow leftover" scope.
- Message batching / batch-frame decomposition (separate, sprawling).
