# WS Optional-Handshake Suppress-Auto-Await (implemented)

> Status: **implemented** (2026-07-30). Originally filed as a speculative
> "relay race" design seed; analysis below corrects the trigger and records the
> shipped fix + its deterministic test.

## Origin and correction

The original seed hypothesized a *timing race*: "peer connects DURING the
receiver's await window → the connect's auto-await consumes the signal → the
later `ws_receive` times out." **That specific race is unreachable** in the
deterministic Steps runner. `runSteps` (`internal/head/agent/execute_phases_steps.go`)
is a **sequential** loop: each step's `executor.Execute` blocks until it returns.
Step 1 (receiver `ws_connect`) blocks inside `doConnect`'s `readMatching` for the
whole optional-handshake timeout; step 2 (peer `ws_connect`) can only run after
step 1 returns. So the peer-join signal always arrives *after* the receiver's
await window closes → it is buffered by the read pump → the later `ws_receive`
catches it. There is no race in sequential execution.

The **underlying defect the seed pointed at is real, though** — just with a
different, deterministically-reachable trigger: a server that pushes the
`AwaitType` frame to the client **on join** (presence broadcast / initial sync).
In that case the optional auto-await *matches and consumes* the connect-time
push, leaving the decisive later `ws_receive` nothing to match → **false fail**
(the server demonstrably sent the frame). The fix is exactly the mechanism the
seed proposed (suppress the connect-time auto-await when a later receive on the
same connection asserts the same type); only the justification changed.

A second, lesser win: suppression removes a pointless stall. Left running, the
optional await always blocks for the full handshake timeout (the signal cannot
arrive during the sequential connect step), taxing every optional-relay case.

## Scope ruling (concurrency semantics)

Suppression is **OPTIONAL-handshake only**, preserving the mandatory/optional
split (cccmemory `ws-handshake-optional-vs-mandatory-semantics`):

- **Optional** (`Handshake.Optional=true`): the connect's await is best-effort.
  When a later receive asserts the same type, suppress the await — the receive is
  the decisive assertion.
- **Mandatory**: the connect **consumes** the `AwaitType` by design, and the
  redundant later receive is dropped at assembly (`sanitizeSelfHandshakeReawait`).
  Suppression is intentionally NOT applied, so that path is unchanged.

This executor-level fix therefore composes with (does not regress) the sibling
assembly-level self-handshake re-await fix.

## Implementation

Three small, scoped changes:

1. **`types.WSConnectAction.SuppressAwaitTypes []string`**
   (`internal/types/actions_http.go`) — `json:"suppress_await_types,omitempty"`.
   Set only by the deterministic Steps runner; the Steer LLM path never sets it
   (zero value ⇒ no suppression ⇒ existing behavior).

2. **`runSteps` pre-scan** (`execute_phases_steps.go`): `suppressAwaitTypes(steps)`
   builds `connection_id → {Type + Aliases}` from every `ws_receive` on that
   connection. (Receives always follow a connection's connect, so "all receives
   on this connection" == "later receives.") Each `ws_connect` step's action is
   populated with the suppress set for its `ConnectionID`.

3. **`doConnect`** (`websocket.go`): at the top of the handshake block, if
   `role.Handshake.Optional && awaitType != "" && slices.Contains(a.SuppressAwaitTypes, awaitType)`,
   return `OK` immediately (connection already stored + alive, pump buffering).
   Mandatory handshakes fall through to the existing `readMatching` await.

## Tests (`internal/head/agent/execute_phases_steps_test.go`)

- **`TestRunStepsOptionalHandshakeSuppressesAwaitForLaterReceive`** — the
  decisive proof. Server pushes `presence:online` on join; case =
  `ws_connect` (optional handshake `presence:online`) + `ws_receive presence:online`.
  **Without the fix the receive times out (case FAILS)** — verified by disabling
  the suppress branch and watching the test go red. With the fix the connect
  skips its await, the push stays buffered, and the receive matches. Deterministic,
  no wall-clock dependency, single connection.
- **`TestSuppressAwaitTypesMap`** — pure unit test of the pre-scan (Type +
  Aliases, per-connection, ignores other connections / non-receive steps).
- **`TestRunStepsMandatoryHandshakeNotSuppressed`** — scope boundary: a
  mandatory handshake still consumes the connect-time push even when a later
  receive asserts the same type (the case fails on the receive, proving
  suppression did not fire for mandatory).
- **`TestWSConnectActionSuppressAwaitTypesRoundTrip`** (`internal/types`) —
  locks the JSON tag / round-trip of the new field.
