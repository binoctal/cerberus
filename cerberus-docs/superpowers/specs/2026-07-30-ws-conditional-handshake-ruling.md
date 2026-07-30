# WS Conditional / Peer-Dependent Handshake — Ruling: redundant (not implemented)

> Status: ruling, 2026-07-30. Roadmap #3 ("条件/peer-dependent 握手 — await
> only-if-peer / no-await connect"). Assessed after #1 (suppress-auto-await) and
> #2 (generated path params) landed. Conclusion: **no non-redundant gap; not
> implemented.**

## The two asks

1. **no-await connect** — a role connect that does not block on the handshake.
2. **await only-if-peer** — await the handshake when a peer is/expected present,
   skip otherwise.

## Why both are already covered

`RoleHandshake` exposes `AwaitType`, `Timeout`, `Optional` (that is the full
knob set; there is no `Await`/`NoAwait` field). Against that:

- **no-await connect** is already expressible two ways: declare **no**
  `Handshake` on the role → `doConnect` skips the entire auto-await block
  (`websocket.go`, `if role != nil && role.Handshake != nil`); or declare
  `Optional: true` → the connect awaits but a timeout succeeds the connect and
  leaves it alive. A role that wants zero blocking simply omits the handshake.

- **await only-if-peer** *is* the `Optional` semantic: the connect awaits the
  peer-join signal, and when no peer is present the await times out and the
  connect still succeeds (peer-absent tolerant). The optional-relay stall that
  remained (the connect blocking for the full timeout even when the signal
  arrives only on a later peer-connect step) was removed by #1's
  suppress-auto-await, which skips the await entirely when a later `ws_receive`
  on the same connection asserts the type.

## The only residual slice, and why it is cosmetic

The one thing not declaratively expressible is an explicit `NoAwait`/`Await:false`
knob that unconditionally skips the connect-time await even when NO later receive
asserts the type (pure protocol-fidelity declaration without blocking). This is
marginal and already subsumed in every reachable path:

- **Deterministic Steps path:** #1 suppress + Optional + (no-Handshake) cover
  every real case.
- **Steer LLM path:** post-S3 the LLM tool surface no longer exposes
  `ws_connect` (`websocket_test.go` `TestReActLoop_IntermediateStepSkipsRecovery`
  documents this). Connects originate only from the deterministic Steps runner,
  so there is no LLM-authored handshake-await to disable.

A `NoAwait` field would therefore be a no-op decoration. Building it would be
over-engineering; the handshake story is closed by Optional (#0/F2) + #1
suppress.

## Decision

Do not implement #3. Record this ruling so a future session does not re-open it
as a gap. If a real use case for unconditional declarative no-await surfaces
(e.g., the Steer path re-exposes `ws_connect`), revisit then.
