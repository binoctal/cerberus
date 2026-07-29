# WS Optional-Handshake Relay Race — Known Gap (design seed, not implemented)

> Status: known gap, design seed only (not implemented). Date: 2026-07-30.
> Surfaced while fixing the LLM-authored self-handshake re-await (`2026-07-30-ws-self-handshake-reaudit-design.md`). Distinct seam; explicitly out of scope of that fix.

## The race

The deterministic relay emitter `wsRelayCases` (`internal/head/scout/ws_cases.go:196-243`) fires ONLY for optional handshakes (`!a.Handshake.Optional → skip`, ws_cases.go:206). For role `a` it emits:
1. `ws_connect` ConnectionID=a, Role=a (the receiver)
2. `ws_connect` for each peer role `p`
3. `ws_receive` on `a` for the peer-join `signal` (the optional `Handshake.AwaitType`)

The connect's auto-handshake await (`internal/head/agent/websocket.go` `doConnect`, ~360-414) awaits `AwaitType` with the handshake timeout. This is a RACE:
- **Common (intended) path:** the peer connects AFTER the receiver's await window times out → the connect does NOT consume the signal → the later `ws_receive` catches the peer-join signal. ✓
- **Race path:** the peer connects DURING the await window → the connect's auto-await consumes the signal frame → the later `ws_receive` times out. ✗ (false fail; the relay actually worked)

This is a timing race, not a deterministic bug. It is the mirror of the LLM-authored self-handshake re-await fixed in the sibling spec, but on the deterministic relay path and at the executor level.

## Candidate fix (not implemented)

Suppress the connect's auto-handshake await when the SAME case later `ws_receive`s the same type on the same connection. Concretely:
- `runSteps` (`internal/head/agent/execute_phases_steps.go`) pre-scans `tc.Steps`: for each `ws_connect`, collect the set of types that a LATER `ws_receive` on the same `ConnectionID` awaits (Type + Aliases). Pass that set to `WSConnectAction` as a "suppress-auto-await-types" field.
- `doConnect` (`websocket.go`): when awaiting the role's `Handshake.AwaitType`, if that type is in the suppress set, SKIP the auto-await (let the later explicit receive catch it).

This is an executor + runner change (not assembly-level like the sibling fix) and touches concurrency-sensitive timing, so it warrants its own focused design + careful TDD (a deterministic in-process relay test that forces the race window). It was deliberately left for a dedicated session.

## Why not done now

Executor-level concurrency change; higher risk than the assembly-level sibling fix. Parked for a focused session with a real race-reproducing test.
