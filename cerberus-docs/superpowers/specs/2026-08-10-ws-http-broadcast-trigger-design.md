# Mixed HTTP+WS Steps — Broadcast-Trigger Use Case — Design

**Date:** 2026-08-10
**Branch:** `feat/ws-http-broadcast-trigger`
**Predecessor:** `feat/ws-reqresp-deviceid-payload` (merged 2026-08-10) — `request_payload` + `resolveMessageBody` + `wsSendBody(typ,payload)` closed the two-direction request-response exchange (`gaps:61`, reqresp case `pass/0.97`).

## Problem

The open-agents integration report (`cerberus-docs/technical/validation/2026-08-07-openagents-integration-test-report.md`) lists exactly one "Out of scope" coverage gap: the `/broadcast` HTTP→WS push path — a public HTTP route whose handler calls the Durable Object's internal `/broadcast`, which then fans a message out to WebSocket clients. cerberus's deterministic Steps runner (`internal/head/agent/execute_phases_steps.go` `runSteps`/`stepToAction`) recognizes only `ws_connect`/`ws_send`/`ws_receive`/`ws_disconnect`, so no Steps case can interleave an HTTP request inside a WS sequence to trigger such a push. This is the last uncovered message edge.

Investigation against live open-agents (`apps/api`, `apps/realtime`) reshaped the problem in two ways that this design accounts for:

1. **The fan-out target is web, not bridge.** The DO's `/broadcast` handler (`apps/realtime/src/room.ts:26-34`) routes `multiagent:task_assign` (with `payload.deviceId`) to `sendToBridge` (directed), and **everything else** to `broadcastToWeb` (fan-out to all web clients). The device-restart handler (`apps/api/src/routes/devices.ts:296`) emits `{type:"device:restart"}` — which is *not* `multiagent:task_assign`, so it fans out to web. (The `devices.ts` code comment "sends a restart command to the bridge" contradicts the DO implementation; the implementation is authoritative.) No public HTTP route in `apps/api/src` emits `multiagent:task_assign` at all — the directed-`sendToBridge` path is unreachable from a public route cerberus can hit. So the coverable HTTP→WS push class is the **web fan-out** (`broadcastToWeb`).

2. **The clean triggers are JWT-gated; cerberus's web token is not a JWT.** `POST /api/devices/:id/restart` (and `/api/missions` create, etc.) sit behind the JWT auth middleware (`apps/api/src/worker.ts:259-291`, `verify(token, JWT_SECRET, 'HS256')`). cerberus's web actor authenticates with the static `demo_token` dev backdoor, which is *not* a JWT: the WS path special-cases it (`apps/realtime/src/room.ts:631`), but the HTTP auth middleware does not, so a `Bearer demo_token` POST is rejected `401 Invalid token`. The send-body predecessor feature never hit this because it only opened WS connections.

   This is closeable: open-agents ships a dev-only `POST /api/dev/login` (`apps/api/src/routes/dev.ts:112`) that signs a 30-day JWT with the same `JWT_SECRET`, `sub` = the `userId` that `/api/dev/setup` provisions, using default credentials `dev@openagents.local` / `dev123456` (the same password setup hashes and returns). So an authflow that calls setup then dev-login obtains a real JWT that authorizes the protected web routes — the device-restart trigger becomes reachable.

A dead end, recorded for honesty: the unauthenticated internal route `POST /api/missions/internal/planner/decompose` does broadcast (`mission:decomposed`), but it requires a pre-existing mission row in `decomposing` status, and creating that row is itself a JWT-gated route; cerberus cannot write to open-agents' D1. Not usable.

## Goal

Add a deterministic `http_request` step to the Steps runner so a WS sequence can interleave an authenticated HTTP request that triggers a server push received on an open WS connection; declare the trigger in the protocol; generate the case deterministically; and prove it end-to-end against live open-agents, closing the `/broadcast` HTTP→WS coverage gap.

## Success Criteria

1. A `http_request` step in a Steps case performs an HTTP request whose URL/body `{{role.param}}`/`{{param}}` placeholders resolve at runtime from provisioned actor state, whose `auth_role` resolves the bound actor's HTTP credential into `Authorization: Bearer <token>`, and whose response status is checked against `expect_status` (if set). An unresolvable placeholder or missing HTTP token fails the step with a clear error.
2. The web actor's authflow, when it declares an `http_login`, performs that second login after the primary login and captures the resulting token into a new index slot (`ActorHTTPTokens`), distinct from the WS token (`ActorTokens`).
3. A live `//go:build integration` Steps case opens a web WS connection, posts `POST /api/devices/<bridge-deviceId>/restart` authenticated as the web actor (JWT, obtained via `/api/dev/login`), and receives `device:restart` on the web WS connection.
4. An autonomous `cerberus run` against the `ws-realtime` dogfood generates and executes the deterministic `device-restart` trigger case; the case verdict and the honest `coverage assessment` line are recorded from the run.
5. Zero regression: protocols/roles without `http_triggers` and actors without `http_login` produce byte-identical auth resolution, case generation, and WS sends.

## Decisions

### D1 — Resolution lives in the runner (Option A), not the HTTPExecutor

An `http_request` step needs two capabilities the generic `HTTPExecutor` lacks: `{{role.param}}` placeholder resolution and actor-credential injection. Both depend on `WSProtocolIndex` (`ActorPathParams`, `ActorTokens`, and the new `ActorHTTPTokens`), which today lives only on `WebSocketExecutor`. The `http_request` step is deterministic-Steps-only in this work — the LLM assembly path (`assembly.go`) does not author it — so polluting the generic `HTTPExecutor` (shared with LLM `api_request`/`navigate`) with protocol/role concerns is unjustified. Instead the runner resolves: `ReActLoop` gains a `wsIdx` field (already available at construction; the multi-executor receives it), and `runSteps` calls a new `resolveHTTPStep(idx, proto, step)` to produce a `types.HTTPAction` before dispatch. The multi-executor routes `HTTPAction` to the existing `HTTPExecutor` unchanged.

Rejected: resolving at the `HTTPExecutor` boundary (mirrors the send-body spec's D1, but the shared-executor cost is not worth it for a Steps-only feature).

### D2 — Placeholder resolution is shared with ws_send

`resolveMessageBody(entry, msg)` (send-body feature) and the `http_request` URL/body resolver share identical `{{param}}`/`{{role.param}}` semantics. The core substitution is extracted to a free function `resolvePlaceholders(idx, proto, owningActor, text) (string, error)`; `resolveMessageBody` calls it with `owningActor = entry.credentialRef` (behavior identical — guarded by the existing resolver unit tests), and `resolveHTTPStep` calls it with `owningActor = step.AuthRole`'s `CredentialRef`. Same double-brace syntax, same hard-fail-on-unresolved contract, same "undeclared role left literal" rule.

### D3 — A second login (`http_login`) captures the HTTP JWT; a new index slot stores it

The web actor needs two credentials: `demo_token` for the WS query param and a JWT for HTTP Bearer. The current `AuthFlow` produces one token per actor (→ `Credentials.RawToken` → `idx.ActorTokens`). Extending it:

- `AuthFlow` gains `HTTPLogin *AuthLogin` + `HTTPTokenFrom string` (a second login run *after* the primary login, capturing its own token by dot-path).
- `ResolveAuthHeader` returns a new `HTTPToken` field when `HTTPLogin` is set.
- `auth_setup.go` writes `a.Credentials.RawHTTPToken = res.HTTPToken`.
- `WSProtocolIndex` gains `ActorHTTPTokens map[string]string`, filled from `Credentials.RawHTTPToken` (only when non-empty).
- `http_request` `auth_role` resolution: `proto.Roles[authRole].CredentialRef` → `idx.ActorHTTPTokens[actor]` → `Authorization: Bearer <token>`. An actor with no `http_login` has no HTTP token → clear step failure.

`http_login` reuses the existing `AuthLogin` shape (method/path/body/headers). The dev-login server defaults to `dev@openagents.local`/`dev123456`, so `body: {}` suffices.

Rejected: making `login` a list (reopens `inject_as`/`path_params` binding per login for no gain); a general named-token map (churn).

### D4 — CSRF does not block the trigger

`csrfProtection` (`apps/api/src/middleware/security.ts:38-44`) passes any POST carrying `Authorization: Bearer ` even without an `Origin` header. Because `device:restart` requires Bearer auth anyway, the `http_request` step needs no `Origin` configuration. (The `http_login` POST itself is under `/api/dev/` and uses the same `Origin` header the primary login already declares.)

### D5 — Trigger declared at Protocol level as `http_triggers`

A trigger is a relationship between an HTTP route and a WS message effect — not a property of one role — so it lives at the `Protocol` level, following the sibling-field convention (optional; absent ⇒ byte-identical behavior):

```yaml
http_triggers:
  - id: device-restart
    request:
      method: POST
      path: /api/devices/{{bridge.deviceId}}/restart   # host-relative
      auth_role: web
      expect_status: 200
    effect:
      message_type: device:restart
      to_role: web
```

- `request.path` is host-relative; the generator prefixes the service host (`scheme://host`, derived from the service URL by stripping the WS path such as `/ws/{userId}`).
- `request.auth_role` and `effect.to_role` must each name a declared role (validated).
- `effect.message_type` is the type the `to_role` connection receives.

`HTTPTrigger` is a struct: `ID`, `Request{Method,Path,AuthRole,ExpectStatus}`, `Effect{MessageType,ToRole}`.

### D6 — Generator emits connect → http_request → receive

`wsHTTPTriggerCases` (new, `internal/head/scout/ws_cases.go`) emits one Steps case per trigger:

1. `ws_connect` the `effect.to_role` connection (so the broadcast has a recipient).
2. `http_request` with the templated URL, `auth_role`, `expect_status`.
3. `ws_receive effect.message_type` on the `to_role` connection (the decisive assertion; placed last).

The `{{bridge.deviceId}}` placeholder resolves from the bridge actor's provisioned `ActorPathParams` (provisioning is session-wide; the bridge need not open a WS connection for the device row to exist). `broadcastToWeb` is synchronous within the DO `/broadcast` fetch, so the frame is buffered by the web connection's read pump before the following `ws_receive` polls. The web role's optional `device:online` handshake (5 s) is accepted as a per-case delay when no bridge connects; connecting the bridge first is an optional generator refinement, not required for v1.

### D7 — Decisive verdict and evidence

`stepEvidence` gains an `http_request` branch recording status/URL as a generic evidence entry. The generator places `ws_receive` last so the decisive verdict is the message arrival (not the HTTP status). A hand-authored case may end on `http_request`; its verdict is then the status check — acceptable.

## Architecture

```
authflow (session setup)                  scout (plan time)               runner (run time)
──────────────────────                    ────────────────                ─────────────────
setup → userId/deviceId/demo_token        wsHTTPTriggerCases              runSteps:
http_login → JWT                          reads proto.HTTPTriggers          ws_connect (to_role)
→ Credentials.RawHTTPToken                  emits Steps case                 http_request → resolveHTTPStep(idx,proto,step)
→ idx.ActorHTTPTokens[web-actor]                                               · resolvePlaceholders(URL/body)
                                                                               · auth_role → idx.ActorHTTPTokens → Bearer
                                                                             → HTTPAction → HTTPExecutor
                                                                             ws_receive message_type (decisive)
```

The placeholder is inert in the generated step text; it is carried verbatim through the case and only materialized against the index at the runner boundary. The plan stays serializable and deterministic; only the runner knows provisioned values (consistent with the send-body design).

## Components

| Unit | Responsibility | Depends on |
|---|---|---|
| `resolvePlaceholders(idx, proto, owningActor, text)` (new free fn, `websocket.go`) | Resolve `{{param}}`/`{{role.param}}` in a URL or body; hard-fail on unresolved. | `idx`, `proto.Roles` |
| `resolveHTTPStep(idx, proto, step)` (new, `execute_phases_steps.go`) | Build `types.HTTPAction`: resolve placeholders, inject `auth_role` Bearer, apply explicit `Headers`, set `ExpectStatus`. | `resolvePlaceholders`, `idx.ActorHTTPTokens` |
| `stepToAction` `case "http_request"` (edit) | Dispatch to `resolveHTTPStep`. | — |
| `ReActLoop.wsIdx` (new field) | Give the runner access to the index. | set at construction |
| `TestStep` fields (edit, `types.go`) | `Method`, `Headers`, `Body`, `ExpectStatus`, `AuthRole`. | — |
| `AuthFlow.HTTPLogin` / `HTTPTokenFrom` (new, `authflow_schema.go`) | Declare the second login. | — |
| `Credentials.RawHTTPToken` (new, `credentials.go`) | Store the captured JWT. | — |
| `ResolveAuthHeader` (edit, `authflow.go`) | Run `HTTPLogin` after primary login; return `HTTPToken`. | — |
| `WSProtocolIndex.ActorHTTPTokens` (new, `ws_protocol.go`) | Index the HTTP token per actor. | `Credentials.RawHTTPToken` |
| `Protocol.HTTPTriggers` + `HTTPTrigger` (new, `protocol_schema.go`) | Declare triggers. | — |
| `validateProtocolHTTPTriggers` (new, `validate_protocol.go`) | Validate trigger shape + role refs. | wired into protocol validation |
| `validate_auth` (edit) | Require `http_token_from` iff `http_login` set. | — |
| `wsHTTPTriggerCases` (new, `ws_cases.go`) | Generate the connect→http→receive case. | `proto.HTTPTriggers` |

## Error Handling

- Unresolved URL/body placeholder → `http_request: unresolved placeholder {{bridge.deviceId}}`.
- `auth_role` whose actor has no HTTP token (no `http_login`) → `http_request: no http token for actor "web-actor"`.
- `expect_status` mismatch → step fails naming expected vs actual status.
- `auth_role`/`to_role` referencing an undeclared role → caught at config validation, not runtime.
- `http_login` failure → the actor degrades to unauthenticated (existing authflow degrade behavior); a later `http_request` then fails clearly on the missing token. The WS path (which uses `ActorTokens`) is unaffected.

## Testing

1. **Unit — resolver:** `resolvePlaceholders` cross-actor URL resolution; owning-actor `{{param}}`; undeclared role left literal; declared-role placeholder with no value hard-fails; no-placeholder text unchanged; marshaled body stays valid JSON. (Re-run the existing send-body resolver tests against the refactored free function — they must pass unchanged.)
2. **Unit — `resolveHTTPStep`:** `auth_role` → Bearer injection; explicit `Headers` override the injected header; `ExpectStatus` honored; missing HTTP token fails clearly.
3. **Unit — authflow:** `HTTPLogin` runs after primary login and populates `HTTPToken`; without `HTTPLogin`, `HTTPToken` is empty and `ActorTokens` is unchanged (no regression).
4. **Unit — validation:** valid `http_triggers` passes; undeclared `auth_role`/`to_role` fails; empty id/method/path fails; `http_token_from` required iff `http_login` set.
5. **Unit — generator:** with one trigger, the case is `{ws_connect web, http_request POST .../restart auth_role=web, ws_receive device:restart@web}`; with no `http_triggers`, case generation is byte-identical to today.
6. **Integration (`//go:build integration`):** a hand-authored Steps case — helper calls `/api/dev/login` to obtain a JWT and sets it on the step's `Headers`; web `ws_connect`; `http_request` restart; `ws_receive device:restart` — passes against live open-agents. Mirrors `TestSessionStartRoundTrip`.
7. **Autonomous live proof:** `cerberus run` against `dogfood/ws-realtime`; record the honest `coverage assessment`/`gaps`/`coverage_pct` line and the `device-restart` case verdict.

### Honest coverage caveat (success criterion 4)

`device:restart` is emitted by the HTTP route (`devices.ts`), not by a DO WS handler, so it is unlikely to be a declared edge in the WS vocab. Receive-driven path attribution credits an edge only when it is declared; therefore the case can **pass** (capability proven — the hard success criterion) while `coverage_pct` may **not** rise. Whether `coverage_pct` moves depends on whether `device:restart` is a declared vocab edge — verified live, not assumed. The two outcomes are reported separately; `coverage_pct` is not the sole success bar.

## Out of Scope

- Directed `sendToBridge` via `multiagent:task_assign` — unreachable from any public HTTP route cerberus can authenticate to.
- The `/api/missions/internal/*` unauthenticated broadcast route — unusable (requires a JWT-created precondition).
- Teaching the LLM `assembly.go` path to author `http_request` steps (it stays ws-only; a separate LLM-prompting change).
- A general named-credential map or per-login `inject_as`/`path_params` (D3 rejected alternatives).
- Additional triggers (mission/orchestrator web fan-out) — the `http_triggers` declaration generalizes to them, but only `device-restart` is built and verified in this work.

## File Structure

- `internal/project/authflow_schema.go` — `AuthFlow.HTTPLogin` / `HTTPTokenFrom`.
- `internal/project/credentials.go` — `Credentials.RawHTTPToken`.
- `internal/project/validate_auth.go` — `http_login`/`http_token_from` consistency check + test.
- `internal/project/protocol_schema.go` — `Protocol.HTTPTriggers`, `HTTPTrigger` (+ request/effect sub-structs).
- `internal/project/validate_protocol.go` — `validateProtocolHTTPTriggers` + test.
- `internal/head/agent/authflow.go` — `ResolveAuthHeader` runs `HTTPLogin`, returns `HTTPToken` + test.
- `internal/head/agent/ws_protocol.go` — `WSProtocolIndex.ActorHTTPTokens`; fill in `BuildWSProtocolIndex`.
- `internal/head/agent/websocket.go` — extract `resolvePlaceholders`; `resolveMessageBody` delegates.
- `internal/head/agent/execute_phases_steps.go` — `case "http_request"`, `resolveHTTPStep`; `stepEvidence` http branch + tests.
- `internal/head/agent/types.go` — `TestStep` new fields.
- `internal/head/agent/executor_types.go` — `ReActLoop.wsIdx` field; set at the construction site that already holds `wsIdx` (alongside `BuildMultiExecutor`/`BuiltinExecutorPlugins`).
- `internal/head/scout/ws_cases.go` — `wsHTTPTriggerCases` + test.
- `dogfood/ws-realtime/.cerberus/project.yaml` — web-actor `http_login` + `http_token_from`.
- `dogfood/ws-realtime/.cerberus/protocols/open-agents.yaml` — `http_triggers: [device-restart]`.
- `internal/head/agent/httptrigger_live_integration_test.go` — hand-authored device-restart live proof (follows `pathcoverage_live_integration_test.go` naming).
- `cerberus-docs/technical/validation/2026-08-07-openagents-integration-test-report.md` — append the autonomous device-restart re-verification.
