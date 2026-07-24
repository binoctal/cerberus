# WS Scout Relay Generation — Design

Status: Design (autonomous; chosen 2026-07-24 as the follow-up to F1).
Trigger: F1 proved + dogfooed multi-connection orchestration, but the multi-connection
`Steps` case was HAND-AUTHORED. Scout relay generation lets `cerberus run`
auto-generate a multi-connection relay `Steps` case from goal + protocol, so a
web↔bridge relay is testable without a hand-authored case.

## Goal

Scout auto-generates a multi-connection relay `Steps` case (connect a `web` and a
`bridge` role to the same endpoint, send/receive across them) from the goal +
declared protocol, validated against the open-agents relay. The executor already
runs such a case (F1, zero prod change); this adds the generation.

## Approach (resolved forks)

- **A1 — plan-time LLM emits the relay intent; `runSteps` executes deterministically.**
  The Scout planning LLM understands the relay topology + type-transform (which the
  F1 dogfood confirmed is genuinely protocol-specific, LLM work — not derivable
  from a light deterministic schema). Execution stays deterministic (Phase 0,
  `runSteps`), sidestepping the Steer runtime drift C worked around.
- **Intent + deterministic assembly.** The LLM emits a compact relay *intent*
  (`ws_relay` case); a new deterministic Scout *expander* assembles the canonical
  multi-connection `Steps` case from it. Structure is deterministic (C-aligned);
  the LLM supplies only protocol understanding.

This keeps the hard part (relay comprehension) in the LLM and the structure
(connect order, step shape) in deterministic code.

## Architecture

The Scout planning LLM already emits `TestCase` objects (id/name/service/action/
body/target/...). This design adds ONE new informational action, `ws_relay`, whose
`body` carries the relay intent. A new deterministic expander in `augmentPlan`
scans the LLM's cases, expands each `ws_relay` into a multi-connection `Steps`
case, and replaces it. `runSteps` (unchanged) executes the `Steps` deterministically
on real connections (the F1 capability).

```
LLM plan → TestCase{action:"ws_relay", service, body:<intent>}
                 │
                 ▼  augmentPlan → expandWSRelayCases (NEW, deterministic)
          TestCase{Steps:[ws_connect ×roles, ws_send/ws_receive ×intent], target}
                 │
                 ▼  runSteps (unchanged, F1 multi-connection execution)
          real web + bridge sockets, relay observed
```

**No `TestCase`/`TestStep` schema change** — `Steps` already exists; `ws_relay` is
just a new action value the prompt teaches and the expander recognizes.

## The intent (`ws_relay` case body)

The LLM emits a `ws_relay` case with `service` set and a JSON `body`:

```json
{
  "roles": ["web", "bridge"],
  "steps": [
    {"do": "send",    "role": "web", "type": "session:start"},
    {"do": "receive", "role": "web", "type": "session:created", "assert": {"payload.ready": true}}
  ]
}
```

- `roles`: every role to connect, **in connect order** (see Connect order). Each
  must be a role declared by `service`'s protocol.
- `steps`: ordered; `do` ∈ `{send, receive}`; `role` names which connection
  (must be in `roles`); `type` is the message routing type; a `receive` step may
  carry `assert` (dotted-path → value, passed through to the step's `Asserts`;
  LLM JSON values are already typed, so numeric normalization in `valueEqual`
  still applies).

`device:online` peer-join example (no send): `roles:["web","bridge"]`,
`steps:[{"do":"receive","role":"web","type":"device:online"}]`.

## The expander (`expandWSRelayCases`, deterministic, in scout)

Called from `augmentPlan` (which already has the plan + config). For each
`ws_relay` case in `plan.Cases`:

1. **Resolve service + protocol.** Look up `case.Service` in `s.config.Services`;
   require it declares a `Protocol` with `Roles`. (R1)
2. **Validate the intent.** (R2, R5)
   - `roles` has ≥ 2 entries (else it is a single-connection flow — drop with a
     log, the LLM should have used the existing path).
   - every `role` in `roles` and in `steps` is a declared role of that ONE
     protocol (roles spanning services are rejected).
   - every step has `do` ∈ `{send,receive}`, a non-empty `type`, and a `role` in
     `roles`.
   - On any validation failure: drop the case and log (never fail the run).
3. **Assemble the `Steps` case** (deterministic):
   - one `ws_connect` per role, **in `roles` order**, with `ConnectionID = role`
     and `Role = role`.
   - the `steps` in order: `send` → `ws_send` (`ConnectionID=role`,
     `Message={"type":<type>}`); `receive` → `ws_receive` (`ConnectionID=role`,
     `Type=type`, `Asserts=assert`, a receive timeout from the role's handshake
     timeout if declared, else the executor default).
   - `Target = service.URL`; `Service = case.Service`; keep the LLM's `Name`/
     `Expectation`.
   - deterministic ID: `ws-<service>-relay-<sorted-roles>` (mirrors `wsCaseID`).
4. **Replace** the `ws_relay` case in `plan.Cases` with the assembled `Steps` case.
5. **Dedupe (R7).** Record the roles covered by an expanded relay; after expansion,
   suppress `WSCases`' connect (and connect-only receive) cases for those roles so
   the run does not redundantly re-connect them. (The LLM-emitted `ws_relay` and
   the deterministic `WSCases` otherwise overlap on connects.)

The expander is a pure function of (plan, config) — fully unit-testable without an
LLM. `WSCases` itself is unchanged (the dedupe is the expander recording covered
roles for `appendExecutorCases` to skip).

## Connect order (R3)

The relay's peer-join signal (open-agents' `device:online`) is pushed by the DO to
an already-connected `web` when `bridge` joins. Connect order therefore matters:
the signal receiver connects first. **`roles` is ordered; the assembler connects in
that order.** The prompt instructs the LLM to list the signal receiver first. (The
read pump makes a receiver resilient to a slightly-early frame, but the peer-join
signal only fires on the joiner's connect, so the receiver must already be
connected.)

## Prompt

`promptPlanSystem` gains one WS-awareness bullet: when the goal describes a relay
(multiple roles exchanging messages through a broker — e.g. "web sends X and
receives the relayed Y while bridge is connected"), emit ONE `ws_relay` case per
such exchange with `service`, ordered `roles` (signal receiver first), and the
ordered `steps` (send/receive + optional assert). Do not also emit single-role
`ws_connect`/`ws_receive` cases for roles covered by the relay (the expander
connects them). Raw-string inline edit (no backticks/`${}`).

## Provisioning dependency (R4 — documented, not a blocker)

A real `cerberus run` of an open-agents relay needs the bridge `deviceToken` and
the `/ws/{userId}` path provisioned in advance: the token stored as the bridge
actor's credential, and the userId baked into `svc.URL` (F3 dynamic URL is still
deferred). Scout generation produces only the intent + `Steps`; credentials are the
user's responsibility. The F1 integration test POSTs `/api/dev/setup` at runtime
for self-containment — that is a test convenience, not a requirement `cerberus run`
must replicate.

## Constraints

- Go 1.25, pure-Go (no CGo); `coder/websocket v1.8.14` ONLY.
- No new dependencies; no expression evaluator; no `TestCase`/`TestStep`/protocol
  schema change (`ws_relay` is a new action value + an expander; `Steps` exists).
- Production change is confined to: the new scout expander + its `augmentPlan`
  wiring + the dedupe hook in `appendExecutorCases` + one prompt bullet + docs.
  The executor, `runSteps`, `stepToAction`, `WebSocketExecutor`, and the protocol
  schema are unchanged.
- Commit author `binoctal <binoctal@gmail.com>`; no `Co-Authored-By`; English;
  docs only in `cerberus-docs/`; `make check` green.
- Determinism: any map iteration in the expander's output is sorted
  (`slices.Sorted(maps.Keys(...))`); the assembled `Steps` order is the intent's
  order (not map-derived).

## Testing

- **Expander unit tests** (no LLM): intent → assembled `Steps` (field-by-field);
  validation drops: <2 roles, unknown role, role spanning services, missing/empty
  type, bad `do`; dedupe suppresses `WSCases` connects for covered roles;
  deterministic ID; connect order == `roles` order; assert pass-through.
- **WSCases regression:** non-`ws_relay` plans byte-identical (the expander is a
  no-op when no `ws_relay` cases are present).
- **Existing WS + Steps tests stay green** (executor/runSteps untouched).
- **Dogfood (best-effort, `cerberus run`-style or a Scout-level test):** against
  open-agents, confirm the LLM emits a well-formed `ws_relay` intent for the
  `device:online` relay and the expanded `Steps` connects web+bridge and matches
  the relayed signal. LLM output quality is the A1 risk (R8); the deterministic
  expander is the testable core. If the LLM emits a malformed intent, the
  expander drops it + logs (graceful), and the hand-authored F1 path still works.

## Non-goals

- F3 dynamic URL/path-param injection (userId stays baked in `svc.URL`).
- F4 message-batching / type-alias matching.
- Steer runtime multi-connection orchestration.
- Auto-provisioning of bridge credentials at run time (the provisioning dependency
  is the user's setup).
- A protocol-schema "relay topology" block (the LLM derives topology from goal +
  roles; a richer schema is M3-3-adjacent and explicitly not added here).

## Open questions (resolve in the plan)

1. Dedupe granularity: suppress only `WSCases` connect cases for covered roles, or
   also connect-only receive cases? Lean: suppress connect cases (the relay
   supersedes bare-connect coverage for those roles); leave receive cases unless
   they assert a type the relay also asserts.
2. Receive-timeout source for expanded `ws_receive` steps: the role's
   `Handshake.Timeout` (like `wsStepsCase`) or a fixed default? Lean: role
   handshake timeout if declared, else executor default (10 s).
3. Whether the expander should also emit a `ws_disconnect` step per role at the end
   (cleanup). Lean: no — case-context cancellation closes all connections (the
   existing cleanup path), as in `wsStepsCase`.
