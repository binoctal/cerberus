# open-agents Known Issues (Protocol Inconsistencies)

Date: 2026-08-16 (items 1-5), extended 2026-08-18 (items 6-12, live-verified
during the workflow-orchestration coverage run — see
`2026-08-18-workflow-coverage-run.md`).
Source: research for the fidelity-ladder real-E2E plan (2026-08-14), re-verified
against `../open-agents` HEAD on 2026-08-16. Closes fidelity-ladder Task 8
item 4 (see `superpowers/plans/2026-08-14-fidelity-ladder-real-e2e.md`).

Scope: facts a Cerberus Scout/executor must know when modeling open-agents
behavior. Each item lists the verified file:line evidence and the Cerberus-side
consequence. None of these are Cerberus bugs; they are upstream divergences
between the open-agents API worker, the Durable Object room, and the Go bridge.

---

## 1. DO message whitelist vs bridge handleMessage diff — RESOLVED 2026-08-21 (open-agents `fix/ws-whitelist-alignment`: whitelist extracted to exported Sets, membership checks replace the case chains; contract test `room-whitelist.test.ts`)

The DO (`apps/api/src/realtime/room.ts:340+`) only forwarded whitelisted
`msg.type` values on the web→bridge path while the Go bridge
(`bridge/internal/bridge/bridge.go` handleMessage) accepted a wider set.
Types the bridge could handle but the DO silently dropped (web origin could
never reach them) — all forwarded since the 2026-08-21 alignment:

- `session:resume`
- `scanner:toggle`
- `scanner:rules:sync` (found 2026-08-17 in the realtime-e2e sweep: the DO
  whitelist carries only the `scanner:rules:synced` ACK direction; the
  bridge's `handleScannerRulesSync` is therefore dead from web origin, and
  the ack edge is untriggerable — marked unsupported in the dogfood vocab)
- `workflow:get_state`, `workflow:set_state`, `workflow:merge_all`,
  `workflow:task_cleanup`, `workflow:task_merge`

Reverse direction: `device:listDir` was documented here as handler-less —
STALE: bridge commit 7ab073e added `handleListDir` (merged; both DO whitelist
directions carry the pair), so directory browsing is live end-to-end and a
declarative realResponder pair covers it since 2026-08-21.

**Cerberus consequence:** do not generate coverage cases that require
web→bridge `session:resume` or the merge/cleanup workflow commands over WS;
they cannot pass. `device:listDir` cases will observe the request forwarded
but no `device:listDirResult` ever returning.

**Bridge→web drops (2026-08-18):** `session:cancelled` is also missing from
the DO's bridge→web whitelist (room.ts:351-399) — the bridge EMITS it in
handleSessionCancel (bridge.go:1557), but web can never observe a cancel
ack. The web→bridge `session:cancel` edge is send-side credit only.

**Payload-shape trap (2026-08-17):** even whitelisted sync commands can
silently no-op on shape: `handleRulesSync`/`handleScannerRulesSync` type-assert
`payload.rules` as a JSON ARRAY and `handleMCPSync` unmarshals `payload.servers`
as an OBJECT keyed by server name — a missing field or wrong shape returns
without any error frame. Sync cases must send `"rules": []` /
`"servers": {}`.

## 2. Three divergent dev-setup endpoints — RESOLVED 2026-08-21 (dead worker.ts dup removed; the three-endpoint table above is historical)

Three implementations of "create dev user + device", all live in dev mode:

| Endpoint | File | Existing-user behavior | Extras |
|---|---|---|---|
| `POST /api/dev/setup` | `routes/dev.ts:13` (mounted first) | verifies password, 401 `INVALID_PASSWORD` on mismatch | `plan='free'`, no role, returns password in body |
| `POST /api/dev/setup` | `worker.ts:102` | no password check | role `superadmin`, no plan |
| `POST /api/auth/dev/setup` | `routes/auth.ts:704` | no password check | role `superadmin`, **returns JWT pair** |

Hono matches in registration order: `app.route('/api/dev', devRoutes)` at
worker.ts:99 wins, so the worker.ts:102 duplicate is **dead code**. The
`/api/auth/dev/setup` variant is the only one that returns a JWT directly.

**Cerberus consequence:** the live `/api/dev/setup` (dev.ts) returns no JWT —
protected HTTP routes still need `POST /api/dev/login` (dev.ts:112), matching
the live-port/auth gotchas memory. Tests must not assert on the worker.ts
variant's response shape.

## 3. auth/sessions + auth/tokens routes defined but unmounted — FIXED 2026-08-21 (worker.ts imports routes/auth/index; /api/auth/sessions now 401-not-404 live)

`routes/auth/index.ts` combines `authRoutes` + `sessionsRoutes` +
`tokenRoutes` (auth/sessions.ts, auth/tokens.ts). But `worker.ts:4` imports
`from './routes/auth'`, which resolves to `routes/auth.ts` (file beats
directory) — so `/api/auth/sessions` and `/api/auth/tokens` are **never
mounted in production**; only `test/routes/auth-{sessions,tokens}.test.ts`
import them directly. Session listing/revocation and API-token management are
unreachable via the deployed worker.

**Cerberus consequence:** HTTP surface extraction should not count
`/api/auth/sessions*` or `/api/auth/tokens*` as reachable claims; negative
cases against them would 404 for routing reasons, not auth reasons.

## 4. `workflow:*` vs `multiagent:*` naming fork

The same subsystem carries three names depending on layer:

- WS message types: `workflow:*` (room.ts whitelist, bridge.go handleMessage)
- DB tables: `multiagent_missions`, `multiagent_tasks`
- HTTP routes: `/api/missions`, with a legacy 301 redirect
  `/api/workflows/jobs/* → /api/missions/*` (worker.ts:306)

**Cerberus consequence:** vocab/protocol modeling must map
`workflow:task_assign` (wire) ↔ `multiagent_tasks` (storage) ↔ `/api/missions`
(HTTP) as one concept. Coverage attribution keyed on literal prefixes will
under-count. The legacy redirect means both prefixes appear in logs.

## 5. `workflow:job_completed` / `workflow:task_status_update` have no emitter — RESOLVED 2026-08-21 (drop: dead whitelist entries deleted from room.ts; web's three no-op job_completed listeners removed — job_status is the live completion signal)

Both types existed **only in the room.ts bridge→web whitelist** — no code
path in apps/api or the Go bridge ever emitted them (deleted from the
whitelist 2026-08-21). Completion and per-task status are signalled by
different types, broadcast DO-side and outside the room.ts case handler:

- `workflow:job_status` with `status: completed|failed`
  (`apps/api/src/services/orchestrator.ts:949`, finalizeMissionIfDone)
- `workflow:state_updated` (`orchestrator.ts:1073`)

**Cerberus consequence:** no ws_receive can ever observe these two types —
any case awaiting them fails on timeout. The dogfood vocab marks both
bridge→web edges `unsupported: true` (2026-08-18), which drops them from
the `requiredEdges` coverage denominator. Workflow success criteria must
instead assert the out-of-band `workflow:job_status {status: completed}`
frame.

---

## 6. Bridge workflow callbacks POST to a ws:// URL (completion unreachable)

The bridge's callback manager builds its HTTP URL from the WS server URL:
`bridge/internal/bridge/bridge.go:208` passes `APIURL: cfg.ServerURL` (the
same `ws://…` URL the bridge connects on), and
`bridge/internal/workflows/callback.go:184` does
`url := m.config.APIURL + "/api/workflows/internal/orchestrator/event"`.
Every task result / error callback therefore fails with
`Post "ws://localhost:8989/api/workflows/internal/orchestrator/event":
unsupported protocol scheme`.

**Cerberus consequence:** `workflow:task_result` / `task_completed` /
`task_failed` never reach the API on the dispatch path; missions never
complete and stuckRecovery is the only exit. The dogfood vocab marks the four
completion-family bridge→web edges `partial` with this live-verified reason
(2026-08-18). Success criteria must assert the observable chain
(task_progress / task_question) instead of completion frames. Reopens when
open-agents derives the http(s) callback base from the ws:// URL.

## 7. Orchestration dispatch never emits `workflow:task_started` — FIXED 2026-08-21 (bridge startTaskSession emits taskStartedMessage before the progress-0 report)

On the orchestrator→bridge dispatch path the bridge's `startTaskSession`
reports start as `workflow:task_progress {progress: 0, step: "started"}`
(`bridge/internal/bridge/bridge.go:2748-2754`). The only bridge emitters of
`workflow:task_started` are `handleWorkflowStartJob` (bridge.go:2621) and
`handleWorkflowStartTask` (bridge.go:2666) — echoes of web-origin commands.
The API can emit it (`apps/api/src/services/orchestrator.ts:889`,
`handleTaskProgress` with `progress === 0`) but that path is driven by the
internal orchestrator event endpoint, i.e. the callback broken in item 6.

**Cerberus consequence:** a ws_receive on `workflow:task_started` fails on
timeout in every live dispatch scenario; await `task_progress` with
`step: "started"` instead.

## 8. Planner agent-list injection is copy-prone — FIXED 2026-08-19 upstream (sanitizeRecommendedAgent, de920a5)

`apps/api/src/services/planner.ts:177` renders the available-agent list as
`- ${a.baseCli}: ${a.name}` into the planner prompt. glm-4.5 (and likely
similar models) sometimes copies the whole literal into `assigned_agent`
(observed `claude: cerberus-bridge-agent`). `resolveCliForAgent`
(orchestrator.ts:354) then finds no agent row with that id/name, falls back
to the literal as the required CLI, and `selectDevice` skips the task forever
("no device with CLI 'claude: cerberus-bridge-agent'").

**Cerberus consequence:** harness-side workaround shipped — the seeded agent
row is named identically to its baseCli so either copy shape resolves. Any
case-seeding that diversifies agent names must re-check this.

## 9. `[QUESTION]` false positive on PTY prompt echo — FIXED 2026-08-21 (bridge 74734d7: extractQuestion requires the marker to OPEN the line, matching the prompt's own contract; PTY echo of the instruction text no longer matches)

The task prompt itself instructs the marker
(`bridge/internal/bridge/bridge.go:2785`, buildTaskPrompt: "output a line
starting with [QUESTION]…"), and output handling runs
`handleQuestionMarker` on every content chunk (bridge.go:712-713 → 3033). On
the PTY path the terminal echoes the prompt, so the marker detection fires
deterministically on harness-generated text, not agent intent (spurious
`workflow:task_question` + pending-question machinery).

**Cerberus consequence:** a `task_question` frame on the PTY path is NOT
evidence the agent asked; treat it as expected noise of the fallback path.
(Usually combined with item 10 — see below.)

## 10. Worktree dispatch passes a relative cwd → ACP rejection → PTY fallback — FIXED 2026-08-21 (WorktreeManager absolutizes its base; live-verified: session workDir absolute)

The bridge constructs its worktree manager with a literal relative base
(`bridge/internal/bridge/bridge.go:206`,
`workflows.NewWorktreeManager(".")`), and `CreateWorktree`
(`bridge/internal/workflows/worktree.go:36-58`) returns
`filepath.Join(w.projectDir, …)` — a relative path like
`./.open-agents-bridge-worktrees/task-…`. The ACP adapter only absolutizes
`""` and `"."` (`bridge/internal/protocol/acp.go:417-421`), so a worktree
path reaches `session/new` as a relative `cwd` →
`Invalid params: cwd must be an absolute path` → 60s hang → PTY fallback.
(Ladder finding #4, root-caused 2026-08-18.)

**Cerberus consequence:** the mission dispatch path deterministically lands
on PTY in this environment; ACP-path-only assertions cannot pass until
upstream absolutizes the worktree path.

## 11. Plan updates don't invalidate the 5-min `getPlanLimits` cache — FIXED 2026-08-21 (admin plans PUT/DELETE call invalidatePlanCache)

`getPlanLimits` caches per plan for 5 minutes
(`apps/api/src/lib/plan-limits.ts:55,68-71,138`); an invalidator exists
(`invalidatePlanCache`, plan-limits.ts:246) but its only production caller is
the Creem webhook (`apps/api/src/routes/creem-webhook.ts:87`). The admin
plans PUT (`apps/api/src/routes/admin/billing/plans.ts:140` UPDATE
subscription_plans) never invalidates, so updated limits keep serving stale
for up to 5 minutes.

**Cerberus consequence:** after a plan-limits fix, either wait out the TTL
or restart the worker before re-running; a harness that PUTs a plan and
immediately asserts new limits will read cached old values.

## 12. Plan-limits deep-merge trap for harness setup

Stored plan limits are deep-merged with `FALLBACK_LIMITS`
(plan-limits.ts:82-89): `{...FALLBACK_LIMITS.rate_limits, ...parsed.rate_limits}`
per sub-object, so any key the plan JSON omits silently falls back to the
code defaults — `api_hourly: 100`, `api_daily: 500`, `max_agents: 5`
(plan-limits.ts:46-50). A harness whose setup performs ~130 admin writes
exhausts api_hourly and every mission-setup POST 429s
(`HOURLY_RATE_LIMIT_EXCEEDED`) before orchestration starts.

**Cerberus consequence:** the seeded plan must explicitly lift
`api_hourly` / `api_daily` / `max_agents` (shipped in
`internal/head/scout/mission_seed_cases.go`).

---

## 13. Workflow completion callback family — six stacked defects (found 2026-08-19, FIXED on open-agents `fix/workflow-callback-url`, MERGED to main 2026-08-19 as 146164c)

Item 6's ws:// scheme diagnosis was only the top layer. Dissecting the
full "mission never completes" chain (live, 4 dogfood runs) surfaced six
defects, all fixed on the branch:

1. **Scheme** (bridge.go:208): `CallbackConfig.APIURL` was the raw
   `ws://` ServerURL → `unsupported protocol scheme` (the known item 6).
2. **Path** (callback.go): posted `/api/workflows/internal/orchestrator/event`,
   which does not exist — real route is `/api/missions/internal/orchestrator/event`
   (the legacy redirect only covers `/api/workflows/jobs/*`).
3. **Auth** (missions.ts `/internal/*` middleware): the callback sent no
   `X-Internal-Secret` → 403.
4. **Merge routing dead** (room.ts): `workflow:task_merge` was neither in
   the DO `/broadcast` sendToBridge set nor the web→bridge whitelist, and
   `sendMergeCommand` carried no deviceId — `handleWorkflowTaskMerge`
   (the only bridge→web `workflow:task_result` emitter) was unreachable.
5. **Internal routes have no user context** (missions.ts): `getOrchestrator`
   read `c.get('userId')`, undefined on secret-authed routes → every
   event callback 500'd with D1_TYPE_ERROR (bind undefined). Fixed by
   payload.userId on the bridge side + `getOrchestratorForUser` /
   mission-owner fallback on the route. Note: `/internal/orchestrator/alarm`
   still has this gap (alarm payloads carry no userId/missionId) — unfixed.
6. **Natural CLI exit never reported** (bridge.go / protocol/pty.go): a
   CLI that finishes by itself only emits status=idle with an exit_code
   meta; nothing stopped the session, so the exit callback
   (`SendTaskResult`) never fired. Fixed by stopping the session on
   exit_code status.

**Dogfood-side companions** (cerberus): the shim now exits 0 after
echoing a `[QUESTION]` line (models a task CLI that finishes), and the
mission-seed agent row is `claude-pty` (base cli `claude` resolves to
`npx @agentclientprotocol/claude-agent-acp`, which never finishes
offline — legacy rows need the one-time D1 cleanup in the env doc).

**Result:** live run 2026-08-19 — "Callback successful", mission-seed
PASS, completion frames (`task_completed`, `job_status`, merge-path
`task_result`) all received, coverage 100% with the family reopened.
Follow-up rounds the same day (runs 5-9) reopened the failure family too
(`task_failed` via retry exhaustion, `task_error` via branchless merge)
at coverage 100% with 0 judge degradations. Four more layers (alarm-route
user, stuck-recovery SQL, planner agent-literal copy, rate-limiter
crowding) are documented in `2026-08-19-completion-family-run-record.md`.

---

## Re-verification

Items 1-5 checked 2026-08-16; items 6-12 checked 2026-08-18 (code read +
live-run evidence) against
`/home/mason/Documents/code_projects/private/open-agents` working tree.
If open-agents refactors, re-run the greps in this doc's history before
trusting the consequences.

## 14. Retry-3 task dispatch silently lost (planner picks a slow CLI) — found 2026-08-23, root-caused + fixed 2026-08-24

**STATUS: FIXED** — open-agents 7fcf9cd (branch
`fix/known-issue-14-stale-progress-resurrection`), stale-start guard in
`handleTaskProgress`.

Dogfood run 8 (cerberus session d161c714): the fail-mission's task never
reached `workflow:task_failed` because its THIRD retry never dispatched.
Full evidence chain (now visible thanks to `<runtime>/logs/<actor>.log`):

- bridge-pty-1 tee log: attempts 1-2 assigned `codex` (the PLANNER picked
  codex this run — bypassing the seeded claude-pty row); each cost a 30s
  ACP initialize timeout before the PTY stub exit-1 → 2 error callbacks
  (23:02:23, 23:02:58).
- Orchestrator log: "retrying … 15000ms (attempt 2/3)"; the retry alarm
  fired at 23:03:13 and POST /orchestrator/alarm returned 200 — then
  SILENCE: no "switching/max-concurrent/no-device/skipping" line follows.
- D1: t0 stuck at `status=running, retry_count=2, assigned_agent=claude`
  (the fallback ladder's 2nd rung); neither bridge logged a task_assign.
- So `scheduleNextTasks` ran, mutated the task (agent→claude, status→running)
  yet no dispatch reached any device.

**Root cause (confirmed against D1 + code, 2026-08-24):** the original
suspects (slot leak keyed by pre-switch agent, getReadyTasks filtering)
were wrong. The bridge emits `task_started` + `task_progress{progress:0}`
immediately after `CreateWithIDAndSize` returns — including when the
session dies in the same millisecond (ACP fail → PTY stub exit-1). Those
start frames travel bridge→WS→DO→fire-and-forget-fetch, while the
`task_error` callback travels bridge→direct HTTP **with 1s/2s/4s retry
backoff** — two unordered paths. In run 8 the error handler completed
first (23:02:58.415: wrote `pending`, switched agent, scheduled the 15s
retryTask alarm), then the stale progress-0 landed and
`handleTaskProgress(progress=0)` unconditionally set status back to
`running`. `getReadyTasks` selects only `status='pending'`, so the alarm
at 23:03:13 found zero ready tasks, executed nothing (hence total log
silence), and the task was stuck forever. D1 sealed it: `progress=0`,
`updated_at` in the same second as the error callback, and `timeout_at`
still reflecting attempt 2's dispatch (23:02:28+30min — no attempt-3
dispatch ever reset it).

**Fix:** `handleTaskProgress(progress=0)` now reads the task and only
advances it to `running` when its status is `assigned` or `running`;
stale frames are dropped with a log line (raw progress frame still
forwarded for UI). Unit tests: `apps/api/src/test/services/orchestrator.test.ts`
(pending/completed dropped, assigned/running advance). Full apps/api suite
1765/1765 green; `TestRealBridge_M1_Orchestration` PASS; mission-seed
`task_failed` leg verdict in the run record.

The flakiness driver: which agent the planner assigns (claude-pty fails
fast → 3 retries exhaust inside the 600s await; codex/claude burn 30s ACP
timeouts per attempt and expose the lost-dispatch bug). With the guard,
either planner pick should now converge on retry exhaustion.

**Adjacent defect noticed while reading — STATUS: FIXED 2026-08-24
(open-agents c038f39, branch `fix/do-alarm-single-slot-overwrite`):** the
user-room DO kept ONE alarm slot (`__alarm_type`/`__alarm_payload` +
single `setAlarm`), so every `orchestrator:schedule_alarm` from the same
user's orchestrator OVERWRITES any pending alarm — last writer wins. It
bit for real during run 10 (`job_1787538326005` t3: two retryTask alarms
35ms apart, the later replaced the first, task stuck pending with frozen
retry_count). Fix: persistent dueAt-sorted queue
(`apps/api/src/realtime/alarm-queue.ts` pure functions + `room.ts`
wiring) — scheduleAlarm appends and arms the hardware alarm at the
earliest dueAt; alarm() drains everything due, persists the remainder,
re-arms; legacy single-slot keys migrate on first wake. Unit tests in
`src/test/realtime/alarm-queue.test.ts` (6 cases incl. the 35ms-apart
repro and legacy migration); full apps/api suite 1771/1771.

Side observation (RESOLVED — stale note): the "periodic stuckRecovery
alarms carry an empty payload → alarm route 400s" gap was closed by
open-agents 0c62e11 (2026-08-19): `Orchestrator.scheduleAlarm` stamps
`userId` into every alarm's data, the DO posts it back as the alarm
payload, and `getOrchestratorForInternalEvent` resolves on it — the
stuckRecovery success case is covered by a route test in
`missions.test.ts`.
