# Relay Coverage Generator — Design

> Status: design for planning. Date: 2026-08-13. Revised after self-review.
> Predecessors: `2026-08-07-saas-coverage-authority-design.md` (three-authority model),
> `2026-08-11-http-push-coverage-attribution-design.md` (receive-driven attribution).

## Context

A live autonomous `cerberus run` against the ws-realtime dogfood reports
`coverage_pct ≈ 0.05 (3-4/64)`; the deterministic integration suite
(`TestPathCoverage_LiveOpenAgentsRelay`) reports `≈ 0.50 (32/64)`. The gap
analysis asked: **why only 32 of 64?**

`requiredEdges` (internal/session/coverage.go:283) returns 64 edges = 63
`message_handled` vocab edges (filter: `Trigger=="message_handled" &&
!Unsupported && !Partial`) + 1 synthesized `http_trigger` edge. The 63
message_handled edges are **bidirectional role→role relays**: `bridge→web` (39),
`web→bridge` (24), `web→web` (1, excluded below). Their entry point is
`<From> send T → server relay → <To> receive T` (room.ts is a generic relay).

## Problem (root cause + a tension)

**Root cause — generator blind spot.** The deterministic generators in
`internal/head/scout/ws_cases.go` do not enumerate declared message_handled
edges. Coverage is incidental:

- `wsRelayCases` — only peer-join signals for roles with an optional handshake.
- `wsRequestResponseCases` — only edges whose role declares a `Responses` map.
- `wsHTTPTriggerCases` — only declared `http_trigger`s.
- `wsFlowConnectCase` — receives only `wsDecisiveTypes(role, goal)`, which returns
  **handshake await_type + types named in the goal string** — not the vocab's
  declared types.

A declared `(From→To, T)` edge with `Trigger=message_handled` is credited only
if it happens to be a handshake signal, a response, an http_trigger, or
goal-named. The remaining ~31 declared edges **never appear in any case's
`ws_receive` step**, so receive-driven attribution can never credit them.

**Tension — escalation.** Some message_handled types may be server-only (the
server emits them on internal events, not on a client send). A case that sends
such a type times out and fails. The executor escalates on consecutive fails:
**`systemic_failure` at 5 consecutive fails**, **`target_unreachable` at 3
consecutive timeouts** (escalation_checks.go:53,69). Escalation interrupts the
run. So Phase 1 cannot blindly emit all 63 cases into the autonomous `WSCases`
path — a cluster of server-only fails would abort the run and *lower* coverage.
This is the chicken-and-egg: to safely generate cases we must know which types
are server-only, but learning that requires running cases.

## Coverage attribution is receive-driven and verdict-decoupled

A key fact for implementers: `exercisedEdges` (coverage.go:192) credits an edge
solely from a **positive matched receive in `StepResult.Evidence`**, keyed by
`(ToRole, Type)`. It does **not** consult case `Status` (Passed/Failed) or any
`Decisive` flag. `TestStep` has no `Decisive` field; decisiveness affects only
case verdict accounting, not coverage. Therefore the generator's goal is to
**produce a matched receive**; case pass/fall is a downstream concern (and the
escalation trigger we must manage).

## Design — three sequenced phases

The generator logic is uniform; the phases differ in *where* it runs and *what*
the denominator contains. The dependency order is **1a → 2 → 1b**.

### Phase 1a — Probe in the integration suite only

Implement `wsRelayCoverageCases(svc)` and call it **only from the
`//go:build integration` test** (`TestPathCoverage_LiveOpenAgentsRelay` and
kin), not yet from `WSCases`. The integration suite is a developer-driven
probe where escalation/fail is observable and acceptable; the autonomous run is
untouched and stays at its current behavior.

**Edge set** — exactly the `requiredEdges` message_handled set (1:1 with the
denominator):

```
filter: e.Trigger == "message_handled" && !e.Unsupported && !e.Partial && e.FromRole != e.ToRole
```

(`FromRole != ToRole` drops the 1 self-relay edge; `FromRole` is never empty
for message_handled edges — verified: 39/24/1 split, no empty-from.)

**Case shape** — 4-step relay (request-response without a reply expectation):

```
ws_connect(ConnectionID=From, Role=From)
ws_connect(ConnectionID=To,   Role=To)
ws_send(ConnectionID=From,    Message=wsSendBody(T, RequestPayload[T]))
ws_receive(ConnectionID=To,   Type=T, Timeout=N)   // produces the evidence
```

`wsSendBody(T, payload)` reuses the existing helper; payload is the sending
role's `RequestPayload[T]` when declared, else `nil`, matching
`wsRequestResponseCases`. Only this `ws_receive` can credit the edge.

**Dedup** — one case per unique `(From, To, T)`.

**Output of 1a** — the per-case pass/timeout result is the
client-triggerability classification: pass ⇒ client-triggerable; timeout ⇒
server-only candidate. This feeds Phase 2.

### Phase 2 — Mark server-only types (denominator honesty)

From Phase 1a's stable timeout set (timeout across repeated integration runs,
not flaky), mark those vocab edges — e.g. set `Partial` (or a new
`ClientUntriggerable` flag if `Partial`'s semantics conflict) so:
- `requiredEdges` excludes them from the denominator, and
- `wsRelayCoverageCases` skips them (the filter above already excludes
  `Partial`).

If Phase 1a shows every type client-triggerable, Phase 2 is a no-op and the
denominator stays 64. Phase 2 is data-gated on 1a; its exact marker is decided
when 1a's data is in.

### Phase 1b — Wire into `WSCases` (safe autonomous coverage)

After Phase 2 has marked server-only types, wire `wsRelayCoverageCases` into
`wsCasesForService` alongside the existing generators. Because server-only
types are now `Partial`/excluded, the generator emits only client-triggerable
cases — which pass and produce matched receives — so the autonomous run gains
coverage **without triggering escalation**.

**Coexistence** — reuse the existing discipline in `wsCasesForService`:
- Skip an edge already emitted by `wsRelayCases`/`wsRequestResponseCases`/
  `wsHTTPTriggerCases` (their cases already connect its roles and receive its
  type).
- Record the roles these cases connect so the per-role `wsFlowConnectCase`
  loop skips them (same pattern as `rrConnected`), avoiding redundant sockets.

## Interfaces

- **Consumes**: `project.Service` (`.Vocabulary.Edges`, `.Protocol.Roles`,
  role `RequestPayload`), `project.VocabEdge` (`FromRole`, `ToRole`, `Type`,
  `Trigger`, `Unsupported`, `Partial`), existing `wsSendBody`/`wsCaseID`/
  `sanitizeTypeID` helpers.
- **Produces**: `[]agent.TestCase` (4-step `ws_flow` cases). Phase 1a: consumed
  by the integration test. Phase 1b: wired into `wsCasesForService` →
  `WSCases`/`WSCasesCovered`.
- **Does not touch** (Phase 1): `requiredEdges`, `exercisedEdges`, the
  attribution rule, existing generators' logic, executor escalation. Phase 2
  touches only vocab edge flags (and thus `requiredEdges` via its existing
  filter).

## Constraints / risks

- **Zero regression**: a service whose message_handled edges are already fully
  covered by existing generators must emit nothing new after coexistence dedup.
  `go test ./...` and the existing `TestPathCoverage_*` stay green.
- **Escalation safety (the core constraint)**: Phase 1a runs only in the
  integration suite (escalation-tolerant). Phase 1b lands only after Phase 2
  excludes server-only types, so the autonomous path never sees a run of
  server-only fails. Violating the 1a→2→1b order reintroduces the abort risk.
- **Room.ts relay behavior is empirical**: Phase 1a exists precisely to measure
  it. Timeouts are honest signal, not bugs.
- **Phase 2 maintenance**: if room.ts changes which types it relays, Phase 1a
  must be re-run and Phase 2 markers updated. This is inherent to a
  runtime-derived denominator.
- **Case population**: up to ~60 new cases in the integration suite; fast and
  bounded. Autonomous run (Phase 1b) gains only the triggerable subset.
- **Pure, deterministic, no LLM**: unit-testable without a live server.

## Testing strategy

- **Unit test** (scout package): for a synthetic service with K message_handled
  edges (mix of relay-covered, reqresp-covered, uncovered, and one self-relay),
  assert the generator emits exactly the uncovered non-self edges as 4-step
  cases; assert edges already covered by the other generators are not
  duplicated; assert self-relay and duplicate `(From,To,T)` are skipped; assert
  `Partial`/`Unsupported` edges are skipped.
- **Coexistence test**: through `wsCasesForService`, assert total connect count
  does not double-connect a role already connected by another generator.
- **Integration test** (Phase 1a, //go:build integration, live open-agents
  :8989): run the suite, record per-case pass/timeout, assert the suite
  `coverage_pct` rises above 0.50, capture the server-only candidate set.
- **Regression**: `go test ./...` green; existing `TestPathCoverage_*`
  unchanged.

## Out of scope

- Phase 2's exact marker (`Partial` vs new flag) — decided from 1a data.
- Autonomous-run Scout coverage — orthogonal, non-repeatable, not a lever.
- Attribution-rule changes — receive-driven attribution is correct as-is.
- `fetch_branch`/`disconnect_branch`/`flushBatch` edges — already excluded from
  `requiredEdges`.
