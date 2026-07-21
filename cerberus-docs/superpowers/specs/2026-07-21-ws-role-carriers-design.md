# WebSocket Realtime Engine (M2) — Role Discriminator Carriers (Design)

**Date:** 2026-07-21
**Status:** Design (brainstormed; pending spec review)
**Scope:** `internal/project/` (`ProtocolRole` schema + validation), `internal/head/agent/` (websocket executor: role header/subprotocol strip-then-inject), `cerberus-docs/executors/websocket.md`, `internal/head/agent/prompts.go`
**Depends on:** M0, M1, M2-roles (`...-m2-roles-design.md`)
**Proposal:** `cerberus-docs/superpowers/specs/2026-07-20-ws-realtime-engine-m2-proposal.md`

## Background & Motivation

M2-roles lets a service declare named connection types under `protocol.roles`.
A role bundles three per-connection facts the executor owns: the credential
(`credential_ref`), discriminator **query params** (`params`), and an optional
mandatory `handshake`. The LLM only names the role on `ws_connect`; the executor
expands the bundle.

The discriminator carrier is hardcoded to **the url query string**. `doConnect`
runs `for k, v := range role.Params { dialURL = setQueryParam(dialURL, k, v) }`
(`websocket.go:183-184`) — every role discriminator becomes a `?k=v` query param.

That covers open-agents (`?type=web` / `?type=bridge`) but not protocols that
discriminate the connection type another way:

- **Subprotocol negotiation** — the client offers
  `Sec-WebSocket-Protocol: web` (or `bridge`, or a versioned `bridge.v1`) and the
  server treats the negotiated subprotocol as the connection type. This is the
  standard WS way to agree on a protocol/role at handshake time.
- **A handshake header** — e.g. `X-Client-Type: web` / `X-Device-Role: bridge`
  set on the upgrade request.

A role that discriminates via subprotocol or header cannot be expressed today;
the case author is forced back to LLM-supplied `subprotocols`/`headers` on
`ws_connect`, losing the determinism and secret-hygiene guarantees the role
declaration exists to provide.

## Goal

Let a role declare discriminator facts on **all three carriers** the executor
already knows (query, header, subprotocol), with the same strip-then-inject
semantics `params` (query) and `auth` already use. The executor expands them; the
LLM still only names the role.

Success criteria:

- A role may declare `headers` (map) and `subprotocols` (list) alongside the
  existing `params` (query map).
- `doConnect` strip-then-injects each carrier: a role header deletes any
  LLM-supplied value at that key then sets the role's; a role subprotocol removes
  any LLM-supplied entry at that name then appends it. Exactly the role's values
  reach the server.
- Validation rejects a role occupying the auth token slot on ANY carrier: a
  `headers` key equal to `auth.param` when `auth.strategy=header`; a `subprotocols`
  entry equal to `auth.param` when `auth.strategy=subprotocol`; (existing) a
  `params` key equal to `auth.param` when `auth.strategy=query`.
- A role with no `headers`/`subprotocols` behaves exactly as M2-roles (query-only
  params). No role → M1. Byte-identical fallback.
- Secret hygiene unchanged: headers/subprotocols never appear in `WSResult.URL`
  (only query params touch the url); the pre-injection url contract is preserved.
- `make check` (fmt + lint + test -race) green; table-driven tests mirroring
  `internal/head/agent/websocket_test.go` and
  `internal/project/validate_protocol_test.go`.

## Non-Goals

- **Per-param carrier declaration** (each key naming its own carrier) —
  over-granular and a complex schema. A role discriminates via a small fixed set
  of slots; parallel collections per carrier (this design) cover every realistic
  shape. See D1.
- **Per-role single-carrier `param_strategy`** — rejected; it cannot express a
  role that discriminates via more than one carrier (e.g. a query `type` plus a
  subprotocol `v1`), which is legitimate. See D1.
- **Changing `WSConnectAction`** — role expansion stays executor-side; the LLM
  still only names the role. No new action field (mirrors M2-roles).
- **Order-sensitive subprotocol negotiation** — the server picks a negotiated
  subprotocol from the offered list per RFC 6455; cerberus does not model the
  server's choice. The role simply offers its discriminator subprotocol name(s).
- **Auto-inference of the carrier** (Scout picking query/header/subprotocol from
  docs/captures) — M3.

## Design Decisions

### D1 — Parallel collections per carrier (`params`/`headers`/`subprotocols`)

`ProtocolRole` gains two optional fields alongside the existing `params`:

```go
type ProtocolRole struct {
    CredentialRef string            `yaml:"credential_ref"`
    Params        map[string]string `yaml:"params,omitempty"`        // query (existing)
    Headers       map[string]string `yaml:"headers,omitempty"`       // dial headers (new)
    Subprotocols  []string          `yaml:"subprotocols,omitempty"`  // offered subprotocols (new)
    Handshake     *RoleHandshake    `yaml:"handshake,omitempty"`
}
```

- `params` stays **query-only** (backward compatible — every existing M2-roles
  declaration is unchanged). No semantic shift: a key here was and remains a
  query param.
- `headers` is a map (header name → value), strip-then-injected into the dial
  headers — same shape and semantics as `params` and as `auth`'s header strategy.
- `subprotocols` is a **list**, not a map: WS subprotocols are offered by name
  during the handshake (RFC 6455 `Sec-WebSocket-Protocol`), not as key/value
  pairs. A list of names is the natural shape and matches
  `WSConnectAction.Subprotocols []string`.

**Rejected — per-role `param_strategy: query|header|subprotocol` (single carrier
for all of a role's discriminators).** Mirrors `auth.strategy` and is marginally
simpler, but a role can legitimately need more than one slot (a query `type`
**and** a subprotocol version). A single-carrier field cannot express that
without awkward workarounds. The parallel-collections shape covers single-carrier
roles (just fill one collection) AND multi-carrier roles, at the cost of two
optional fields. The flexibility is cheap and the realistic topologies need it.

**Rejected — per-param carrier (each key/value names its own carrier).** Maximum
flexibility but a complex schema (a union type or a carrier tag per entry) for
no real gain: a role's discriminators are a small fixed set across three known
slots. YAGNI.

### D2 — Strip-then-inject on every carrier (consistency with params + auth)

Each carrier uses the same strip-then-inject the query params and auth already
use, so an LLM-supplied value at a role slot is normalized to exactly the role's
value (no duplicates, no drift):

- **query (`params`)**: `setQueryParam(dialURL, k, v)` — unchanged (M2-roles).
- **header (`headers`)**: `opts.HTTPHeader.Del(k); opts.HTTPHeader.Set(k, v)`.
  `opts.HTTPHeader` is already built from `a.Headers` earlier in `doConnect`
  (`websocket.go:132-136`), so this strips any LLM-supplied value at that key
  before setting the role's.
- **subprotocol (`subprotocols`)**: for each `s`:
  `opts.Subprotocols = removeString(opts.Subprotocols, s); opts.Subprotocols =
  append(opts.Subprotocols, s)`. `opts.Subprotocols` is already built from
  `a.Subprotocols` (`websocket.go:137-139`), so this removes any LLM-supplied
  entry at that name before appending the role's. `removeString` already exists
  (`websocket.go:345`, used by auth's subprotocol strategy).

The role-carrier injection runs in the existing role-expansion block
(`websocket.go:181-194`), after `injectAuth` (so auth's token slot is already
filled on its carrier; a role's carrier entries are different keys/names — see
D3 — so they never touch the token).

### D3 — Token-slot collision is rejected on every carrier (symmetric validation)

M2-roles rejects a `params` key equal to `auth.param` (the token slot) when
`auth.strategy=query`. This generalizes: a role must not occupy the auth token
slot on **any** carrier, or the role would overwrite the injected credential.

Validation extends the collision check:

- `params` key == `auth.param` AND `auth.strategy=query` → reject (existing).
- `headers` key == `auth.param` AND `auth.strategy=header` → reject (new).
- `subprotocols` entry == `auth.param` AND `auth.strategy=subprotocol` → reject
  (new).

When `auth` is unset, or `auth.strategy` does not match the carrier, there is no
token slot on that carrier and no collision is possible — the role is free to use
it. (A role on a service without `auth` can set any header/subprotocol/query it
likes.)

### D4 — Secret hygiene unchanged (headers/subprotocols never reach WSResult.URL)

`WSResult.URL` carries the **pre-injection** url (query string only, auth param
stripped). Headers and subprotocols are not part of a url, so adding role
header/subprotocol injection does not touch `WSResult.URL` at all — the
pre-injection-url contract and the redaction backstop are both unaffected. The
resolved credential value continues to live only in local scope during
`doConnect` and is never logged (M1 guarantee, unchanged).

### D5 — Fallback (no headers/subprotocols, or no role) is byte-identical to M2-roles

A role with `Headers == nil` and `Subprotocols == nil` injects nothing new —
exactly M2-roles (query params + credential + handshake). A `ws_connect` without
`role`, or a service without `roles:`, is M1. The new code paths are guarded by
`len(role.Headers) > 0` / `len(role.Subprotocols) > 0` and are inert otherwise.

## Schema & Validation Changes

**`internal/project/protocol_schema.go`** — add two fields to `ProtocolRole`
(see D1) with doc comments noting the carrier and the token-slot collision rule.

**`internal/project/validate_protocol.go`** — in the existing role loop
(`validate_protocol.go:42-68`), add the two collision checks (D3). The check is
gated on `p.Auth != nil && p.Auth.Strategy == <carrier>` so a role on an
auth-less service, or with a non-matching auth strategy, is unrestricted on that
carrier.

No new validation for the carrier values themselves: header names and
subprotocol names are opaque strings the target defines (cerberus does not model
HTTP header / WS subprotocol naming rules — that is protocol detail left to the
LLM/case author, per the architecture philosophy).

## Executor Changes

**`doConnect`** (`websocket.go:181-194`) — in the role-expansion block, after
the existing `roleParams` query injection, add:

```go
// Role discriminator headers (strip-then-inject): remove any LLM-supplied
// value at this key, then set the role's. opts.HTTPHeader already carries
// a.Headers, so this normalizes to exactly the role's value.
for k, v := range role.Headers {
    opts.HTTPHeader.Del(k)
    opts.HTTPHeader.Set(k, v)
}
// Role discriminator subprotocols (strip-then-inject): remove any LLM-supplied
// entry at this name, then append it.
for _, s := range role.Subprotocols {
    opts.Subprotocols = append(removeString(opts.Subprotocols, s), s)
}
```

`role` is already in scope (resolved at `websocket.go:146-157`). `opts` is the
`*websocket.DialOptions` built earlier. `removeString` exists. The
`preInjectionURL` recompute (`websocket.go:188-194`) is **unchanged** — it only
cares about query params (role params + auth param); headers/subprotocols do not
appear in a url.

**Unchanged:** `injectAuth`, the query-param injection, the handshake loop,
`doSend`/`doReceive`/`doDisconnect`, the action types, secret hygiene, and the
no-role / no-protocol fallback paths.

## Judgment Model

Unchanged from M2-roles. `ws_connect` stays intermediate; the handshake (if any)
is non-decisive. Role carrier expansion is connect-time mechanics; it does not
touch the decisive/intermediate contract.

## Testing Strategy

Table-driven, mirroring `websocket_test.go` and `validate_protocol_test.go`.

- **validation:** `headers`/`subprotocols` accepted on a role; a `headers` key
  colliding with `auth.param` (strategy=header) rejected; a `subprotocols` entry
  colliding with `auth.param` (strategy=subprotocol) rejected; the existing
  `params` collision (strategy=query) still rejected; a role on an auth-less
  service accepts any header/subprotocol (no token slot).
- **role header injection:** a server that inspects `r.Header` before `Accept`
  (the `TestWSConnectSendsHeaders` pattern, `websocket_test.go:118`) observes
  the role's header value; an LLM-supplied value at the same key is stripped
  (exactly the role's value reaches the server).
- **role subprotocol injection:** a server observes the offered subprotocols
  (the upgrade request's `Sec-WebSocket-Protocol` header, readable before
  `Accept`); the role's subprotocol is offered; an LLM-supplied duplicate is
  stripped.
- **carrier coexistence with auth:** a role header/subprotocol plus an auth
  token on a different carrier both reach the server (no collision, no
  overwrite).
- **fallback:** a role with no `headers`/`subprotocols` behaves as M2-roles
  (regression: existing role tests unchanged); `WSResult.URL` is the
  pre-injection url (headers/subprotocols never leak into it).
- **secret hygiene:** the auth token never appears in `WSResult.URL` when a role
  also injects headers/subprotocols.

## Relationship to M0 / M1 / M2 / M3

- **M2-roles** is the base; this sub-project widens the discriminator carrier
  from query-only to all three, with no change to existing role declarations.
- **M1 auth** already knows the three carriers; the role carriers reuse the same
  strip-then-inject primitives (`setQueryParam`, header `Del`/`Set`,
  `removeString`+append) — no new mechanics.
- **M3** may have Scout infer the carrier (query/header/subprotocol) per role
  from descriptions/docs/captures and emit role declarations accordingly.

## Open Questions

1. **Subprotocol ordering.** Does any real target require a specific offered
   order (e.g. discriminator subprotocol before a version subprotocol)? cerberus
   appends role subprotocols after auth's; validate in dogfooding, model order
   only if needed.
2. **Mixed auth + role subprotocol collision beyond the token.** If a service
   uses `auth.strategy=subprotocol` and a role offers a subprotocol that is not
   `auth.param` but conceptually overlaps, nothing detects it — acceptable (the
   names are opaque; the case author owns correctness).
3. **Header value validation.** No validation of header/subprotocol name or
   value shape (opaque strings). If a target needs structural validation (e.g.
   subprotocol must match `^[a-z]+$`), that is a future, target-specific rule.
