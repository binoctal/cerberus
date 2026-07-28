# Single-connection WS cases as Steps cases (Finding-2 fix) — 2026-07-28

## Context

`WSCasesCovered` (`internal/head/scout/ws_cases.go`) emits the connect+receive
form for a role not covered by a relay, a goal-exchange, or an LLM ws_flow case
(the no-exchange path, lines 112-142). Today this is **two kinds of free-form
TestCase** with no `Steps`:

- a `ws_connect` case (`Background: true`)
- one `ws_receive` case per decisive type, `DependsOn` the connect.

The 07-24 dogfood (Finding-2) reported these "execute fail ×3, matched=0" via
Steer drift. **Code investigation 2026-07-28 found a deeper, deterministic
cause that makes the form fundamentally broken, not merely drift-prone:**

## Root cause: per-case namespacing breaks cross-case connection sharing

The WS connection table is keyed by `caseNamespace(ctx, connectionID) =
<caseID>:<connectionID>` (`websocket.go:32`), where `caseID` is set per-case in
`executeStep` (`execute_phases.go:46`). `Background` does NOT broaden the
namespace (it only short-circuits `ProcessExecAction` in `tryRuleEngine`).

So the connect case (caseID `ws-realtime-web-connect`) opens
`ws-realtime-web-connect:web`, while a receive case (caseID
`ws-realtime-web-receive-<type>`) looks up `ws-realtime-web-receive-<type>:web`
→ **not found**. The receive can never use the connect's socket, regardless of
how the action is produced. Steer drift was a second symptom on top of this.

**Implication:** adding a WS rule-engine phase (turning the free-form TestCases
into TypedActions deterministically) would NOT fix it — the receives still
cannot reach the connect's connection across cases. The only correct fix is to
put the connect and its receives in ONE case so they share a namespace.

## Design: emit one per-role Steps case (option B)

Replace the no-exchange free-form connect + receive cases with a single
`ws_flow` Steps case per role, exactly like `wsRelayCases`/`wsStepsCase`:

```
steps = [ ws_connect(role) ] + [ ws_receive(typ) for each decisive type ]
```

This runs via `runSteps` (deterministic, no Steer) and shares the connection
within the case (one caseID → one namespace). `stepToAction` does role/auth
expansion and the handshake await, reused verbatim.

### Handshake exclusion (the one nuance)

The `ws_connect` step carries `Role`, so the executor auto-awaits the role's
`handshake.await_type` (mandatory handshake must arrive or the connect fails;
optional handshake times out but keeps the connection). Therefore the receive
steps must EXCLUDE the handshake await_type — a separate receive for it would
await a message the connect already consumed. So receives are emitted for
`wsDecisiveTypes(role, goal)` MINUS the handshake await_type MINUS relay
signals. (The handshake is still verified — by the connect step itself.)

### What changes

- `WSCasesCovered` no-exchange path: build a Steps case instead of the
  Background connect + DependsOn receives. Case ID stays
  `ws-<service>-<role>-connect`; `Action` becomes `ws_flow`; `Steps` populated.
- `wsRelayCases`/`wsStepsCase`/the `covered`/`relayConnected`/`relaySignals`
  suppression logic: unchanged.
- A role with no decisive non-handshake type emits a connect-only Steps case
  (valid: verifies the connect + handshake).

## Test impact (`internal/head/scout/ws_cases_test.go`)

These tests assert the OLD free-form structure and must be rewritten to assert
the Steps case:

- `TestWSCasesEmitsConnectAndDecisiveReceives` — now one Steps case with a
  connect step + goal-type receive steps (handshake type excluded, verified by
  the connect step).
- `TestWSCasesNoGoalMatchJustHandshake` — now a connect-only Steps case
  (handshake verified by connect).
- `TestWSCasesMultiRoleDeterministicOrder` — assert `ws_flow` case order by
  sorted role (was `ws_connect` action filter).
- `TestWSCasesIDFormat` — the Steps case ID `ws-<service>-<role>-connect`; no
  `DependsOn` (steps are in-case).
- `TestWSCasesTargetSetAndGoalTemplateBraces` — assert target/braces on the
  Steps case.
- `TestWSCasesSendVerbTokenNotReceive` — already a Steps case (exchange path);
  unchanged.

New test: a decisive handshake-only role → connect-only Steps case (handshake
not a separate receive step).

## Verification

- `make check` EXIT 0.
- The deterministic-ness is inherent (runSteps, no Steer; one namespace). A live
  dogfood would only exercise this path when the LLM produces zero ws cases for
  a non-relay role (rare); the unit tests are the primary proof.

## Out of scope

- Routing the rule engine at WS (rejected — does not fix namespacing).
- Changing the per-case namespacing contract (rejected — side effects on
  parallel cases; the Steps path already solves sharing within a case).
- The LLM-emission quality issue (unrealistic receives on connect-only goals)
  seen during the probe — separate.
