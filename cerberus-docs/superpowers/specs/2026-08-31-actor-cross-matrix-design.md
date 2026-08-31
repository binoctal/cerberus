# Actor Cross-Matrix v1 — Read-Only IDOR Tier

Date: 2026-08-31
Status: approved design (brainstorm 2026-08-31), awaiting implementation plan
Sub-project of: Scout depth-gap roadmap (see memory: scout-depth-gap-diagnosis)
Roadmap position: A (cheapest) of A actor-cross-matrix / B vocab semantic
annotation / C constrained LLM experiment design / D coverage depth dimensions.

## Problem

The breadth machine has zero horizontal-isolation coverage. The sweep runs a
single web principal, so "other user's JWT touches my resource id" is never
probed: the sentinel DELETE tier proves nonexistent-id 404, not other-user
403. This is blind spot #1 (single-user world) of the 2026-08-31 four-factor
diagnosis — the vocab records shape, not ownership semantics, and nothing in
the accident-driven pattern library ever fired here because no run produced
this kind of incident.

## Goal

For every per-user read resource, prove that a second authenticated principal
of the same role is REJECTED (4xx) when reading the first principal's
resource id. A 200 is a real IDOR finding and must fail the case.

## Non-Goals

- Write-method cross cases (POST/PUT/DELETE with a rival JWT). Deferred: a
  genuinely vulnerable SUT would let the rival mutate real owner data —
  v1 proves the finding class without the destruction.
- Admin/web cross pairs, role escalation, or N>2 principals.
- Any LLM in the generation path (the zero-LLM generation discipline stands;
  the rival principal is declarative, the cases are deterministic).
- Coverage-denominator changes (cross cases ride existing edges).

## Design

### 1. Case shape (generation layer, `internal/head/scout/http_route_cases.go`)

A route qualifies for the `-crossuser` tier when ALL hold:

- vocab route, not partial/unsupported, `auth: required`
- method GET, path carries at least one `:param`
- every `:param` has a param source whose list route is param-free
  (same `paramResolvable` predicate as the authed tier)
- the param-source list route resolves (via `roleForRoute`) to the web role
  or to no mapping — i.e. the chained id is per-user data, not admin-carried
- `cross_exempt` is NOT set on the route

Emit after the authed tier:

- capture steps 1..n: identical to the authed tier (owner = web role JWT,
  GET the list route, capture the id, assert 2xx)
- target step: same URL template with the captured id, `AuthRole: web-rival`,
  GET, `ExpectStatusClass: "4xx"`
- expectation text: `cross-user isolation: rival principal reading another
  user's resource is rejected (4xx) — horizontal access control holds`
- case ID: `routeBaseID(...) + "-crossuser"`

Empty owner lists inherit the existing ErrEmptyListCapture skip path — no
false reds from a barren dev database.

Current vocab yields 7 candidates: sessions, agents, skills, permissions,
missions, teams, external-agents (each `GET .../:id` chained to its list).

### 2. Rival principal (declaration layer, zero executor changes)

`AuthRole` resolves through `proto.Roles[role].CredentialRef` to exactly one
actor, so a second principal is purely declarative:

- `dogfood/realtime-e2e/.cerberus/protocols/open-agents.yaml` — add role
  `web-rival` with `credential_ref: web-rival-actor`; params/handshake copied
  from the web role (same SUT role, different principal).
- `dogfood/realtime-e2e/.cerberus/project.yaml` — add actor `web-rival-actor`
  with `rival@openagents.local` / two-step auth identical to web-actor
  (`/api/dev/setup` provision then `/api/dev/login` JWT), **with `plan: pro`**.
  Rationale: a free-plan rival would conflate plan-gate 403 with isolation
  403 — the tier must isolate exactly one variable.

The vocab HTTP role map (`http_role_routes`) is untouched: `web-rival` never
routes by prefix; only the crossuser generator names it explicitly.

Trade-off accepted: `web-rival` is not a SUT role but a second principal of
one — mildly abusing role semantics in exchange for zero changes to the
executor, claims gate, and coverage attribution, which are all role-keyed.
If the matrix later needs N principals or role-pair crossings, upgrade to a
TestStep-level actor override (rejected as approach B for v1 blast radius).

### 3. Exemption (judgment layer, regen-preserved)

- `VocabHTTPRoute` gains `CrossExempt bool` (`yaml:"cross_exempt,omitempty"`).
- The generator skips exempt routes; everything else stays as derived.
- `cmd/cerberus/main_protocol.go` vocab regen merge (the judgment layer,
  keyed `method|path`) preserves `cross_exempt` like `min_query`,
  `param_sources`, and hand-set `auth`: it is live-probe knowledge
  (is the resource genuinely shared?), not source-derivable fact.
- First expected exemption: `GET /api/teams/:id` — team-scoped membership is
  plausibly legitimate cross-principal access; exempt on evidence after the
  first dogfood run rather than by guess now.

### 4. Coverage and judging

Cross cases ride existing edges — the coverage denominator does not move.
The docket grows by the candidate count (7 today; budget headroom is ample).
Judging uses the existing Examiner flow with no special-casing.

## Testing

- Unit (`internal/head/scout/http_route_cases_test.go`): tier emission —
  candidate routes emit `-crossuser` with rival target step asserting 4xx;
  `cross_exempt` routes emit nothing; non-GET / param-free / admin-sourced
  routes emit nothing; capture steps are byte-identical to the authed tier.
- Validation: vocab round-trip preserves `cross_exempt` (mirror the
  min_query regen tests).
- Dogfood acceptance (run40): all crossuser cases green with 4xx — OR any
  200 files a real IDOR finding against open-agents. Either outcome is the
  tier working; the run decides which.

## Failure Modes Considered

- Rival setup 404s (endpoint churn like run38's d6e5390): the auth flow
  degrades the rival to unauthenticated, the target step fails with
  "no http token" — visible, not silent; the launcher HEAD pin covers the
  known cause.
- Shared-but-unexempted routes go red on the first run: that is the
  exemption list being populated by evidence, exactly the min_query path.
- Owner data missing mid-run (another delete-account-style wipe): capture
  skips, case skips — no false red.
