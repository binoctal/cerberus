# WebSocket Realtime Engine (M2) — Roles & Handshake (Design)

**Date:** 2026-07-21
**Status:** Design (brainstormed + self-reviewed; pending spec review)
**Scope:** `internal/project/` (role schema + validation), `internal/head/agent/` (websocket executor role expansion + auto-handshake, prompts), `internal/types/` (`WSConnectAction.Role`), `cerberus-docs/executors/websocket.md`
**Depends on:** M1 — `cerberus-docs/superpowers/specs/2026-07-20-ws-realtime-engine-m1-design.md`

## Background & Motivation

M1 added a `protocol:` block declaring service-level facts (auth strategy,
`type_path`, framing) consumed executor-side. But a multi-client realtime
protocol has **multiple distinct connection types on one service**. In
open-agents (the dogfooding target):

- **web** connection: the web-actor's JWT, discriminator `?type=web`, and a
  **mandatory handshake** — after connect it must await a `devices:sync`
  message before it is usable.
- **bridge** connection: the bridge-actor's deviceToken, discriminator
  `?type=bridge&deviceId=…`, no handshake.

Without roles (M1), the LLM re-derives and emits all three — which actor, the
discriminator, and whether to await a handshake — on **every** connect. That is
repetitive (token cost) and error-prone (wrong actor, forgotten discriminator,
missed `devices:sync`). M1 explicitly deferred the handshake to M2 precisely
because it is **per-role** (only web awaits `devices:sync`) and could not be
expressed without a role concept.

### Role's purpose

A **role** bundles the per-connection-type facts — credential (`credential_ref`),
discriminator query `params`, and optional `handshake` — into a named declaration
(`web`, `bridge`) that the **executor** owns:

1. **Determinism** — for a given role name the executor applies the right
   credential + discriminator + handshake every time; no LLM variation.
2. **Token savings** — the LLM emits `role: "web"` instead of re-deriving the
   bundle each connect.
3. **Correctness** — the mandatory handshake is formalized and guaranteed by the
   executor, not left to LLM memory.
4. **M3 bridge** — a role is the natural unit Scout will emit when generating WS
   cases from protocol descriptions.

Roles and handshake are **coupled**: the handshake is per-role (only web), so
handshake cannot be cleanly formalized without something that distinguishes the
connection types — i.e., roles.

### Grounding signal (static, not runtime)

As with M1, M0/M1 dogfooding was not run; the facts above are extracted
statically from the open-agents source/docs (`permission-flow-real.spec.ts`,
`SESSION_MESSAGE_FLOW.md`, `PERMISSION_TESTING.md`).

## Goal

Let a service declare named **roles** (each with its credential, discriminator
params, and optional mandatory handshake) and have the WS executor expand a
`ws_connect { role: … }` into: credential injection (reusing M1 strip-then-inject),
discriminator-param injection, and an auto-run handshake — while preserving M1
behavior for connections that do not use a role.

Success criteria:

- A `ws_connect` with `role: "web"` gets the web-actor's token, `?type=web`, and
  the executor auto-awaits `devices:sync` (via the declared `type_path`) before
  the connect returns success.
- A `ws_connect` without `role` behaves exactly as M1.
- The handshake is **non-decisive**: its arrival means "connection ready," not
  "case passed."
- Handshake timeout fails the connect (and cleans up the connection).
- Credentials never leak (M1 secret hygiene preserved).

## Non-Goals

- **Role discovery / Scout-generated cases** — how the LLM learns role names
  (the static steer prompt cannot carry them) is an Open Question; full
  value-realization is dogfooding / M3. M2 ships the mechanism + graceful
  fallback.
- **Field-level assertions** (`assert payload.approved == true`) — separate M2
  sub-project, own spec.
- **`text`/`binary` framing** — separate M2 sub-project, own spec.
- **header/subprotocol discriminator params** — M2 roles inject `params` as
  **query** params (the open-agents case). Header/subprotocol discriminators
  defer to M2.x.
- **Multiple roles per connection** — one role per `ws_connect`.
- **A general expression/evaluator engine** — roles are static templates; the
  dynamic url (userId/deviceId) stays LLM-supplied (M0 Constraint 3 preserved).

## Design Decisions

### D1 — Role = executor-expanded template (the LLM names it, the executor owns it)

**Decision:** a `ws_connect { role: "web", connection_id, url }` references a
declared role; the executor resolves the role and applies its credential,
discriminator params, and handshake. The LLM provides the **base url** (with
dynamic path/query like `/ws/{userId}` and `deviceId`) but not the token, the
discriminator, or the handshake.

**Rejected — role as annotation only (LLM still orchestrates handshake).** Would
not formalize the handshake — the core correctness win — and leaves drift.

**Rejected — role as full url template (executor constructs the whole url).**
Requires bounded variable substitution (`{userId}`, `{deviceId}` resolution),
which borders on the evaluator cerberus deliberately lacks (M0 Constraint 3).
Keeping the dynamic url LLM-supplied avoids that.

### D2 — Roles reuse M1's authority model (strip-then-inject) for both credential and discriminator

The executor is authoritative on the role's **credential** (reuses M1
`injectAuth` with the role's `credential_ref` overriding `protocol.auth.credential_ref`)
and on the role's **discriminator params**: it strips any LLM-supplied value at
those param names, then injects the role's values. So even if the LLM emits
`?type=web` itself, the executor normalizes to exactly the role's declaration.
Consistent with M1's executor-authoritative auth; the static-prompt limitation
is mitigated the same way (correctness does not depend on the LLM omitting
values).

### D3 — Handshake is a non-decisive, executor-run receive inside `doConnect`

After dial, if the role declares a `handshake.await_type`, the executor runs an
internal receive loop (guarded by the connection's `readMu`, matching via
`extractTypePath(data, type_path)` — both reused from M1) until `await_type`
arrives or `timeout` elapses.

- **Non-decisive:** `await_type` arrival → connect **success** (the connection is
  "ready"); the case continues steering. It is NOT a `decisive=true` receive —
  it does not pass the case. (`ws_connect` stays an intermediate step.)
- **Non-matching messages during the handshake** are accumulated as evidence
  (same as M1's `doReceive`), not consumed silently.
- **Timeout → connect fails:** the dial succeeded but the mandatory handshake
  did not complete; a connection without the handshake is unusable. The executor
  **closes the connection and removes its entry** from the table (explicit
  cleanup, not just `ctx.Done`), then returns a failure result. This drives the
  M0 recovery/retry path.

### D4 — Handshake evidence flows through the connect's `WSResult`

A connect result today carries only `{OK, URL, Latency}`. With an auto-handshake,
the connect accumulates the handshake message plus any non-matching
handshake-period messages. These go into the connect `WSResult.SeenMessages`
(reusing the existing field — no new field). The Examiner's `buildEvidenceContext`
(M0) already reads `SeenMessages`, so the handshake exchange is visible to
content judgment. `MatchedMessage` stays empty for a connect (a connect has no
"matched" decisive message).

### D5 — Dynamic values stay LLM-supplied (no evaluator)

Roles are **static templates**: `credential_ref`, discriminator `params`, and
`handshake`. Dynamic per-test values — the `userId` in the url path, bridge's
`deviceId` — are provided by the LLM in the base `url` (as in M0/M1, where the
LLM already constructs the url). This preserves M0 Constraint 3 (no
substitution/evaluator engine). If dogfooding shows the LLM cannot reliably
source these values, that becomes an Open Question for M3 (Scout-supplied case
context), not a role-mechanism change.

### D6 — Fallback: no role → exact M1 behavior

`ws_connect` without `role` (or a service without `roles:`) uses
`protocol.auth.credential_ref` (or the action's `credential_ref`) and no
auto-handshake — exactly M1. Roles are a graceful enhancement.

## Protocol Schema (extends M1 `Protocol`)

Add a `Roles` map to `Protocol` (`internal/project/protocol_schema.go`):

```go
// ProtocolRole declares a named connection type's credential, discriminator
// query params, and optional mandatory handshake.
type ProtocolRole struct {
	// CredentialRef names the actor whose resolved raw token is injected for
	// this role (overrides protocol.auth.credential_ref).
	CredentialRef string            `yaml:"credential_ref"`
	// Params are discriminator query params applied (strip-then-inject) to the
	// dial url. Must not include protocol.auth.param (the token slot).
	Params        map[string]string `yaml:"params,omitempty"`
	// Handshake is the optional mandatory post-connect exchange.
	Handshake     *RoleHandshake    `yaml:"handshake,omitempty"`
}

// RoleHandshake declares the message the executor auto-awaits after connect.
type RoleHandshake struct {
	// AwaitType is the routing-key value (at protocol.type_path) to wait for.
	AwaitType string `yaml:"await_type"`
	// Timeout is seconds to wait; must be > 0 (validation) so a mandatory
	// handshake cannot hang a case indefinitely.
	Timeout   int    `yaml:"timeout,omitempty"`
}

type Protocol struct {
	Framing  string                    `yaml:"framing,omitempty"`
	TypePath string                    `yaml:"type_path,omitempty"`
	Auth     *ProtocolAuth             `yaml:"auth,omitempty"`
	Roles    map[string]*ProtocolRole  `yaml:"roles,omitempty"`   // M2
}
```

`WSConnectAction` (`internal/types/actions_http.go`) gains:

```go
// Role optionally names a declared protocol role whose credential, discriminator
// params, and handshake the executor expands. When set, CredentialRef is
// ignored and the role's declaration drives auth + params + handshake.
Role string `json:"role,omitempty"`
```

> **`HandshakeTimeout` field clarification:** `WSConnectAction` already has a
> M0-era `HandshakeTimeout` (dial-timeout, effectively unused — the dial uses
> `ctx`). M2's `role.handshake.timeout` is a **different** concept (await a
> message). The M0 field is left as-is (unused); M2 does not overload it. The
> plan may deprecate/remove the unused M0 field if lint flags it.

Example — open-agents:

```yaml
protocol:
  framing: json
  type_path: type
  auth: { strategy: query, param: token, credential_ref: web-actor }
  roles:
    web:
      credential_ref: web-actor
      params: { type: web }
      handshake: { await_type: devices:sync, timeout: 5 }
    bridge:
      credential_ref: bridge-actor
      params: { type: bridge }
```

The LLM orchestrates: `ws_connect { role: "bridge", connection_id: "bridge",
url: "ws://host:8787/ws/<userId>?deviceId=<devId>" }` (base url with dynamic
path + deviceId), then `ws_connect { role: "web", connection_id: "web", url:
"ws://host:8787/ws/<userId>" }` — the executor adds the token + `type=…` and
auto-awaits `devices:sync` for web.

## Executor Changes

**`doConnect`** (extends M1, after `resolveProtocol` at the top, before
`injectAuth`/dial):

1. If `a.Role != ""`:
   - Resolve `proto.Roles[a.Role]`; if absent (or `proto` itself is nil) → fail
     the connect with a non-secret error (`unknown role %q`) and do not dial.
   - Resolve the effective credential_ref: `role.CredentialRef` (role set) →
     `a.CredentialRef` (action) → `proto.Auth.CredentialRef` (protocol default).
     **Refactor `injectAuth` to take this as an explicit `actor` param** (M1 read
     `a.CredentialRef`/`auth.CredentialRef` internally, so without this refactor a
     role connect would silently fall back to the protocol default and ignore
     `role.CredentialRef`); `doConnect` passes the resolved value.
   - After `injectAuth` (token strip-then-inject), **strip-then-inject each of
     `role.Params`** into the dial url query (delete then set).
2. Dial (M1).
3. If the role declares `Handshake`: run the auto-handshake loop (D3) before
   returning — guard `entry.readMu`; loop `conn.Read`, match via
   `extractTypePath(data, type_path) == await_type`, accumulate non-matches into
   `seen`, until match or `timeout`. On match → success (append the matched
   handshake msg + `seen` into the result's `SeenMessages`). On timeout → close
   conn, remove the namespaced entry from the table, return failure.
4. If no role → unchanged M1 path (no param injection, no handshake).

**Role params authority:** strip-then-inject (delete each role-param key from
the url query, then set the role's value), mirroring `injectAuth`. If a role
param name equals `protocol.auth.param` (the token slot), the later token
injection wins — validation forbids this collision (see Validation).

**No new threading:** roles ride on the existing `*Protocol` (and thus
`WSProtocolIndex.ByHost`); the executor reads `proto.Roles` directly.

## Validation

Extend `internal/project/validate_protocol.go` (`ValidateProtocol`):
- Role names are keys (inherently unique within a map).
- `role.credential_ref` (when set) must name an existing actor (reuse the
  existing actor check).
- `role.params` must not include `protocol.auth.param` (token-slot collision).
- When `role.handshake` is set: `await_type` non-empty, `timeout > 0` (a
  mandatory handshake must not hang a case indefinitely).
- (Optional warning) a `ws_connect` setting both `role` and `credential_ref`.

## Prompt & Docs

- `prompts.go` steer prompt (single raw-string literal, inline edit): generic
  best-effort hint — when a service declares roles, the executor expands a
  `role` connect (omit token + discriminator params; provide the base url with
  dynamic values like userId/deviceId); the handshake runs automatically. This is
  token-saving only; correctness does not depend on it (the executor is
  authoritative — D2).
- `cerberus-docs/executors/websocket.md`: document `roles:`, the `role` connect
  field, auto-handshake, and the M1 fallback.

## Testing

Table-driven, mirroring M1:
- Role resolution + credential override (role's `credential_ref` wins over
  `protocol.auth.credential_ref`).
- Discriminator `params` strip-then-inject (LLM-supplied `type=web` normalized;
  exactly one present).
- Auto-handshake: `await_type` arrives → connect success + handshake msg in
  `SeenMessages`; non-matching handshake-period msg accumulated.
- Handshake timeout → connect fails + conn closed + entry removed (assert a
  subsequent `ws_send` on that `connection_id` fails as unknown).
- Unknown role / role without protocol → connect fails, no dial.
- `role.params`/`auth.param` collision rejected by validation.
- No role → M1 behavior (regression).

## Impact / Change List

**New:** none (extends existing files).
**Modified:**
- `internal/project/protocol_schema.go` — `Protocol.Roles`, `ProtocolRole`,
  `RoleHandshake`.
- `internal/project/validate_protocol.go` — role validation.
- `internal/types/actions_http.go` — `WSConnectAction.Role`.
- `internal/head/agent/websocket.go` — role resolution, param strip-then-inject,
  auto-handshake loop, failure cleanup; `injectAuth` refactored to take an
  explicit resolved-actor param.
- `internal/head/agent/prompts.go` — best-effort role hint.
- `cerberus-docs/executors/websocket.md` — roles documentation.

**Unchanged:** `WSProtocolIndex` threading, action registry/deref/plugin wiring
(only a field on `WSConnectAction`), decisive/intermediate judgment, the M1
fallback path.

## Open Questions

1. **Role discovery (headline).** Roles only deliver token savings when the LLM
   emits the role name; the static steer prompt cannot teach role names per
   service. M2 ships the mechanism + graceful M1 fallback (no regression if the
   LLM never uses roles). Whether the LLM discovers/uses roles — and thus
   whether M2 realizes its value — is validated by dogfooding and fully resolved
   by M3 (Scout-generated WS cases that emit role connects from the protocol
   declaration).
2. **LLM sourcing dynamic url values** (`userId`, `deviceId`). Assumed
   LLM-provided in the base url (M0/M1 posture). If dogfooding shows the LLM
   cannot reliably source these, it becomes an M3 Scout/case-context concern.
3. **Discriminator param strategies beyond query.** M2 injects `params` as query
   params (open-agents). Header/subprotocol discriminators defer to M2.x if a
   real target needs them.
4. **Dogfooding** remains deferred (same as M0/M1).
