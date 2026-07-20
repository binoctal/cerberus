# WebSocket Realtime Engine (M1) — Protocol Adaptation Layer (Design)

**Date:** 2026-07-20
**Status:** Design (brainstormed; pending spec review)
**Scope:** `internal/project/` (protocol schema + validation), `internal/head/agent/` (websocket executor, service→protocol threading, prompts), `internal/types/` (WSConnectAction `actor` field), `cerberus-docs/executors/websocket.md`
**Depends on:** M0 — `cerberus-docs/superpowers/specs/2026-07-20-ws-realtime-engine-m0-design.md`
**Proposal:** `cerberus-docs/superpowers/specs/2026-07-20-ws-realtime-engine-m1-proposal.md`

## Background & Motivation

M0 shipped a protocol-agnostic WS primitive engine: `ws_connect`/`ws_send`/
`ws_receive`/`ws_disconnect` over persistent, per-case-context-bound connections,
with `decisive`-step judgment and Examiner visibility into message bodies. It is
zero-config generic by design — the LLM re-derives protocol knowledge on every
run: how auth is attached (query / header / subprotocol), where the
message-routing field lives (M0 hard-assumes top-level `type`), and the wire
framing (M0 assumes JSON).

This maximizes generality but costs:

- **Tokens:** the same protocol facts are inferred run after run.
- **Determinism:** the LLM can guess differently across runs (and can guess
  wrong, e.g. auth in the wrong place).
- **Reproducibility:** two runs of the same case may orchestrate differently.

M1 persists the protocol knowledge that is *stable and knowable ahead of time*
into a declaration on the service, and makes the executor **act on it** — so the
executor reads a declaration instead of the LLM re-inferring it. The LLM remains
the orchestrator (which connection, when, what message content); the declaration
removes only the repetitive protocol guesswork.

M1 also completes two items deferred from M0 (see M0 design D3 / Open Questions):
per-case `connection_id` namespacing and `doReceive` read serialization.

### Grounding signal (static, not runtime)

M0 dogfooding was not run before this design (the open-agents stack is a
heavyweight docker+supabase multi-service setup, not running in this session).
The protocol facts below were extracted **statically** from the open-agents
source and docs (`test/integration/websocket/permission-flow-real.spec.ts`,
`docs/PERMISSION_TESTING.md`, `docs/SESSION_MESSAGE_FLOW.md`):

- **Auth:** credentials ride the URL query string — `?type=web&token=<jwt>` for
  the web client, `?type=bridge&deviceId=<id>&token=<deviceToken>` for bridge.
- **Routing field:** messages are `{type, payload, timestamp}` JSON routed on the
  **top-level `type`** (`permission:request`, `permission:response`,
  `devices:sync`, …). open-agents itself satisfies M0's assumption; `type_path`
  earns its keep for *other* protocols.
- **Framing:** JSON text.
- **Handshake:** role-dependent — only the web connection must await
  `devices:sync` before it is ready; bridge does not. This per-role shape is why
  handshake is deferred to M2 (see Non-Goals).

LLM-behavior signals (token-waste magnitude, run-to-run drift) are not measured
here; they are listed as Open Questions to validate after M1 lands.

## Goal

A **protocol adaptation layer** that lets a service declare its three stable
protocol facts (`auth`, `type_path`, `framing`) and have the WS executor consume
that declaration deterministically, while preserving M0's zero-config fallback
for undeclared services.

Success criteria:

- A service with a `protocol:` block runs cheaper (LLM no longer emits
  credentials or re-derives the routing field) and more deterministically
  (executor injects auth and matches by the declared path every time).
- A service **without** a `protocol:` block behaves exactly as M0.
- `type_path` lifts M0's top-level-`type` assumption: a protocol whose routing
  key lives at `data.event` (or any dotted path) is matchable without code
  changes.
- Parallel cases passing the same LLM-supplied `connection_id` (e.g. `"conn1"`)
  do not collide — connection table keys are namespaced by case.
- Concurrent `ws_receive` on the same connection does not race
  (`coder/websocket` forbids concurrent `Read` on one conn).
- Credentials resolved via the declaration never appear in results or logs.

## Non-Goals

- **Handshake / connect-time exchange** — per-role in the one real target;
  belongs with role abstraction in **M2**.
- **Role abstraction** (named connection templates like `bridge`/`web`) — **M2**.
- **Timing & field-level assertions** (message-order/window, `assert`) — **M2**.
- **Standalone protocol files, Scout-generated cases, auto-inference** — **M3**.
- **Binary framing** — deferred; most targets are JSON/text. M1 supports
  `json` (default) and `text`.
- **Array indexing / JSONPath in `type_path`** — cerberus has no runtime
  evaluator (M0 Constraint 3); M1 uses lightweight dotted paths only. No new
  expression/JSONPath dependency.
- **Reducing orchestration tokens** (the LLM still steers which connection,
  when, and message content) — M1 removes only protocol-fact re-inference.

## Design Decisions

### D1 — Where the declaration lives: inline on `Service`; fallback to M0

The declaration is a `Protocol *Protocol` field on the existing `Service`
struct (`internal/project/schema.go:17`), mirroring how `Actor.Auth *AuthFlow`
and `Service.BodyTemplate` are embedded sub-structs today. No new top-level
`protocols:` slice (YAGNI — cross-project protocol reuse is unproven; extract to
standalone files in M3 only if reuse materializes).

**Fallback policy (graceful enhancement, not replacement).** `Service.Protocol`
is nil-able. When nil (or when the executor has no protocol entry for the
dial host), the executor behaves exactly as M0: receive matches top-level
`type`, framing is JSON, auth is **not** auto-injected (the LLM puts credentials
into url/headers/subprotocols itself). This preserves M0's zero-config
generality — a new/unknown protocol still runs with no declaration; a known
protocol runs cheaper and more deterministically with one. This mirrors
`Actor.Auth == nil` semantics exactly.

### D2 — What the declaration covers: `auth`, `type_path`, `framing` (no handshake)

M1 declares three facts. `handshake` is **out** — see Background: the one real
target's handshake is per-role (only web awaits `devices:sync`), and expressing
"this handshake applies to connection A but not B" cleanly requires role
abstraction, which is M2. Declaring a role-specific handshake without roles is
forced; M2 will carry both. The static handshake fact can be conveyed
informally via the prompt/example in M1 until M2 formalizes it.

### D3 — Executor-driven consumption (the LLM orchestrates, the executor adapts)

**Decision:** the executor reads the declaration and **acts on it** —
`ws_connect` auto-injects the declared auth; `ws_receive` matches by the declared
`type_path` and decodes by the declared `framing`. The declaration becomes
deterministic executor behavior, not advice the LLM may ignore.

**Rejected — prompt-driven (declaration fed to the steer prompt, executor
unchanged).** The LLM would still emit token-laden urls/headers and could still
mis-place auth — token cost barely falls and drift is untreated. M1's value
comes from the executor *executing* the declaration.

**Rejected — hybrid (type_path/framing to executor, auth placement to LLM).**
Auth placement (query vs header vs subprotocol) is precisely the repetitive
guesswork M1 exists to eliminate; leaving it to the LLM discards the core win.

**Trade-off accepted:** the executor gains protocol knowledge (auth injection +
dotted-path resolution + framing decode). This is intentional and bounded — it
is generic protocol mechanics, not any system's business semantics, and the LLM
remains the orchestrator for everything else.

### D4 — Per-case `connection_id` namespacing (completes M0 D3)

M0's connection table keys on the raw `connection_id`. Auto-generated ids
(`ws-<seq>`, monotonic counter) are globally unique and safe, but an
LLM-supplied id like `"conn1"` **collides across parallel cases** that happen to
choose the same name. M0's design intended case-namespacing; the implementation
only delivered unique auto-ids.

**Decision:** namespace the table key internally as `<caseID>:<connection_id>`.
`store`/`lookup`/`doDisconnect` all use the namespaced key. The case identifier
is carried on the per-case context: the step executor (which derives the
per-case `context.WithTimeout` at `execute_phases.go:40`) injects the `TestCase`
identifier via `context.WithValue(ctx, caseIDKey, id)`; `doConnect` reads it. A
package-local unexported key type avoids collisions. When the key is absent
(e.g., executor unit tests using `context.Background()`), the executor uses a
sentinel namespace (e.g. `"_default"`) so behavior is well-defined and
single-case tests are unaffected. Auto-generated ids remain unique.

### D5 — `doReceive` read serialization (M0 hardening)

`coder/websocket` forbids concurrent `Read` on the same connection. M0 had a
single receive per connection in practice and did not guard this; two concurrent
`ws_receive` actions on one `connection_id` (or a receive racing another read)
would error or panic.

**Decision:** add a per-connection read mutex. `wsEntry` gains
`readMu sync.Mutex`; `doReceive` takes it around `conn.Read`. Writes
(`doSend`/`conn.Write`) are unaffected — `coder/websocket` permits concurrent
Read and Write, only concurrent same-direction calls are forbidden. This keeps
multi-receive scanning (the M0 evidence-accumulation loop) safe.

### D6 — Secret hygiene (credential redaction)

Query-string auth puts the token in the url; M0 already exposes this (the LLM
emits `?token=…` and `WSResult.URL` carries it). M1 makes auth injection
systematic, so it must also systematize redaction.

**Decision:** `WSResult` never surfaces secrets. The executor keeps the secret
out of `WSResult.URL` by storing the **pre-injection** url (the LLM-supplied url
without the secret) in the result, while dialing with the injected url. Any
residual sensitive query params (defensive — e.g., an LLM that ignores the
prompt and includes a token) are redacted in `WSResult.Summary()`/`Evidence()`
using a denylist built from every declared `protocol.auth.param` plus a default
set (`token`, `password`, `secret`, `key`, `authorization`). Redacted params
render as `token=<redacted>`. The resolved credential value itself is held only
in local scope during `doConnect` and is never logged (same guarantee as
`authflow.go` / `auth_setup.go`).

### D7 — `credential_ref` resolution: reuse the actor pipeline

There is no existing `credential_ref` concept; the architecture is
**actor-centric** (`Actor{Credentials, Auth *AuthFlow, Service}`). M1 does not
invent a parallel secret store.

**Decision:** `protocol.auth.credential_ref` (and the per-action override on
`WSConnectAction`) **names an entry in `cfg.Actors`**. The resolved credential
**value** is obtained through the existing pipeline:

- If the actor has an `AuthFlow`, its login runs once per session and the token
  is extracted via `TokenFrom` (the `{token}` substitution source) — the same
  machinery as `ResolveAuthHeader` (`authflow.go:70`).
- Else the actor's static `Credentials` / env-overlayed values are used.

Injection depends on `strategy`:

- `header` — reuse the existing formatted resolution (`ResolveAuthHeader`
  returns `(name, value)`); set `name: value` on the dial headers. Pure reuse.
- `query` / `subprotocol` — the **raw token value** is needed (open-agents wants
  `?token=<jwt>`, not `?token=Bearer <jwt>`). M1 adds a raw-value resolution
  alongside `ResolveAuthHeader` (e.g., `ResolveAuthToken`) that returns the
  `{token}` before `InjectAs` formatting, then places it at the declared
  `param`. (See Open Question 3 — the exact function is finalized in the plan
  against how open-agents actors obtain their JWT/deviceToken.)

`WSConnectAction` gains an optional `credential_ref` field (JSON
`credential_ref,omitempty`) that overrides the service default for this
connection. This is how M1 represents open-agents' two distinct credentials
(web JWT, bridge deviceToken) **without** roles: both connections use the same
service `strategy=query/param=token`, and each `ws_connect` names the actor whose
value to inject. Roles (M2) will formalize "web"/"bridge" as named templates.

## Protocol Schema

New file `internal/project/protocol_schema.go` (mirrors `authflow_schema.go`):

```go
// Protocol declares the stable, knowable-ahead-of-time WebSocket protocol
// facts for a service. Nil on a Service means "fall back to M0 behavior".
type Protocol struct {
    Framing  string       `yaml:"framing,omitempty"`   // "json" (default) | "text"
    TypePath string       `yaml:"type_path,omitempty"`  // dotted path; default "type"
    Auth     *ProtocolAuth `yaml:"auth,omitempty"`
}

// ProtocolAuth declares where credentials go and which actor supplies them.
type ProtocolAuth struct {
    Strategy      string `yaml:"strategy"`        // "query" | "header" | "subprotocol"
    Param         string `yaml:"param"`           // query param / header / subprotocol name
    CredentialRef string `yaml:"credential_ref"`  // names an entry in actors[]
}
```

`Service` (`schema.go:17`) gains:

```go
Protocol *Protocol `yaml:"protocol,omitempty"`
```

Example — open-agents realtime service:

```yaml
services:
  - name: open-agents-realtime
    url: http://localhost:8787
    protocol:
      framing: json
      type_path: type
      auth:
        strategy: query
        param: token
        credential_ref: web-actor

actors:
  - name: web-actor
    credentials: { email: ..., password: ... }
    auth:
      login: { method: POST, path: /auth/login, body: { ... } }
      token_from: data.jwt
      inject_as: "Authorization: Bearer {token}"   # used by HTTP; WS reads raw token
  - name: bridge-actor
    credentials: { email: ..., password: ... }
    auth:
      login: { method: POST, path: /devices/register, body: { ... } }
      token_from: data.deviceToken
      inject_as: "{token}"
```

- `framing`: `json` (default) or `text`. Binary deferred.
- `type_path`: dotted path to the routing key; default `"type"` (= M0). Empty
  also means top-level `type`.
- `auth.strategy` / `auth.param` / `auth.credential_ref` as in D7.

### type_path resolver

Reuse the dotted-path pattern from `extractByDotPath`
(`internal/head/agent/authflow.go:33`), generalized to take `[]byte`:

```go
// extractTypePath returns the routing key at path within a JSON message.
// path is a dotted set of object keys (no array indexing). Returns ("", false)
// if the message is not a JSON object or the path is absent.
func extractTypePath(data []byte, path string) (string, bool)
```

- `path == ""` or `"type"` → top-level `type` (M0 behavior).
- Dotted keys only; no array indexing, no JSONPath (YAGNI + no evaluator).
- `framing: text` → no JSON parse; `ws_receive` matches the raw message text by
  literal equality against `type` (or returns the first message if `type` is
  empty). Structured text protocols are M2+.

## Auth Resolution & Secret Hygiene

Connect-time flow (`doConnect`):

1. Resolve the `Protocol` for the dial url's `host:port` from the
   `protocolByHost` index (see *Executor Changes — Threading*). If none → M0
   behavior (no auto-auth, receive uses top-level `type`, json framing).
2. Determine the actor: `WSConnectAction.credential_ref`, else
   `Protocol.Auth.CredentialRef`.
3. Resolve the value: `header` strategy → `ResolveAuthHeader` formatted pair;
   `query`/`subprotocol` → raw `ResolveAuthToken` value.
4. Inject: `query` → append `&<param>=<value>` (or `?<param>=<value>`) to the
   dial url; `header` → set on dial headers; `subprotocol` → add to
   `Subprotocols`.
5. Store the **pre-injection** url in `WSResult.URL` (secret-free); dial with
   the injected url.
6. Stash the resolved `*Protocol` on the `wsEntry` so `doReceive` can read
   `type_path`/`framing`.

Redaction (D6) applies in `WSResult.Summary()`/`Evidence()` regardless, as a
defense against an LLM that includes a secret in the url despite the prompt.

## Executor Changes

**Threading (mirrors `NewHTTPExecutorWithServiceHeaders`):**

```go
func NewWebSocketExecutor(logger *zap.Logger, protocolByHost map[string]*project.Protocol) *WebSocketExecutor
```

`protocolByHost` is built from `cfg.Services` (host:port → `*Protocol`) at the
same call sites that build `ServiceHeadersMap` today
(`session/run_phases_agent.go:25`, `session/resume_phases_run.go:25`). A service
with `Protocol == nil` simply contributes no entry, so the host falls back to
M0 behavior. Host-level granularity matches the existing HTTP service-header
matching; a future need for path-level matching can extend the index then.

**`wsEntry`** gains `protocol *Protocol` and `readMu sync.Mutex`.

**`doConnect`** — namespaced key (`<caseID>:<connectionID>`), auth resolution +
injection per above, stash protocol, store pre-injection url in result.

**`doReceive`** — guard `readMu`; decode by `entry.protocol.framing`; extract
routing key by `entry.protocol.type_path` (or top-level `type` when no
protocol); match logic otherwise unchanged from M0 (accumulate non-matches as
evidence).

**`doSend` / `doDisconnect`** — namespaced lookup; otherwise unchanged.

## Judgment Model

Unchanged from M0. `ws_connect`/`ws_send`/`ws_disconnect` and non-decisive
`ws_receive` remain intermediate (`isIntermediateStep`,
`react_loop_helpers.go:184`); a `decisive=true` receive whose routing key
arrives passes the case. M1 changes *how* the routing key is extracted, not the
decisive/intermediate contract or the Phase-7 recovery guard.

## Impact / Change List

**New:**
- `internal/project/protocol_schema.go` — `Protocol`, `ProtocolAuth`.
- `internal/project/validate_protocol.go` — `validateProtocol(cfg, ve)` (Phase
  6) + exported `ValidateProtocol(*Protocol) error`.
- (If not already present) a raw-token resolution helper alongside
  `ResolveAuthHeader` in the auth path (D7).

**Modified:**
- `internal/project/schema.go` — `Protocol *Protocol` on `Service`.
- `internal/project/validate.go` — call `validateProtocol` as Phase 6.
- `internal/head/agent/websocket.go` — `NewWebSocketExecutor` signature,
  `wsEntry` (`protocol`, `readMu`), case-namespaced keys, auth injection in
  `doConnect`, `type_path`/`framing` in `doReceive`, `readMu` guard.
- `internal/types/actions_http.go` — `WSConnectAction.credential_ref`.
- `internal/types/result_ws.go` — secret redaction in `Summary()`/`Evidence()`.
- `internal/head/agent/authflow.go` — generalize `extractByDotPath` to a shared
  `extractTypePath(data []byte, path string)` (or extract to a shared helper).
- `internal/head/agent/prompts.go` — steer prompt (single raw-string literal;
  edit inline) documents declaration-driven auth/type_path when a protocol is
  present; M0 guidance otherwise.
- The step-executor path that derives the per-case ctx (`execute_phases.go:40`)
  — inject `caseID` via `context.WithValue`.
- `session/run_phases_agent.go`, `session/resume_phases_run.go` — build
  `protocolByHost` alongside `ServiceHeadersMap`.
- `cerberus-docs/executors/websocket.md` — document the `protocol:` block and
  declaration-driven behavior.

**Unchanged:** `MultiExecutor` routing, the action registry/deref groups (no new
action types — only a field on `WSConnectAction`), Scout, the decisive/
intermediate judgment, the Examiner (it already reads `WSResult` message bodies
from M0), the `protocolByHost`-empty (M0) path.

> **M0 pitfall reminder (from M0 implementation):** no new `ActionType` is added
> here (only a field on an existing action), so the registry/deref/plugin/multi
> wiring is untouched — but any future WS action type must still be registered
> in `actions_registry.go`, `wsPlugin.ActionTypes()` (`plugin_executors.go`),
> and the `multi_sandbox.go` switch or `MultiExecutor` will not route it.

## Testing Strategy

Table-driven, mirroring `internal/head/agent/http_test.go` and
`internal/project/authflow_schema_test.go`.

- **type_path resolver:** top-level `type`, nested (`data.event`), missing path
  → no match, non-JSON → no match, empty path → top-level.
- **framing:** json decode + path; text literal match / first-message.
- **auth injection:** each strategy (query/header/subprotocol) with a stubbed
  actor-resolution; assert the dial url/headers/subprotocols carry the value and
  `WSResult.URL` does **not**.
- **secret redaction:** `WSResult.Summary()`/`Evidence()` redact `token=…` (and
  denylist) whether or not the executor injected it.
- **per-case namespace:** two executors (or one executor, two caseIDs via ctx)
  using the same `connection_id` `"c1"` in parallel do not collide; disconnect
  in one case leaves the other's connection intact.
- **receive serialization:** two concurrent `ws_receive` on one connection
  serialize via `readMu` (no panic/error from `coder/websocket`); `-race` clean.
- **validation:** `validate_protocol_test.go` — framing/strategy enums,
  type_path shape, param-required-when-strategy-set, credential_ref-must-name-an-actor.
- **fallback:** a service with `Protocol == nil` behaves as M0 (top-level type,
  no auto-auth).

Integration against the live open-agents stack is **deferred** (see Open
Questions); the static protocol facts above are encoded as fixture/test shapes.

## Validation Case — open-agents permission-flow (static encoding)

Single case, `Expectation`: "bridge receives a `permission:response` with
`payload.approved == true`". With the `protocol:` block above, the LLM
orchestrates within the case; the executor adapts:

1. `ws_connect { url: "ws://host:8787/ws/{userId}?type=bridge&deviceId=<id>",
   connection_id: "bridge", credential_ref: "bridge-actor" }` — executor
   appends `&token=<deviceToken>` (strategy=query/param=token).
2. `ws_connect { url: "ws://host:8787/ws/{userId}?type=web",
   connection_id: "web", credential_ref: "web-actor" }` — executor appends
   `?token=<jwt>`; **note:** awaiting `devices:sync` before ready is a
   handshake step the LLM performs with a non-decisive `ws_receive
   {type:"devices:sync"}` (handshake formalization is M2).
3. `ws_send { connection_id: "bridge", message: {type:"permission:request", …} }`.
4. `ws_receive { connection_id: "web", type:"permission:request", decisive:false }`
   — matched via `type_path: type`.
5. `ws_send { connection_id: "web", message: {type:"permission:response", payload:{approved:true}} }`.
6. `ws_receive { connection_id: "bridge", type:"permission:response", decisive:true }`
   → **case passed**; the `approved==true` content check is judged by the
   Examiner from the matched-message evidence (M0).
7. Per-case ctx exit closes both connections.

No open-agents symbol appears in the executor; the declaration carries the
protocol facts, the LLM carries the orchestration.

## Relationship to M0 / M2 / M3

- **M0** is not rewritten; M1 layers an optional declaration on top and completes
  M0's deferred namespacing/serialization.
- **M2** will add role abstraction (formalizing `bridge`/`web` templates),
  handshake sequences (the role-dependent `devices:sync` step), and field-level
  assertions. M1's per-connect `credential_ref` is the bridge to M2 roles
  without depending on them.
- **M3** may extract standalone protocol files and have Scout generate cases from
  descriptions/docs/captures.

## Open Questions

1. **Token-waste magnitude.** How many tokens does the LLM spend re-deriving
   auth/type/framing per run today? Validate post-M1 with a before/after trace
   comparison on the open-agents permission-flow.
2. **Run-to-run drift.** Does the LLM ever mis-place auth or mis-route without a
   declaration? Validate post-M1.
3. **`credential_ref` value semantics (the one uncertain technical point).**
   `query`/`subprotocol` need the **raw** token; `header` reuses the formatted
   value. The design assumes a small `ResolveAuthToken`-style raw-value helper
   alongside `ResolveAuthHeader`. Finalize the exact function in the plan once
   the open-agents actor setup (how web obtains its JWT, how bridge obtains its
   deviceToken) is confirmed against the auth package internals.
4. **`type_path` coverage.** How many real targets route on a non-top-level
   field? open-agents uses top-level; `type_path`'s value is for future targets.
5. **Host vs path granularity.** `protocolByHost` keys on `host:port` (matching
   the HTTP service-header pattern). If a service ever needs per-path protocol
   selection, the index extends then — deferred as YAGNI.
