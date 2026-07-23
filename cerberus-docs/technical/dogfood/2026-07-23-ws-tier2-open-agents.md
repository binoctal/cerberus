# WS Tier-2 — Real Target (open-agents) — 2026-07-23

Validated the released WS engine (M0–M3-2 + quickwins 2+3 + A + **C deterministic
Steps**) against a real, undocumented WebSocket target — `open-agents`
(`github.com` sibling at `/home/mason/Documents/code_projects/private/open-agents`),
a SaaS for managing AI CLI tools with a Cloudflare Workers + Durable Object
realtime layer. Goal: confirm the engine reaches a real target and surface
cross-project reuse signals + the M3-3 trigger.

## Setup (autonomous; no Supabase/OAuth/secrets needed)

- `fnm use 22` (wrangler v4.95 requires Node ≥22; v20 is the default), then
  `cd apps/api && npm run dev` (`wrangler dev --port 8989`). Local D1 sqlite is
  already migrated; `.dev.vars`/`wrangler.toml` ship dev values. Up in ~1s.
- **Stale-doc note:** `docs/DEV_MODE.md` and `apps/api/CLAUDE.md` say `:8787`;
  the real port is **`:8989`** (hardcoded `apps/api/src/worker.ts:167`).
- Auth backdoor: `?type=web&token=demo_token` is accepted in development
  (`realtime/room.ts` `verifyToken`), so no login is required to reach the WS
  layer. A bridge client needs a `POST /api/dev/setup`-issued `token_…`.

## Empirical result (direct WS client against the live target)

```
ws://localhost:8989/ws/demo_user?type=web&token=demo_token
  → OPEN (HTTP upgrade + dev-token auth succeeded)
  → 0 messages received in 2.5s
```

The connect + auth chain works on a real target. The **0 messages** confirms the
protocol's defining wrinkle (below).

## The open-agents realtime protocol (discovered, undocumented)

- **Envelope:** every frame both directions is `{type, payload, timestamp}`.
- **Two-socket relay via a `UserRoom` Durable Object:** a *web* client and a
  *bridge* client connect to `/ws/{userId}`; web→bridge messages are forwarded
  through the DO, bridge→web messages broadcast to the user's web clients. There
  is no single-socket request/response — every exchange is web↔DO↔bridge.
- **Conditional handshake:** on a web connect, the server sends `devices:sync`
  only if a bridge socket is currently online for that user. A lone web client
  gets **silence** — there is no welcome/HELLO. (Empirically confirmed above.)
- **Large type vocabulary:** web→bridge (`session:start/stop/cancel`,
  `chat:send`, `permission:response`, `session:send` …); bridge→web
  (`session:created/started/stopped/message`, `chat:response`, `permission:request` …).
- **Batching:** `session:output` is coalesced into `session:output-batch`
  (`{payload:{lines:[…]}}`) every 50ms — a different type + shape.
- **No generic ACK;** replies are asynchronous `session:*`/`chat:*` frames.
- **Dynamic, user-scoped URL:** the connection path embeds the userId
  (`/ws/{userId}`).

## cerberus-fit findings (the Tier-2 signal)

C's deterministic `Steps` model is **single-connection connect→send→receive→assert
with a handshake-await-on-connect and a static service URL**. It fits simple
RPC-style WS protocols (the Tier-1 dogfood `device:command`/`device:ack` exchange
**PASSes** deterministically post-C). Against open-agents it surfaces real gaps:

1. **Conditional/peer-dependent handshake.** cerberus's role `handshake.await_type`
  reads a message on connect; open-agents only sends `devices:sync` when a bridge
  peer is online, so a lone web client's await **times out**. The dogfood sent
  its handshake unconditionally; real targets gate it on peers.
2. **Multi-party relay (two connections).** A web↔bridge exchange needs TWO
  sockets relayed through the DO. C's one-case-one-connection `Steps` cannot
  express "connect web + connect bridge, send from web, receive the relayed reply
  on web." This is the same class as the M3-2 `DependsOn`-is-ordering-only limit,
  extended to *cross-socket* orchestration.
3. **Dynamic URL parameter.** `/ws/{userId}` embeds a runtime id. cerberus's
  `svc.URL` is static and auth injects only the token; a URL-path id must be
  pre-provisioned and baked in (workable, not seamless).
4. **Message batching.** A `ws_receive` awaiting `session:output` would miss the
  emitted `session:output-batch` (different `type`, nested `payload.lines`).
5. **Envelope nesting.** Field asserts must reach `payload.*` (supported by the
  dotted-path asserts; richer than the dogfood's flat shape, no blocker).

None of these are bugs in C — they delimit where C's current model (simple
single-socket RPC) applies and where extensions are needed for real-world
multi-party realtime protocols.

## M3-3 trigger — NOW justified

Authoring a cerberus protocol declaration for this *undocumented* target required
discovering: the envelope shape, the conditional/peer-gated handshake, the
two-socket relay, the type vocabulary, and the output batching — by reading
source (`realtime/room.ts`) and test scripts, not from any spec. That is exactly
the blank-page cost `cerberus protocol infer` (M3-3) exists to remove. **Unlike
Tier-1** (where the author wrote both sides), Tier-2 shows real discovery cost.
**M3-3 is trigger-justified.**

## Future work surfaced (not in C's scope)

- Multi-connection / cross-socket case orchestration (web + bridge sharing a
  scenario) — the natural extension of `Steps` past one connection.
- Conditional or peer-dependent handshakes (await only-if-peer, or no-await
  connect).
- Dynamic URL/path-parameter injection from the auth result.
- Message-batching / type-aliasing awareness in `ws_receive`.

## Outcome

- cerberus reaches a real undocumented WS target end-to-end: dial + dev-token
  auth + upgrade succeed on live traffic (the integration gap Tier-1 could not
  test, since Tier-1's target was self-authored).
- C's deterministic `Steps` is validated for single-socket RPC protocols
  (Tier-1 dogfood `device:ack` PASS); real multi-party relay protocols need the
  extensions above.
- **M3-3 (`protocol infer`) is now trigger-justified** by the real discovery
  cost observed here.

## Confirmed

- `wrangler dev` on local D1 + DO (no Supabase/OAuth) is a viable, repeatable
  Tier-2 harness; `demo_token` reaches the WS layer without a login.
- The conditional handshake + two-socket relay are real protocol properties
  (empirically: lone web client → upgrade OK, 0 frames), not cerberus defects.
