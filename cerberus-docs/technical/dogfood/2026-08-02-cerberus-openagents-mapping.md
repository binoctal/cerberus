# cerberus ↔ open-agents — Test Mapping & Coverage — 2026-08-02

How cerberus tests `open-agents`, grounded in the real open-agents source
(sibling repo `../open-agents`). Supersedes the protocol guesses in the earlier
yaml configs; this is the empirical mapping after the 2026-07-23 dogfood run.

## Target architecture (real code)

```
client ──ws──► Worker (apps/api/src/worker.ts) ──► UserRoom DO (apps/realtime/src/room.ts)
                 /ws/{userId} route + bridge DB auth        hibernatable DO, relays web↔bridge
```

open-agents shards by `userId`: a `web` client and a `bridge` device connect to
the same `/ws/<userId>` and land in the **same `UserRoom` Durable Object**, which
relays messages between the two roles (`broadcastToWeb` / `sendToBridge`).

## The four facts cerberus dogfood pinned down ↔ real code

| # | Dogfood finding | Real open-agents code |
|---|---|---|
| 1 | `/ws/{userId}` routing, shared DO | `worker.ts:349` (`pathname.startsWith('/ws/')`, `userId = split('/')[2]`) → DO |
| 2 | Bridge auth needs `deviceId` + `token` (DB-validated) | `worker.ts:362` `SELECT id FROM devices WHERE id=? AND user_id=? AND device_token=?` (the cerberus doc's "worker.ts:359-367" cite) |
| 3 | The peer-join relay signal is `device:online`, not `devices:sync` | `room.ts:91-95` — on bridge connect, DO `broadcastToWeb({type:'device:online',...})` |
| 4 | Minimal `session:start` is dropped (no relay) | `room.ts:248` — Web→Bridge routing keys on `payload.deviceId`; without it the message is silently dropped, so no `session:created` ever returns |

### Why `devices:sync` was wrong (and is now fixed)

`devices:sync` is real (`room.ts:105-128`), but it is sent to a web client **only
on connect, and only if a bridge is already online** (`room.ts:117`
`if onlineDevices.length > 0`). In the standard test order (web connects first,
bridge second) the web client gets silence on connect; the signal that actually
fires on bridge-join is `device:online`. Both `open-agents.yaml` configs awaited
`devices:sync` and only survived because of `optional: true` — the await target
was mis-aimed. Both yamls now await `device:online`.

## Two parallel cerberus test configurations

| | cerberus repo integration test | open-agents own `.cerberus` |
|---|---|---|
| Entry | `TestRunStepsMultiConnectionOpenAgents` (Go, `//go:build integration`) | cerberus CLI smoke |
| Provisioning | **dynamic** — `devSetup()` calls `/api/dev/setup`, reads real `userId/deviceId/deviceToken` | **static** — hardcoded `demo_user` + `token_d0d0…` (32 hex) |
| web auth | `demo_token` dev backdoor | `demo_token` dev backdoor |
| bridge auth | real DB row token from `/api/dev/setup` | format-valid static token (`token_` + ≥32 chars passes `room.ts:350-358`) |

The dev backdoor is `worker.ts:102` (`/api/dev/setup`, localhost/dev-only); it
creates a user + device with `deviceToken = token_<UUID>` (`worker.ts:144`).

Note: `room.ts` `verifyToken` for bridge checks **format only**
(`token_` + ≥32 chars, `room.ts:350-358`); the real DB check is at the Worker
(`worker.ts:362`) which stamps a verified marker the DO trusts (`worker.ts:400`).
So the static-token smoke config passes the DO's own check but would be rejected
by the Worker in a non-dev path — fine for local smoke, not for staging.

## Coverage matrix — what cerberus exercises today vs gaps

cerberus WS step vocabulary: `ws_connect{role,connection_id}`,
`ws_send{connection_id,message}`, `ws_receive{connection_id,type,timeout?,decisive?,assert?,match_all?}`,
`ws_disconnect{connection_id}`, plus automatic batch decomposition. This is rich
enough to express most room.ts paths; the gaps below are "not yet authored", not
"inexpressible" — except the side-effect rows marked §.

### Covered (by `TestRunStepsMultiConnectionOpenAgents`)

- web connect, bridge connect (hard capability assertion)
- `device:online` relay (bridge→web on join)
- `session:start` → `session:created` (best-effort; currently surfaces Finding 4)

### Gap A — Bridge→Web relay (`room.ts:178-220`)
Inject via `ws_send` on `c-bridge`, assert arrival on `c-web` with `ws_receive`:
`encrypted`, `session:created/started/output/stopped/error/message/status`,
`chat:response/thought/permission`, `permission:request`, `acp:status/output/tool_call/tool_result`,
`agent:status`, `tool:call`, `session:usage`, `multiagent:task_started/progress/completed/failed/job_completed`,
`multiagent:task_result/task_error` (§ also trigger `notifyOrchestrator`),
`prompts:synced`, `mcp:synced/list_response`, `config/rules/storage:synced`.

### Gap B — Web→Bridge routing (`room.ts:224-252`)
`ws_send` on `c-web` with `payload.deviceId`, assert on `c-bridge`. Includes the
`session:start` fix (add `payload.deviceId` → expect `sendToBridge` hit): `session:start/send/stop/resize`,
`chat:send`, `permission:response`, `control:takeover`, `config/rules/storage:sync`,
`prompts:sync`, `mcp:sync/list`, `multiagent:start_job/pause_job/cancel_job/start_task/task_assign`,
`acp:query_status`.

### Gap C — Lifecycle
- `device:offline` on bridge disconnect (`room.ts:154-160`): `ws_disconnect c-bridge` → `ws_receive device:offline` on `c-web`.
- `sendToBridge` miss path: unknown `deviceId` → silent drop (`room.ts:295`); assert no frame arrives.
- Fan-out: ≥2 web clients, assert `broadcastToWeb` reaches all (`room.ts:269`).

### Gap D — Auth / error paths
- invalid/missing `type` → 400 (`room.ts:49`)
- bridge without `deviceId` → 400 (`room.ts:53`)
- missing token → 401 (`room.ts:57`); bad token → 401 (`worker.ts:365`)

### Gap E — Not observable via WS-only (§)
- `notifyOrchestrator` fire-and-forget fetch (`room.ts:326-338`) — needs an HTTP
  or DB assertion against `/api/multiagent/internal/orchestrator/event`.
- Internal `/broadcast` HTTP→WS endpoint (`room.ts:26-35`) — Worker→DO→clients
  push; needs an HTTP step, not a WS step.

## Suggested next test cases (highest value first)

1. **`session:start` round-trip** (closes Finding 4): web sends
   `{"type":"session:start","payload":{"deviceId":"<id>"}}`, bridge replies
   `session:created` via `ws_send c-bridge`, web `ws_receive session:created`.
   Proves Web→Bridge→Web routing end-to-end.
2. **`device:offline` lifecycle**: disconnect bridge, assert `device:offline` on web.
3. **`multiagent:task_progress` relay + § orchestrator callback**: bridge sends
   `task_progress`, assert relay on web AND the orchestrator HTTP callback
   (requires extending the case with an HTTP assertion — Gap E).
4. **`session:output-batch` (`match_all`)**: bridge sends a
   `session:output-batch` frame with N lines, web `ws_receive session:output
   match_all=true assert={payload.<field>: <expected>}` — exercises cerberus's
   batch decomposition against real open-agents batch framing.
