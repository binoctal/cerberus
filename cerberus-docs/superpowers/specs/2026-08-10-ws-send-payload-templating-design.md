# Generic WS Send Payload Templating — Design

**Date:** 2026-08-10
**Branch:** `feat/ws-reqresp-deviceid-payload`
**Predecessor:** `feat/ws-autonomous-coverage` (merged 2026-08-10) — closed autonomous `coverage_pct > 0` (1/63) but the reqresp exchange completed only 1 of 2 directions.

## Problem

An autonomous `cerberus run` against the `ws-realtime` dogfood reports an objective but tiny message-edge path coverage (`1/63`). The deterministic two-role request-response case (`wsRequestResponseCases`) opens both sockets and the bridge replies, but **only one of the two exchange edges is exercised**. Root cause, verified against live open-agents (`apps/realtime/src/room.ts:247-252`):

```ts
case 'session:start':
  if (meta.type === 'web') {
    const payload = msg.payload as { deviceId?: string };
    if (payload.deviceId) {
      this.sendToBridge(payload.deviceId, msg);   // <-- routes by payload.deviceId
    }
  }
```

open-agents is a **push relay that routes by a payload field**. A web-origin `session:start` with no `payload.deviceId` is silently dropped — the bridge never receives it, so edge `(web→bridge, session:start)` is never exercised, and the bridge's `session:created` reply (the one edge that does fire today) is the only hit. The deterministic `wsSendBody` emits a minimal `{"type":"<T>"}` envelope with no payload, so the routing field is always absent.

This is not open-agents-specific. Routing-by-payload-field is the common shape of WS relay protocols; cerberus's deterministic WS cases cannot currently express it at all. The capability gap is general.

## Goal

Let any deterministic `ws_send` step carry a **templated payload** whose placeholders resolve at runtime from provisioned actor state, so a two-role exchange that routes by a peer's provisioned id completes in both directions. Producer-agnostic: the reqresp generator is the first user, but the resolution mechanism is shared by every `ws_send`.

## Success Criteria

1. A `ws_send` whose `Message` contains `{{role.param}}` / `{{param}}` placeholders has them resolved against captured path params before the frame is written; an unresolved placeholder fails the step with a clear error.
2. The reqresp case for `session:start → session:created` completes **both** directions against live open-agents: bridge receives `session:start` AND web receives `session:created`.
3. Autonomous `cerberus run` against `ws-realtime`: `coverage assessment` gaps drop below 63 (≥2 message edges exercised), and the reqresp case verdict improves from `fail` toward `pass` (Examiner correctness rises materially). The exact number is recorded honestly from the run, not asserted.
4. Zero regression: protocols/roles without `request_payload` and messages without placeholders produce byte-identical sends and identical case generation.

## Decisions

### D1 — Resolution is generic and lives in the executor (`doSend`)

deviceId is provisioned at runtime (per-actor `/api/dev/setup`); it is unknown at plan time when scout generates cases. Therefore placeholder resolution **must** happen at send time. `doSend` is the universal wire boundary: it already holds the connection `entry` (which carries the connection's `protocol`) and `e.idx.ActorPathParams` (every actor's captured params, keyed by actor name). Resolving there makes the mechanism producer-agnostic — reqresp, relay, and any future LLM-authored send all pass through the same resolver.

Small supporting change: the connection entry currently stores only the `protocol`; it must also store the `credentialRef` that opened it, so an owning-actor `{{param}}` (and the role→credentialRef lookup for `{{role.param}}`) can resolve. `doConnect` already computes `credentialRef`; it just does not persist it.

Rejected: resolving in `runSteps`/`stepToAction` — those live in the agent package and have no access to the executor's `idx`.

### D2 — Placeholder syntax

Placeholders use **double braces** `{{...}}`, not single. A ws_send `Message` is JSON; single-brace `{...}` would collide with JSON object braces and make reliable matching impossible. Double-brace is also consistent with the existing `{{uuid}}` role-param sentinel (`resolveRoleParamValue`).

- `{{param}}` resolves from the **connection-owning actor's** captured path params (the `credentialRef` that opened this connection).
- `{{role.param}}` is **role-qualified** (dot splits role name from param). Resolves as `proto.Roles[role].CredentialRef → e.idx.ActorPathParams[credRef][param]`. This is the cross-actor form required when a sender needs a peer's id (web needs bridge's deviceId).
- The dot form is only interpreted when the text before the dot names a declared role; otherwise the token is left literal (no accidental partial substitution). The placeholder scanner matches only `{{[A-Za-z0-9_.]+}}`, so JSON braces never match.
- **Unresolved placeholder ⇒ hard step failure** naming the placeholder, following `resolveURLParams`'s "clear failure over a silent wrong send". A message with no placeholders is sent verbatim.

`{{param}}` / `{{role.param}}` apply to the send body; the existing connect-URL `{name}` (single brace) applies to the dial URL. The two never overlap (URL vs body), and each is scoped to its own substitution pass.

### D3 — Template source for the reqresp generator: an optional `request_payload` sibling field

Generic resolution (D1) makes the *mechanism* shared, but the generator still needs to know "a `session:start` to the bridge must carry `payload.deviceId`". Three sources were considered:

- **Hardcode the field name in `wsRequestResponseCases`** — rejected: leaks an open-agents-specific routing rule into a general generator.
- **Change `Responses` from `map[string]string` to a struct carrying the payload** — rejected: schema migration + backward-compat + extra validation for narrower benefit.
- **Add an optional sibling field `request_payload` on `ProtocolRole`** — **chosen.** Does not touch the existing `responses` map (no migration), is opt-in, and keeps protocol-specific knowledge in the protocol declaration where it belongs.

```yaml
bridge:
  credential_ref: bridge-actor
  params:
    type: bridge
    deviceId: "{deviceId}"
  responses:
    session:start: session:created
  request_payload:            # NEW — optional, map[received_type]map[field]template
    session:start:
      deviceId: "{{bridge.deviceId}}"
```

Semantics: when the reqresp generator builds the **requester's** send of `recvType` to responder role `R`, it looks up `R.RequestPayload[recvType]`. If present, the send body is `{"type":<recvType>,"payload":{<field>:<template>,...}}` with placeholders resolved at send time; if absent, the body is the current `{"type":<recvType>}` (byte-identical). The generator knows the responder role name at emit time, so the role part of `{{bridge.deviceId}}` could be written generically — but the **field name** (`deviceId`) is protocol-specific and must come from the declaration.

`request_payload` is `map[string]map[string]string`: outer key = received message type, inner = payload field → placeholder template. Validation (D5): non-empty type token; field names non-empty.

### D4 — `wsSendBody` gains an optional payload

`wsSendBody(typ string) string` becomes `wsSendBody(typ string, payload map[string]string) string`. With a non-nil/non-empty payload it marshals `{"type":<typ>,"payload":<payload>}`; with an empty payload it returns `{"type":<typ>}` exactly as today. All existing call sites pass `nil` (or are updated to pass the declared payload) — zero behavioral change where no payload applies. (The LLM-authored `ws_send` path in `assembly.go` passes `nil`.)

### D5 — Validation

`request_payload` validated per-role alongside the existing `responses` validator (`validateProtocolResponses`):
- outer key (received type) non-empty;
- inner field name non-empty;
- placeholder templates are NOT validated for resolvability at config-load time (resolution depends on runtime provisioning); a bad placeholder surfaces as a clear step failure at send time (D2).

## Architecture

```
scout (plan time)                         agent executor (run time)
─────────────────                         ────────────────────────
wsRequestResponseCases                    doSend(action)
  reads role.RequestPayload[recvType]       entry := lookup(conn)           // has protocol + credentialRef
  → wsSendBody(recvType, payload)           msg  := resolveMessageBody(entry, action.Message)
       = {"type":T,"payload":{..:{{role.p}}}}    // {{param}}←owning actor, {{role.param}}←role's actor
                                                 // unresolved ⇒ hard fail
                                             conn.Write(msg)
```

Data flow: the placeholder is inert in the generated `Message` string; it is carried verbatim through `stepToAction → WSSendAction.Message → doSend` and only materialized against `e.idx.ActorPathParams` at the wire boundary. This keeps the plan serializable and deterministic; only the executor knows provisioned values.

## Components

| Unit | Responsibility | Depends on |
|---|---|---|
| `resolveMessageBody(entry, msg) (string, error)` (new, `internal/head/agent/websocket.go`) | Resolve `{{param}}` / `{{role.param}}` in a send body; hard-fail on unresolved. | `entry.protocol`, `entry.credentialRef`, `e.idx.ActorPathParams` |
| `connEntry.credentialRef` (new field) | Record which actor opened the connection. | set in `doConnect`, stored via `store` |
| `ProtocolRole.RequestPayload map[string]map[string]string` (new field, `internal/project/protocol_schema.go`) | Declare the payload template a requester must include per received type. | — |
| `validateProtocolRequestPayload` (new, `internal/project/validate_protocol.go`) | Validate the declaration shape. | wired into the per-role loop |
| `wsSendBody(typ, payload)` (signature change, `internal/head/scout/ws_cases.go`) | Build the envelope, with optional payload. | existing callers updated |
| `wsRequestResponseCases` (edit) | Emit requester send with declared payload template. | `role.RequestPayload` |

## Error Handling

- Unresolved placeholder at send time → step fails with `ws send: unresolved placeholder {{bridge.deviceId}}` (clear, names the token).
- Missing connection (send on unknown id) → unchanged existing error.
- `request_payload` references a role that is not declared → the dot form is left literal at resolution (no declared role match); since it is not a recognized placeholder it passes through as-is — consistent and debuggable.
- Non-JSON-safe resolved values (e.g. containing quotes): resolution substitutes the captured value directly into the marshaled body via `regexp` string replacement (`ReplaceAllStringFunc` over the pre-marshaled JSON). JSON validity is preserved because captured path params are path-safe identifiers — the same authflow capture contract that `resolveURLParams` already relies on; a value containing quotes or backslashes would break the surrounding JSON, so capture is intentionally limited to ids.

## Testing

1. **Unit — resolver:** owning-actor `{{param}}` resolves; cross-actor `{{role.param}}` resolves from a different actor's params; unknown role left literal; declared-role/owning-actor placeholder with no value hard-fails; no-placeholder message returned unchanged; the JSON of the marshaled body stays valid after substitution (JSON braces never match).
2. **Unit — generator:** with `request_payload` declared, the requester's send step Message is `{"type":"session:start","payload":{"deviceId":"{{bridge.deviceId}}"}}`; without it, byte-identical to today.
3. **Unit — validation:** valid `request_payload` passes; empty type token / empty field name fail.
4. **Integration (`//go:build integration`):** extend or mirror `TestPathCoverage_LiveTwoRoleExchange` — web sends `session:start` carrying the bridge's deviceId (via the templated body), bridge receives it and replies `session:created`, web receives the reply; assert both `web|bridge|session:start` and `bridge|web|session:created` are exercised (path coverage strictly greater than the one-direction baseline).
5. **Autonomous live proof:** `cerberus run` against live open-agents on `dogfood/ws-realtime`; record the honest `coverage assessment` line and the reqresp case verdict.

## Out of Scope

- Changing the `Responses` value type (D3 rejected alternative).
- Teaching LLM-authored `ws_send` (assembly.go) to emit payloads — it passes `nil` and is unaffected; wiring it to a payload source is a separate, LLM-prompting change.
- The `/broadcast` HTTP→WS endpoint (the other documented uncovered item; different capability class).
- General JSON-expression encoding of captured values (capture contract stays path-safe identifiers).

## File Structure

- `internal/project/protocol_schema.go` — add `RequestPayload` to `ProtocolRole`.
- `internal/project/validate_protocol.go` — add `validateProtocolRequestPayload`, wire into role loop.
- `internal/project/validate_protocol_test.go` — validation test.
- `internal/head/agent/websocket.go` — `resolveMessageBody`; `connEntry.credentialRef`; store it in `doConnect`; call resolver in `doSend`.
- `internal/head/agent/websocket_test.go` — resolver unit test.
- `internal/head/scout/ws_cases.go` — `wsSendBody(typ, payload)`; `wsRequestResponseCases` emits declared payload; update all `wsSendBody` call sites.
- `internal/head/scout/ws_cases_test.go` — generator payload test.
- `dogfood/ws-realtime/.cerberus/protocols/open-agents.yaml` — add `bridge.request_payload`.
- `internal/head/agent/pathcoverage_live_integration_test.go` — live two-direction proof.
- `cerberus-docs/technical/validation/2026-08-07-openagents-integration-test-report.md` — append the autonomous two-direction re-verification.
