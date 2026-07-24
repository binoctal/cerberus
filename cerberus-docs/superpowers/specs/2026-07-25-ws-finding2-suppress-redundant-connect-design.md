# WS Finding-2 — Suppress Redundant Single-Connection WS Cases — Design — 2026-07-25

## Background

The 2026-07-24 live execution dogfood
(`cerberus-docs/technical/dogfood/2026-07-24-ws-relay-live-execution-dogfood.md`)
surfaced a reliability gap: `WSCasesCovered` emits a single-connection connect
case (`ws-<svc>-<role>-connect`, Action `ws_connect`, Background true) for every
role. When a role is already connected by the deterministic relay Steps case
(the 3C detector connects the receiver + peers), this single-conn connect case
is redundant — and because single-action cases route through rule-engine →
Steer (they are not `Steps` cases), Steer drift makes them FAIL on real traffic
(dogfood: `ws-realtime-web-connect` / `ws-realtime-bridge-connect` failed ×3,
`matched=0 seen=0`, 404/403, connection_id drift to `web_client` /
`bridge_conn_1`). The relay Steps case (runSteps, deterministic) PASSED
attempts:1.

The deterministic Steps path is the reliable WS execution path. Single-action
`ws_*` cases inherit the long-standing Steer drift (Finding-3). This design
removes the redundant single-conn cases so they stop polluting the run —
without touching the executor or Steer.

## Goal

Stop the redundant single-connection WS connect case (and its dependent
receives) from being generated when the 3C deterministic relay case already
connects those roles. No executor change, no Steer change, no new deps.

## Design

Scope: `internal/head/scout/ws_cases.go` only.

### 1. `wsRelayCases` reports the roles it connects

`wsRelayCases` gains a third return value — `connectedRoles map[string]bool` —
naming every role a relay case opens a socket for: the receiver
(`Steps[0].Role`) and every peer (`Steps[1..].Role`). The existing `covered`
return (receiver → signal types, used to suppress the redundant single-conn
*receive* of a relayed signal) is unchanged.

```go
func wsRelayCases(svc project.Service) (cases []agent.TestCase,
    covered map[string]map[string]bool, connectedRoles map[string]bool)
```

### 2. `WSCasesCovered` skips the single-conn form for connected roles

In the role loop's no-exchange branch (where connect + decisive receives are
emitted), a role present in `connectedRoles` is skipped entirely:

```go
if ex, ok := wsExchangeFromGoal(goal); ok {   // goal exchange: kept (reliable Steps)
    cases = append(cases, wsStepsCase(svc, roleName, role, ex)); continue
}
if connectedRoles[roleName] {                  // NEW: 3C relay already connects this role
    continue                                   //       single-conn connect+receive form is redundant
}
connectID := wsCaseID(...); cases = append(connect case)
for _, typ := range wsDecisiveTypes(role, goal) {
    if relaySignals[typ] { continue }
    cases = append(receive case)
}
```

Why skip the whole connect+receive block (not just connect): a receive
`DependsOn` its role's connect. Suppressing connect while keeping a dependent
receive would emit an orphan case with a dangling `DependsOn`. Suppressing the
block keeps the `DependsOn` graph sound.

## Assumptions & Edge Cases

- **Relay-connected roles' single-conn receives are peer-gated.** A relay
  receiver (optional-handshake role) receives its signal only when a peer
  joins; on a lone connection it gets silence (dogfood: `matched=0 seen=0`).
  So suppressing the single-conn receive loses no achievable coverage. *If a
  future goal names a server-push, non-signal type that a receiver can get on a
  lone socket, this assumption breaks — that is a future precision refinement,
  out of scope here.*
- **goal-exchange (`wsStepsCase`) is preserved** for connected roles (the check
  precedes `connectedRoles`). A connected role with a send→receive exchange
  still gets its own `wsStepsCase` (a separate Steps case, runs via runSteps,
  opens its own connection under a distinct caseID). Correct: the exchange is
  an independent reliable validation, not redundant with the relay.
- **LLM-covered roles (A1 `ws_relay`) vs 3C-connected roles are independent.**
  LLM-covered roles still skip ALL forms (existing behavior); 3C-connected
  roles skip only the connect+receive form. The coexistence filter
  (`covered[svc.Name]`, deviation #2 in the SDD ledger) is unchanged.
- **The relay case itself is unchanged** — still generated (connect
  receiver+peers + receive signal) before the role loop.
- **<2 roles, or no optional-handshake role** → `wsRelayCases` returns empty
  `connectedRoles` → no suppression. Single-role and non-optional protocols are
  byte-identical to today (all existing `WSCases*` tests fall here and are
  untouched).

## Testing

- Unit: `wsRelayCases` returns `connectedRoles` containing the receiver + all
  peers (multi-peer, ≥3-role case included).
- Unit: `WSCasesCovered` with a 2-role optional-handshake protocol (the dogfood
  shape: web receiver + bridge peer) emits NO `ws-<svc>-web-connect` /
  `ws-<svc>-bridge-connect`; the relay case IS emitted; a non-connected third
  role (if any) still gets its connect.
- Regression: existing `WSCases*` tests unchanged (all single-role or
  non-optional multi-role — none trigger suppression).
  `WSCasesCovered(cfg,goal,nil)==WSCases(cfg,goal)` still holds.
- `wsRelayCases` signature update ripples to its direct call sites
  (`ws_cases.go` prod caller + `ws_cases_test.go` direct callers) — updated in
  lockstep.
- (Optional, live) re-run the dogfood `cerberus run`: confirm
  `ws-realtime-web-connect` / `ws-realtime-bridge-connect` disappear and the
  relay case still PASSES.

## Constraints

Go 1.25 pure-Go; coder/websocket v1.8.14 only (unchanged — scout-only change);
no expression/evaluator deps; author `binoctal <binoctal@gmail.com>`, no
Co-Authored-By; docs only in `cerberus-docs/`; `make check` green; multi-map
iteration stays sorted (unchanged — `connectedRoles` is membership-only; the
role loop already iterates sorted keys).

## Out of Scope

- Finding-3 (Steer WS drift on free-form `tc-*` cases) — separate scope.
- Single-conn `ws_*` deterministic execution (approach B) — larger executor
  change.
- Precision refinement: keep goal-named non-signal receives for connected roles
  (future, only if a goal needs it).
