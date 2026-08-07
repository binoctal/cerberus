# cerberus ↔ open-agents — Test Mapping & Coverage — 2026-08-02

How cerberus tests `open-agents`, grounded in the real open-agents source
(sibling repo `../open-agents`). Supersedes the protocol guesses in the earlier
yaml configs; this is the empirical mapping after the 2026-07-23 dogfood run.

## Target architecture (real code)

```
client ──ws──► Worker (apps/api/src/worker.ts) ──► UserRoom DO (apps/api/src/realtime/room.ts)
                 /ws/{userId} route + bridge DB auth        hibernatable DO, relays web↔bridge
```

> **Correction (2026-08-03):** the running DO is `apps/api/src/realtime/room.ts`
> (imported by `apps/api/src/worker.ts`). The standalone `apps/realtime/src/room.ts`
> is a deployment variant with a different (older) vocabulary — it uses `multiagent:*`
> types and `/api/multiagent/...` paths. The running file uses `workflow:*` types and
> `/api/missions/...` paths (a `multiagent→workflow`/`missions` rename). Earlier
> sections of this doc cited `apps/realtime/src/room.ts` and `multiagent:*`; the
> coverage section below is re-grounded in the running file. The line-cites in the
> "four facts" table predate this correction and may point at the variant file.

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

## Coverage matrix — what cerberus exercises (running DO: `apps/api/src/realtime/room.ts`)

cerberus WS step vocabulary: `ws_connect{role,connection_id}`,
`ws_send{connection_id,message}`, `ws_receive{connection_id,type,timeout?,decisive?,assert?,match_all?}`,
`ws_disconnect{connection_id}`, plus automatic batch decomposition. The
side-effect rows (§) are asserted via a local HTTP **capture server**
(`internal/head/agent/captureserver_test.go`, port 9099) with open-agents'
`API_BASE_URL=http://127.0.0.1:9099`.

All five gaps A–E are now covered by `//go:build integration` tests in
`internal/head/agent/execute_phases_steps_integration_test.go` (shared fixture
`setupOpenAgents` in `openagents_setup_test.go`). Run a single test:
`go test -tags integration -run <Name> -v ./internal/head/agent/`.

### Covered

- **Capability** (`TestRunStepsMultiConnectionOpenAgents`): web + bridge connects
  to the same `/ws/<userId>` DO; `device:online` relay on bridge join.
- **Gap A — Bridge→Web relay** (`TestBridgeToWebRelay`, `room.ts:349-401`):
  `ws_send c-bridge` → `ws_receive c-web`. Covers `encrypted`,
  `session:created/started/stopped/error/message/status`, `chat:response/thought/permission`,
  `permission:request`, `acp:status/output/tool_call/tool_result`, `agent:status`,
  `tool:call`, `session:usage`, the `workflow:task_started/progress/completed/failed`,
  `workflow:job_completed`, `workflow:task_result/error/question/task_status_update`,
  `workflow:merge_progress`, `prompts:synced`, `mcp:synced/list_response`,
  `security:alert`, `scanner:rules:synced`, `device:listDirResult`,
  `config/rules/storage:synced` — **37/37 PASS**.
  - `session:output` is intentionally excluded: it is *batched* (`batchOutput`,
    `room.ts:342-346`), not a plain relay.
- **Gap B — Web→Bridge routing** (`TestWebToBridgeRouting`, `room.ts:404-458`):
  `ws_send c-web {type, payload:{deviceId}}` → `ws_receive c-bridge`. Covers
  `session:start/stop/cancel/resize/send`, `chat:send`, `permission:response`,
  `control:takeover`, `config/rules/storage:sync`, `prompts:sync`, `mcp:sync/list`,
  `workflow:start/pause/cancel/start_task/task_assign/task_answer/task_guidance`,
  `acp:query_status`, `device:restart/listDir` — **24/24 PASS**.
- **Session round-trip** (`TestSessionStartRoundTrip`): web `session:start` →
  bridge receives → bridge replies `session:created` → web receives it (closes
  the original Finding 4).
- **Gap C — Lifecycle** (`TestLifecycleSignals`): `device:offline` on bridge
  disconnect; `sendToBridge` silent-drop on unknown `deviceId` (inverted
  assertion — the miss is the proof); `broadcastToWeb` fan-out to two web
  clients — **3/3 PASS**.
- **Gap D — Auth / error** (`TestAuthErrorPaths`, raw dial with `nil` wsIdx so
  no token is injected): invalid `type` → 400; bridge without `deviceId` → 400;
  missing token → 401; bad bridge token → 401 — **4/4 PASS** (HTTP status surfaced
  best-effort in the dial error).
- **Gap E — Orchestrator callback §** (`TestOrchestratorCallback`, `room.ts:397,575-587`):
  bridge sends `workflow:task_progress/result/error` → the capture server observes
  a POST to `/api/missions/internal/orchestrator/event` — **3/3 PASS**. (Trigger
  condition: `room.ts:397` fires `notifyOrchestrator` for
  `workflow:task_result/error/progress/question`.) Prerequisite: open-agents must
  run with `API_BASE_URL=http://127.0.0.1:9099` (wrangler ignores shell-env
  prefixes — set it in `apps/api/.dev.vars` or via `wrangler dev --var`); the
  test `t.Skipf`s (not fails) if the callback never arrives.

### Still uncovered (deferred)

- `/broadcast` internal HTTP→WS endpoint (`room.ts:98-108`) — Worker→DO→clients
  push; needs an HTTP step into the DO, a different capability class.
- `workflow:task_question` as a 4th gap-E trigger (covered indirectly; not a row).

> `session:output` batching (`flushBatch`, `room.ts`) was previously listed here;
> it is now covered by `TestVocabularyDriven`'s `flushBatch__to_web/session:output-batch`
> edge (re-verified via `make integration-openagents`, 2026-08-07).
