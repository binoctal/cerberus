# WebSocket Realtime Engine (M1) — Protocol Adaptation Layer (Design)

**Date:** 2026-07-20
**Status:** Design (brainstormed; revised after self-review; pending spec review)
**Scope:** `internal/project/` (protocol schema + validation), `internal/head/agent/` (websocket executor, service→protocol threading, prompts), `internal/session/` (protocol-index wiring, raw-token caching), `internal/types/` (`WSConnectAction.credential_ref`), `cerberus-docs/executors/websocket.md`
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
- **Framing:** JSON.
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

- A service with a `protocol:` block runs cheaper (the executor injects auth
  authoritatively and matches by the declared routing field, so the LLM no
  longer needs to emit credentials or re-derive routing) and more
  deterministically (same injection + path every run).
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
- **Binary and text framing** — deferred to M2; the one real target and most
  others are JSON. M1 supports `json` only; the `framing` field is reserved
  (accepted only as `json`/empty; `text`/`binary` are rejected by validation
  until M2).
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

**Auth is executor-authoritative (strip-then-inject).** The steer prompt is a
static const (`internal/head/agent/prompts.go:4`) assembled at
`executor_steer.go:28` with **no per-service runtime injection** — there is no
channel to tell the LLM per-service that auth is auto-injected. Correctness
therefore cannot depend on the LLM omitting credentials. Instead `doConnect`
treats its resolved credential as authoritative: it **strips** any existing
`param` the LLM supplied (from url query / headers / subprotocols) and then
**injects** the resolved value. Whether the LLM obeys a prompt hint or not,
exactly one correct credential reaches the server. The prompt change (see
Impact — `prompts.go`) is a best-effort token-saving hint only.

**Why only auth needs this guarantee.** `type_path` and `framing` are fully
executor-side: the LLM supplies the routing-key *value* (e.g.
`type: "permission:response"`) and the message text, and the executor extracts/
decodes per the declaration — no LLM cooperation or prompt dependency. Only auth
risks an LLM-supplied duplicate (the LLM may also emit credentials), which is why
auth alone needs executor authority.

### D4 — Per-case `connection_id` namespacing (completes M0 D3)

M0's connection table keys on the raw `connection_id`. Auto-generated ids
(`ws-<seq>`, monotonic counter) are globally unique and safe, but an
LLM-supplied id like `"conn1"` **collides across parallel cases** that happen to
choose the same name. M0's design intended case-namespacing; the implementation
only delivered unique auto-ids.

**Decision:** namespace the table key internally as `<caseID>:<connection_id>`.
`store`/`lookup`/`doDisconnect` and the ctx-cancellation cleanup goroutine all
use the namespaced key. The case identifier is the `TestCase.ID` field
(`internal/head/agent/types.go:23`) — **not `Name`**, which may collide across
cases. It is carried on the per-case context: `executeStep`
(`internal/head/agent/execute_phases.go:12`, which has `tc *TestCase` in hand
and derives the per-case `context.WithTimeout` at line 40) injects `tc.ID` via
`context.WithValue(ctx, caseIDKey, tc.ID)`; `doConnect` reads it. A
package-local unexported key type avoids collisions. When the key is absent
(e.g., executor unit tests using `context.Background()`), the executor uses a
sentinel namespace (e.g. `"_default"`) so behavior is well-defined and
single-case tests are unaffected. The cleanup goroutine spawned in `store()`
closes over the already-computed namespaced key (not the raw id), so ctx
cancellation prunes exactly the right entry. Auto-generated ids remain unique.

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

### D6 — Secret hygiene (strip-then-inject primary; redaction backstop)

Query-string auth puts the token in the url; M0 already exposes this (the LLM
emits `?token=…` and `WSResult.URL` carries it). M1 makes auth injection
systematic, so it must also systematize secret hygiene. Two layers:

1. **Strip-then-inject (D3, primary).** `doConnect` removes any credential the
   LLM placed at the declared `param` before injecting the resolved value, so a
   stray LLM-supplied token never reaches the result url.
2. **Pre-injection url + redaction (backstop).** The executor stores the
   **pre-injection** url (LLM-supplied, secret-free) in `WSResult.URL` while
   dialing with the injected url. Any residual sensitive query params are
   redacted in `WSResult.Summary()`/`Evidence()` using a denylist built from
   every declared `protocol.auth.param` plus a default set (`token`,
   `password`, `secret`, `key`, `authorization`), rendered as
   `token=<redacted>`.

The resolved credential value is held only in local scope during `doConnect`
and is never logged (same guarantee as `authflow.go` / `auth_setup.go`).

### D7 — `credential_ref` resolution: reuse the actor pipeline

There is no existing `credential_ref` concept; the architecture is
**actor-centric** (`Actor{Credentials, Auth *AuthFlow, Service}`). M1 does not
invent a parallel secret store.

**Decision:** `protocol.auth.credential_ref` (and the per-action override on
`WSConnectAction`) **names an entry in `cfg.Actors`**. The resolved credential
**value** is obtained through the existing pipeline:

- If the actor has an `AuthFlow`, its login runs once per session and the raw
  token is extracted via `TokenFrom` (the `{token}` substitution source,
  extracted at `authflow.go:138` before `InjectAs` formatting).
- Else the actor's static `Credentials` / env-overlayed values are used.

Injection is uniform across strategies — the resolved **raw token** is placed at
the declared `param`:

- `query` → url query param `param`.
- `header` → dial header `param`.
- `subprotocol` → subprotocol entry `param`.

The raw token is the `{token}` extracted at `authflow.go:138` before `InjectAs`
formatting (`InjectAs` stays HTTP-only — WS protocols want the bare token at the
declared slot, e.g. open-agents `?token=<jwt>`, not `?token=Bearer <jwt>`). A WS
protocol that needs a formatted header (e.g. `Authorization: Bearer …`) is not
expressible in M1 and lands with roles in M2.

Today session setup (`internal/session/auth_setup.go`) caches only the
**formatted** value into `Credentials.Headers`; M1 extends setup to **also cache
the raw token** so `doConnect` reads it without re-running the login
(open-agents opens two connections — re-login per connect would double the auth
calls). A small `ResolveAuthToken`-style read over the cached raw value supplies
`doConnect` for all three strategies.

**Failure mode.** If `protocol.auth` is declared but the value cannot be
resolved (the named actor is missing, has no AuthFlow, or its login failed at
session setup), `doConnect` **fails** with a non-secret error (e.g. `"ws auth:
no token for actor <name>"`) rather than silently connecting unauthenticated —
a declared auth that silently no-ops would let an unauthenticated connection
masquerade as authenticated.

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
    Framing  string        `yaml:"framing,omitempty"`  // "json" only (default); text/binary reserved for M2
    TypePath string        `yaml:"type_path,omitempty"` // dotted path; default "type"
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
      inject_as: "Authorization: Bearer {token}"   # HTTP header form; WS reads the raw token
  - name: bridge-actor
    credentials: { email: ..., password: ... }
    auth:
      login: { method: POST, path: /devices/register, body: { ... } }
      token_from: data.deviceToken
      inject_as: "{token}"
```

- `framing`: `json` only in M1 (default; empty also means json). `text`/`binary`
  are reserved for M2 and rejected by validation until then.
- `type_path`: dotted path to the routing key; default `"type"` (= M0). Empty
  also means top-level `type`.
- `auth.strategy` / `auth.param` / `auth.credential_ref` as in D7.

### type_path resolver

Reuse the dotted-path pattern from `extractByDotPath`
(`internal/head/agent/authflow.go:33`), generalized to take `[]byte`:

```go
// extractTypePath returns the routing key at path within a JSON message.
// path is a dotted set of object keys (no array indexing). Returns ("", false)
// if the message is not a JSON object, the path is absent, or the leaf is not a
// string.
func extractTypePath(data []byte, path string) (string, bool)
```

- `path == ""` or `"type"` → top-level `type` (M0 behavior).
- Dotted keys only; no array indexing, no JSONPath (YAGNI + no evaluator).
- A **non-string leaf** (e.g. `{"type":123}`) returns `("", false)` — no match —
  so the fallback path reproduces M0 `messageType` semantics exactly (the old
  probe struct rejected non-string `type`).
- (Text framing is removed — see Non-Goals; M1 is json-only.)

## Auth Resolution & Secret Hygiene

Connect-time flow (`doConnect`):

1. Resolve the `Protocol` for the dial url's `host:port` from the
   `protocolByHost` index (see *Executor Changes — Threading*). If none → M0
   behavior (no auto-auth, receive uses top-level `type`, json framing).
2. Determine the actor: `WSConnectAction.credential_ref`, else
   `Protocol.Auth.CredentialRef`.
3. Resolve the value: the cached **raw token** for the actor (D7), regardless of
   strategy. If it is absent (declared auth, unresolvable actor), **fail** the
   connect with a non-secret error (D7 failure mode).
4. **Strip** any existing `param` the LLM supplied: remove that query key from
   the url, that header, or that subprotocol entry. (Executor-authoritative —
   D3.)
5. **Inject** the resolved value: `query` → set query param `param` to
   `url.QueryEscape(value)`; `header` → set `param: value`; `subprotocol` →
   append to `Subprotocols`.
6. Store the **pre-injection** url in `WSResult.URL` (secret-free); dial with
   the injected url.
7. Stash the resolved `*Protocol` on the `wsEntry` so `doReceive` can read
   `type_path`/`framing`.

Redaction (D6) applies in `WSResult.Summary()`/`Evidence()` as a defensive
backstop regardless.

## Executor Changes

**Threading (mirrors `NewHTTPExecutorWithServiceHeaders`).** Today the WS
executor is constructed with only a logger:
`&wsPlugin{executor: NewWebSocketExecutor(logger)}`
(`internal/head/agent/plugin_helpers.go:30`). To pass the protocol index in,
**four** signatures in the chain gain a `protocolByHost` param (mirroring how
`serviceHeaders` is already threaded) — or, if preferred, a small options struct
bundling `serviceHeaders` + `protocolByHost` is introduced at the same layer:

```
BuildMultiExecutor(projectDir, serviceHeaders, protocolByHost, gate, logger)   // internal/head/agent/multi.go:93
  └─ BuiltinPluginsWithSandbox(projectDir, serviceHeaders, protocolByHost, sb, gate, logger)  // plugin_helpers.go:21
       └─ BuiltinExecutorPlugins(projectDir, serviceHeaders, protocolByHost, logger)          // plugin_helpers.go:12
            └─ NewWebSocketExecutor(logger, protocolByHost)                                   // websocket.go:37
```

`protocolByHost` is built from `cfg.Services` (host → `*Protocol`, keyed by
`url.Parse(svc.URL).Host` exactly like `ServiceHeadersMap`,
`internal/head/agent/service_headers.go:12`) at the two `BuildMultiExecutor`
call sites: `internal/session/run_phases_agent.go:25` and
`internal/session/resume_phases_run.go:25`. A service with `Protocol == nil`
contributes no entry, so its host falls back to M0 behavior. Host-level
granularity matches the existing HTTP service-header matching; a future need for
path-level matching can extend the index then.

**`wsEntry`** gains `protocol *Protocol` and `readMu sync.Mutex`.

**`doConnect`** — namespaced key (`<caseID>:<connectionID>`), auth resolution +
**strip-then-inject** per *Auth Resolution & Secret Hygiene*, stash protocol,
store pre-injection url in result.

**`doReceive`** — guard `readMu`; decode by `entry.protocol.framing` (json only
in M1); extract routing key by `entry.protocol.type_path` (or top-level `type`
when no protocol); match logic otherwise unchanged from M0 (accumulate
non-matches as evidence).

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
- A raw-token read helper (`ResolveAuthToken`) over the cached raw value in the
  auth path (D7).

**Modified:**
- `internal/project/schema.go` — `Protocol *Protocol` on `Service`.
- `internal/project/validate.go` — call `validateProtocol` as Phase 6.
- `internal/head/agent/websocket.go` — `NewWebSocketExecutor(logger, protocolByHost)`
  signature, `wsEntry` (`protocol`, `readMu`), case-namespaced keys +
  caseID-from-ctx, **strip-then-inject** auth in `doConnect`, `type_path`/
  `framing` in `doReceive`, `readMu` guard.
- `internal/head/agent/multi.go`, `plugin_helpers.go` — thread `protocolByHost`
  through `BuildMultiExecutor` → `BuiltinPluginsWithSandbox` →
  `BuiltinExecutorPlugins` (mirror the existing `serviceHeaders` param).
- `internal/types/actions_http.go` — `WSConnectAction.credential_ref`.
- `internal/types/result_ws.go` — secret redaction in `Summary()`/`Evidence()`.
- `internal/head/agent/authflow.go` — generalize `extractByDotPath` to a shared
  `extractTypePath(data []byte, path string)` (non-string leaf → no match).
- `internal/session/auth_setup.go` — cache the **raw token** (alongside the
  formatted header value) when resolving an actor's AuthFlow, so WS
  query/subprotocol auth reads it without re-login (D7).
- `internal/session/run_phases_agent.go`, `internal/session/resume_phases_run.go`
  — build `protocolByHost` and pass to `BuildMultiExecutor`.
- `internal/head/agent/execute_phases.go` — inject `tc.ID` via
  `context.WithValue` on the per-case ctx (D4).
- `internal/head/agent/prompts.go` — steer prompt (single raw-string literal;
  edit inline) carries a **best-effort hint** that the executor may inject auth
  for declared protocols (so the LLM may omit credentials) and that routing
  follows the declared `type_path`. Token-saving only — correctness does not
  depend on it (the prompt is static; see D3 strip-then-inject).
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
  → no match, non-JSON → no match, **non-string leaf (`{"type":123}`) → no
  match**, empty path → top-level.
- **framing:** json decode + path only (text/binary rejected by validation).
- **auth injection (strip-then-inject):** each strategy (query/header/
  subprotocol) with a stubbed actor-resolution; assert the dial
  url/headers/subprotocols carry the resolved value and `WSResult.URL` does
  **not**. **Plus:** when the LLM-supplied url/headers already contain `param`,
  the executor strips it and injects exactly one resolved value (no duplicate).
- **query encoding:** a value with reserved characters is `url.QueryEscape`'d
  in the query strategy.
- **raw-token caching:** session setup caches the raw token (not only the
  formatted header); two connects on actors that share a login resolve without
  re-running the login.
- **auth-resolution failure:** when `protocol.auth` is declared but the actor has
  no resolvable token, `doConnect` fails (`OK=false`) with a non-secret error and
  does not dial.
- **secret redaction:** `WSResult.Summary()`/`Evidence()` redact `token=…` (and
  denylist) whether or not the executor injected it.
- **per-case namespace:** one executor, two caseIDs via ctx, using the same
  `connection_id` `"c1"` in parallel do not collide; disconnect in one case
  leaves the other's connection intact; ctx cancellation prunes only that case's
  entry.
- **receive serialization:** two concurrent `ws_receive` on one connection
  serialize via `readMu` (no panic/error from `coder/websocket`); `-race` clean.
- **validation:** `validate_protocol_test.go` — framing ∈ {json, empty},
  strategy ∈ {query, header, subprotocol}, type_path shape, param-required-when-
  strategy-set, credential_ref-must-name-an-actor, text/binary-framing-rejected.
- **fallback:** a service with `Protocol == nil` behaves as M0 (top-level type,
  no auto-auth).

Integration against the live open-agents stack is **deferred** (see Open
Questions); the static protocol facts above are encoded as fixture/test shapes.

## Validation Case — open-agents permission-flow (static encoding)

Single case, `Expectation`: "bridge receives a `permission:response` with
`payload.approved == true`". With the `protocol:` block above, the LLM
orchestrates within the case; the executor adapts:

1. `ws_connect { url: "ws://host:8787/ws/{userId}?type=bridge&deviceId=<id>",
   connection_id: "bridge", credential_ref: "bridge-actor" }` — executor strips
   any LLM `token` and injects `&token=<deviceToken>` (strategy=query/param=token).
2. `ws_connect { url: "ws://host:8787/ws/{userId}?type=web",
   connection_id: "web", credential_ref: "web-actor" }` — executor injects
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
  handshake sequences (the role-dependent `devices:sync` step), field-level
  assertions, and `text`/`binary` framing. M1's per-connect `credential_ref` is
  the bridge to M2 roles without depending on them.
- **M3** may extract standalone protocol files and have Scout generate cases from
  descriptions/docs/captures.

## Open Questions

1. **Token-waste magnitude.** How many tokens does the LLM spend re-deriving
   auth/type/framing per run today? Validate post-M1 with a before/after trace
   comparison on the open-agents permission-flow.
2. **Run-to-run drift.** Does the LLM ever mis-place auth or mis-route without a
   declaration? Validate post-M1.
3. **Raw-token cache field.** M1 requires session setup to cache the raw token
   (D7) alongside the formatted header value. The exact field/representation on
   the actor (e.g., a separate `Credentials.RawToken` or a reserved header key)
   is finalized in the plan against the open-agents actor setup (how web obtains
   its JWT, how bridge obtains its deviceToken). The mechanism — cache once, read
   at connect — is fixed here.
4. **`type_path` coverage.** How many real targets route on a non-top-level
   field? open-agents uses top-level; `type_path`'s value is for future targets.
5. **Host vs path granularity.** `protocolByHost` keys on host (matching the
   HTTP service-header pattern, `url.Parse(svc.URL).Host`). If a service ever
   needs per-path protocol selection, the index extends then — deferred as YAGNI.
