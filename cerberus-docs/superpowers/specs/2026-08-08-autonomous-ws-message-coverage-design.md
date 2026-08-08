# Autonomous WS Message-Edge Coverage — Design

> Status: design for planning. Date: 2026-08-08.
> Predecessor: `cerberus-docs/superpowers/specs/2026-08-07-saas-coverage-authority-design.md`
> (which shipped honest-unmeasured + objective receive-driven path coverage).

## Problem (root cause)

The SaaS coverage authority change made `pathCoverage` receive-driven so a
real open-agents bridge→web relay (a server-pushed `device:online`) is correctly
attributed and measured >0 — proven live by
`TestPathCoverage_LiveOpenAgentsRelay`. But an **autonomous** `cerberus run`
against the `ws-realtime` dogfood still reports `coverage_pct=0`. Three
independent gaps combine to cause this:

1. **No deterministic case exercises a `message_handled` edge.** cerberus's WS
   case generators are `wsRelayCases` (connect-signals only — no message body)
   and `wsStepsCase` (single-role send→receive — but open-agents replies come
   from the *peer*, not the server, so a web-only flow never receives the
   reply). No generator produces a two-role active message exchange, which is
   the only thing that exercises a `message_handled` edge. (`device:online` is a
   connect-signal whose vocab `Trigger` is not `message_handled`, so it is not
   in the required surface by Decision 2 of the prior spec.)

2. **Provisioning cannot express this dogfood's auth.** The dev server requires
   `web` to present the static `demo_token` backdoor **and** a provisioned
   `userId` path segment (`/ws/<userId>`); `bridge` requires a provisioned
   `deviceToken` + `deviceId` query param sharing that same `userId`. cerberus's
   authflow couples an actor's token to its login response (`token_from`), so an
   actor cannot have a static token *and* a provisioned path param; and role
   discriminator params are literal-or-uuid only, so `deviceId` cannot be sourced
   from provisioning.

3. **The dogfood config does not declare a `bridge` role.** `ws_cases.go:210`
   skips relay/exchange generation unless `len(Protocol.Roles) >= 2`; the
   committed protocol declares only `web`. There is also no `/ws/{userId}` path
   template and no provisioning hook.

The measurement itself is correct: it reports 0 honestly (no `message_handled`
edge was exercised). This epic closes the three gaps so an autonomous run
actually exercises real message edges and reports an objective >0.

## Scope

Three coupled pieces, all in scope for this epic:

- **(A) Deterministic two-role request-response case generator** (scout) — the
  core new capability.
- **(B) Two small provisioning features** (cerberus runtime) — the enablers.
- **(C) ws-realtime dogfood rework** (config) — declares the bridge role,
  path template, provisioning hooks, and representative response mappings.

**Non-goal:** exhaustively declaring response mappings for all ~64 vocab edges
in the dogfood (the completeness suite remains the authority for full surface
coverage). The dogfood declares a representative subset sufficient to
demonstrate objective >0 autonomously.

**Non-goal:** general session-level shared provisioning for production
deployments. This epic relies on the dev server's deterministic `/api/dev/setup`
(returns the same `userId` per server session), documented as a dogfood
assumption.

## Decisions

### Decision 1 — Response mapping lives on the protocol role

A new optional field `ProtocolRole.Responses map[string]string`
(`received_type → reply_type`) declares how a role responds when its test
driver observes a type, e.g. `bridge.responses: {session:start: session:created}`.

**Why the protocol (not the vocab, not the goal):**

- The protocol file is already the "how cerberus drives each role at test time"
  contract (roles, auth, handshake, discriminator params). Response scripts are
  the same class of test-driving concern, so this is their natural home.
- It is deterministic and **does not touch the vocab or its source extractor**.
  The vocab stays a pure source-derived "what messages exist" surface; inferring
  request/response pairings from `room.ts` source would be unreliable extractor
  work and would couple test semantics into the shared completeness surface.
- It generalizes: any role can be a responder.

Rejected alternatives: vocab edge `response` field (requires hard, unreliable
extractor changes; pollutes the source-derived surface); goal-text inference
(`wsExchangeFromGoal` is single-exchange-per-run — cannot cover a surface
autonomously, and brittle NL parsing).

### Decision 2 — Provisioning via dev-server determinism + two small features

The dev server's `POST /api/dev/setup` returns the same `userId` on every call
within a server session, so `web-actor` and `bridge-actor` each provisioning
independently receive the same `userId` — no session-shared provisioning store
is needed. Two small cerberus features make the dogfood's auth expressible:

- **B1 — provisioning-only authflow.** `AuthFlow.TokenFrom` becomes optional.
  When empty, the actor's static `Credentials.Token` is used as the token, but
  login still runs to capture `PathParams`. This lets `web-actor` use static
  `demo_token` while still provisioning its `userId` path param.
- **B2 — role-param templating from captured path params.** Role discriminator
  param values may carry `{name}` placeholders resolved from the resolved
  actor's captured `PathParams` (alongside the existing uuid sentinel). This
  lets `bridge` inject `deviceId: "{deviceId}"`.

### Decision 3 — A new generator, `wsRequestResponseCases`, emits two-role exchanges

For each role `R` with a non-empty `Responses`, for each `(T → T')` pair, the
generator locates the request edge `(From→R, T)` in the vocab and emits one
case (the reply is driven back to the requester, so the reply edge exercised is
`(R→From, T')`):

1. requester `ws_connect` (role `From`)
2. `R` `ws_connect`
3. requester `ws_send` `{"type":"T"}`
4. `R` `ws_receive` `T`
5. `R` `ws_send` `{"type":"T'"}`
6. requester `ws_receive` `T'`

Step 4 exercises edge `(From→R, T)` and step 6 exercises edge `(R→From, T')` —
both counted by the existing receive-driven `pathCoverage` (a matched `ws_receive`
of a type by a role attributes the declared edge). The reply edge `(R→From, T')`
need not be declared in the vocab for the request edge to be exercised; if it is
declared, it is counted too. The generator is wired into `WSCasesCovered`
alongside `wsRelayCases`/`wsStepsCase`, participating in the same covered-role
dedup.

## Architecture

```
Scout case-gen (A)              dogfood config (C)               cerberus runtime (B)
wsRequestResponseCases  ◀──  protocol: bridge.responses     provisioning-only authflow (B1)
  web send T                 project.yaml: /ws/{userId}      role-param {name} templating (B2)
  bridge recv T → send T'    web=demo_token+provision            │
  web recv T'                bridge=provision+deviceToken        │
       │                                                          ▼
       └─────────────── evidence → pathCoverage (receive-driven) → objective >0
```

## Components & Changes

- `internal/project/protocol_schema.go` — add `Responses map[string]string` to
  `ProtocolRole` (`yaml:"responses,omitempty"`).
- `internal/project/validate_protocol.go` — validate `Responses` keys/vals are
  non-empty type tokens; a response type with no matching reply edge in the vocab
  is a warning, not an error (the case still exercises the request edge).
- `internal/head/scout/ws_cases.go` — new `wsRequestResponseCases(svc)`
  generator + a covered-role return so `WSCasesCovered` dedups; pure, no LLM.
- `internal/head/agent/authflow.go` — B1: when `TokenFrom == ""`, set the
  result token from `actor.Credentials.Token` but still run login and capture
  `PathParams`.
- `internal/head/agent/websocket.go` — B2: `resolveRoleParamValue` resolves
  `{name}` from the resolved actor's `PathParams` (composed with the uuid
  sentinel).
- `dogfood/ws-realtime/.cerberus/protocols/open-agents.yaml` — add `bridge`
  role (`credential_ref: bridge-actor`, `params: {type: bridge, deviceId:
  "{deviceId}"}`, `responses: {session:start: session:created, …}`); keep
  `web` role + its optional `device:online` handshake.
- `dogfood/ws-realtime/.cerberus/project.yaml` — service URL
  `http://localhost:8989/ws/{userId}`; `web-actor` (static `demo_token` +
  provisioning-only authflow `/api/dev/setup` capturing `userId`); `bridge-actor`
  (authflow `/api/dev/setup`, `token_from: config.deviceToken`, `path_params:
  {userId: config.userId, deviceId: config.deviceId}`).

## Testing

- **Unit (A):** `wsRequestResponseCases` over a fixture protocol+vocab emits the
  exact two-role step sequence for each response pair, requests both edges, and
  dedups covered roles; no cases when `Responses` is empty or `len(Roles) < 2`.
- **Unit (B):** B1 — authflow with empty `TokenFrom` uses the static token and
  still populates `PathParams`; B2 — `{deviceId}` resolves from the actor's
  captured path params, uuid sentinel still works.
- **Integration (live):** extend the `//go:build integration` suite — a two-role
  request-response case against live open-agents exercises a `message_handled`
  edge; assert `pathCoverage > 0` over the real evidence.
- **Live autonomous:** `cerberus run` against ws-realtime reports
  `path_coverage > 0` with ≥1 real `message_handled` edge exercised and an
  accurate gap list (no LLM in the measurement).
- **Zero regression:** protocols without `Responses`/provisioning produce
  byte-identical case generation and auth resolution.

## Success Criteria

- An autonomous `cerberus run` against the ws-realtime dogfood reports an
  objective message-edge coverage fraction > 0 and a concrete, accurate gap
  list — no `message_handled` edge fabricated or missed.
- The new generator + provisioning features are unit- and live-verified.
- Local-codebase sessions and existing no-vocab / no-responses protocols are
  unaffected (zero regression).

## Risks

- **Hand-authored `responses` may drift** from the real protocol's reply
  behavior. Accepted: any test fixture has this property; the completeness suite
  remains the surface authority.
- **Dev-server determinism is a dogfood assumption.** `/api/dev/setup` returning
  a stable `userId` per session holds for local wrangler dev; a production
  deployment would need session-shared provisioning (deferred, documented as a
  known limitation).
- **B2 templating vs uuid sentinel.** `{name}` resolution and the existing uuid
  sentinel must compose without ambiguity (a literal `{...}` is only treated as a
  placeholder when `name` is a captured path param; otherwise left literal).
