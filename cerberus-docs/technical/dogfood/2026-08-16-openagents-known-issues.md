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

## 6. Bridge workflow callbacks POST to a ws:// URL (completion unreachable) — RESOLVED (subsumed by the #13 fix family, closed 2026-08-24)

The bridge's callback manager built its HTTP URL from the WS server URL:
`bridge/internal/bridge/bridge.go` passed `APIURL: cfg.ServerURL` (the
same `ws://…` URL the bridge connects on), so every task result / error
callback failed with `Post "ws://…": unsupported protocol scheme`.

**RESOLUTION:** `bridge/internal/workflows/callback.go` now normalizes the
base at construction (`httpURLFromWS`: ws://→http://, wss://→https://) —
part of the #13 completion-callback family fix (open-agents 146164c,
2026-08-19). Live-verified since: dogfood runs 9-11 completion missions
complete on the dispatch path over real HTTP callbacks (run 10
`job_1787544529518` t0 completed via claude-pty; run 11 mission-seed
completion leg pass/0.95), and the vocab completion-family edges were
re-marked live at 100% (2026-08-19, bridge 5f866ac era).

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

## 9. `[QUESTION]` false positive on PTY prompt echo — FIXED 2026-08-21 (bridge 74734d7), residual hard-wrap variant FIXED 2026-08-24 (bridge 9839ad1: buildTaskPrompt no longer contains the literal marker at all)

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

**2026-08-24 residual closed:** the line-prefix guard still fired when the
terminal HARD-WRAPPED the echoed prompt at the column width — a wrap landing
right before any literal marker occurrence puts it at a genuine `\n` line
start (live logs 2026-08-18..24 show the example sentence split mid-word
"Should I use JW", other widths "or ses"/"or authent", and the instruction
sentence's own marker landing at a line start). Root fix: buildTaskPrompt
(bridge 9839ad1) describes the marker format without spelling the literal
out, so no echoed line can ever open with it; regression tests lock both the
no-literal invariant and width-10–160 wrapped echoes. Cerberus dogfood shims
switched their completion trigger to the `--- Instruction ---` header
(cerberus 1e20463). Run 15 (2026-08-24, session 5e624718): zero spurious
question firings.

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

## 12. Plan-limits deep-merge trap for harness setup — FIXED 2026-08-24 (write path rejects partial sections; read path logs fallback keys)

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

**FIX (2026-08-24):** the read-side deep merge stays (legacy rows are
partial), but the trap is no longer silent or reachable from the write
path: `plan-limits.ts` exports `findMissingLimitKeys` (shared key lists)
and `getPlanLimits` logs a warning naming exactly which keys fell back to
code defaults; the admin plans POST/PUT (`routes/admin/billing/plans.ts`)
reject a PROVIDED limits section that omits keys with 400
`limits_incomplete` + the missing dotted key paths (sections may still be
omitted wholesale). Cerberus's mission-seed payload now sends complete
sections (and an explicit positive `max_concurrent_tasks: 100` — the
0-trap guard updated from "omit" to "positive"). Tests:
`plan-limits.test.ts`, `plans-limits-validation.test.ts` (4 route cases).

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

## 15. question-timeout session leak wedged missions via pool starvation — FIXED 2026-08-25 (bridge: timeout branch stops the session; run 18 live-validated: mission finalizes). Root cause was NOT message loss: the duplicate-dispatch retry session re-asked after the web had answered, timed out, and leaked its pool slot forever

Run 17's fan-out mission: t0 dispatched to bridge-pty-1 (D1
`assigned_device_id` set) but the bridge log has ZERO assign entries — the
DO's sendToBridge dropped the frame while the bridge's HTTP heartbeat kept
D1 `last_seen` fresh (checkDeviceOnline passes). task_assign has no
delivery ack, so nothing retries; the task sits in `assigned` and
finalizeMissionIfDone never fires (job_status never emitted; mission
`running` forever until stuck-recovery). Evidence:
2026-08-25-run17-fanout-finds-defects.md.

**Cerberus consequence:** a mission case cannot assert job_status until
this is fixed; multi-device fan-out proof is blocked on it.

## 16. Duplicate task dispatch race → worktree "branch already exists" — FIXED 2026-08-25 (apps/api markTaskDispatched CAS; run 18: zero 600ms duplicates). Same fix family: bridge session route last_seen now ISO (the offline-flicker that made round-robin skip devices) — FIXED same day

Two dispatch passes re-assigned t1 and t2 within ~600ms (first pass's
status write not visible to the second); the second `git worktree add`
fails fatally on the existing branch name. Same run, same evidence doc.

**Cerberus consequence:** none directly (the first session completes), but
it corrupts retry accounting and log signal-to-noise (62 log lines for one
task).

## 17. ACP-path task failure never fired the exit callback → task wedged in running — FIXED 2026-08-25 (bridge 205631b: agent death emits the PTY-shaped exit_code status; run 24 live-validated: task_failed returns, mission-seed passes, the four coverage gaps close, coverage 100%)

The fake ACP agent now errors and exits 1 on CERBERUS_FAIL prompts (PTY-shim
parity), but an agent process exit on the ACP path does not translate into
the session exit callback: no task_error is reported, the task sits in
`running`, retries never fire, and workflow:task_failed is never broadcast
(runs 22-23: the fail mission's task_failed receive timed out at 600s with
80+ unmatched frames). PTY tasks fail correctly via process-exit → exit
callback; the ACP adapter lacks the equivalent teardown wiring.

**Cerberus consequence:** mission-seed's fail leg cannot pass while any
fail-mission rung lands on the ACP path; its four edges stay coverage gaps
until fixed.

## 18. Sidebar "Connecting..." forever — web appStore.isConnected is a dead field (written by nobody) — FIXED 2026-08-26 (open-agents 660d41f: sidebar reads websocketStore.isConnected; MissionsPage fetches devices — live-verified "Connected / 3/744 online")

WebSocketProvider.onConnect calls websocketStore.setConnected, but
DashboardLayout's sidebar indicator reads appStore.isConnected. No code
anywhere calls appStore.setConnected (grep: only the store's own unit
test). The app's WebSocket actually connects fine (page-instrumented
WebSocket probe 2026-08-25: `ws://localhost:8989/ws/<user>?type=web` fires
`open`), yet the sidebar shows "Connecting..." with an amber pulsing dot
forever, which is what made the live demo look broken.

Also seen on the same page: MissionsPage never calls fetchDevices (only
Dashboard/Terminal/AddDeviceDialog do), so the device selector and the
"0/0 online" counter stay empty there while `/api/devices` shows the three
demo bridges online.

**Cerberus consequence:** none — SUT APIs and WS are healthy; this is
purely a web display defect that misleads demo observers.

## 19. WebSocketProvider gives up permanently when the first ensureValidToken fails — FIXED 2026-08-26 (open-agents 660d41f: 5xx refresh keeps credentials instead of wiping auth; refreshWithMutex returns one shared promise and frees the slot on settle; WS token gate retries with backoff)

The provider's effect (deps `[isHydrated, userId]`) calls
`ensureValidToken()` once; if it returns null (e.g. a transient
`/api/auth/refresh` 500 during page load) the connect flow returns with
no retry, and websocketStore stays at its 'connecting' default until a
full page reload. Related: `refreshWithMutex` lacks try/finally, so a
throwing `refreshAccessToken` leaves the mutex promise set forever,
freezing every later `ensureValidToken` await in the same page session
(observed as WS never even attempting to connect during the evening
gateway flaps; POST /auth/refresh 500 in console, zero ws:// opens).

**Cerberus consequence:** none for dogfood runs (they drive WS directly);
it degrades the web demo exactly when the API host is flaky.

## 20. Duplicate re-dispatch destroys a healthy in-flight session (worktree collision → wrong-dir replacement) — FIXED 2026-08-26 (bridge 8766f72, open-agents e2b0a83)

Demo mission `job_1787672167461` t1 on b2: first dispatch created the
worktree session normally. Exactly +30.03s later a second task_assign
arrived (t1 had been reset to pending — the reset itself happened during a
worker degradation window, see #21; CAS correctly allowed the re-dispatch
because the task was pending again). The second assign's worktree creation
collided with the existing branch (#16 family) and fell back to workDir
"."; `manager_new.go:57` then treated the workDir mismatch as
"cannot resume" and REPLACED the session — disconnecting the healthy
in-flight session and recreating it in the bridge root directory. The task
lost its session ("Session not found ... Active sessions: []" output drops,
/tmp/demo-ui-b2.out lines 69-78), sat stuck for ~4 minutes until the 5-min
stuck-recovery alarm re-dispatched, and the final successful attempt ran
with workDir "." (no worktree isolation).

Evidence: /tmp/demo-ui-b2.out full timeline 23:37:51→23:42:59; wrangler
demo log (alarm storm + 503s + 5.5s event POSTs at the same instant).

Two missing defenses:
1. Bridge: a re-assign whose existing session is ALIVE and matches the same
   job/task should be ignored (or resume), never replaced on a workDir
   mismatch — the mismatch is the re-dispatcher's degradation, not the
   session's.
2. Orchestrator: the retry/re-dispatch path should reuse the original
   workDir (or clean up the colliding branch) instead of silently dropping
   isolation ("falling back to original dir" runs the task in the bridge
   root).

**Fix (2026-08-26, bridge 8766f72 → open-agents e2b0a83 submodule bump):**
both defenses landed bridge-side (the orchestrator never knew the worktree
path, so inheritance belongs where the worktree is created):
- `handleWorkflowTaskAssign` ignores a re-dispatch while the task's session
  is live (active + connected), re-emitting `task_started` so the
  orchestrator records the dispatch to this device — the healthy session is
  never touched;
- `CreateWorktree` is idempotent: an existing task worktree (linked `.git`
  file present) is reused instead of erroring, so any dispatch that does
  get through (e.g. after the first session died) lands in the same
  isolated directory rather than falling back to the bridge root.
Unit-verified (isLiveTaskSession + CreateWorktree idempotency tests; the
four affected packages green); live re-dispatch reproduction folded into
the next dogfood run.

**Cerberus consequence:** mission-fanout cases can spuriously stall on this
(any re-dispatch after a transient error while the first attempt is still
connecting); the 3-min stuck window in dogfood timings hides it.

## 21. Stuck missions' 10s scheduleNext retry alarms never expire → permanent DO alarm storm — FIXED 2026-08-26 (open-agents c3a0071)

Old missions with no online devices (dogfood history: job_1787595006700,
job_1787656433018, job_1787659470814, job_1787660490153 ×4, …) each
schedule a 10s 'scheduleNext' retry on every pass
(orchestrator.ts:150-151). The DO alarm queue sat at capacity (queued=16,
climbing 10→16 in the log) and fired CONTINUOUSLY (~30 alarms/s, one DO
invocation each) for hours. This degraded the whole worker during the
demo: device heartbeats 503 (3-4s latency), an orchestrator event POST at
5577ms, and a workerd internal disconnect ("write: Connection reset by
peer"). The degraded window is what reset t1 to pending and triggered the
#20 chain.

Missing defenses: retry alarms need a TTL/attempt cap (a mission offline
for >N hours must stop retrying and park as failed), and/or the 10s retry
should back off exponentially.

**Fix (2026-08-26, open-agents 57c36c7 merged as c3a0071):** the no-device
scheduleNext retry now backs off 10s → 30s → 90s → 270s → 5min ceiling,
attempt count riding the alarm payload (no new state). Ceiling is
deliberately the stuck-recovery cadence rather than a TTL give-up: there is
no device-online re-kick path, so a parking mission would never resume —
at the ceiling the storm cost is ~1 alarm/5min/mission (trivial) and the
mission self-resumes within 5 min of a device returning. Unit-verified
(orchestrator-scheduling.test.ts, 143 api test files green, tsc clean);
live storm-reproduction folded into the next dogfood run's baseline.

Bonus observation (same log): the demo bridge's WS died with 1006 at
01:02:23 and "Reconnect time budget exhausted (10m0s), giving up" — a
bridge that exhausts its reconnect budget stays a live-but-zombie process
forever (never exits, never reconnects). Related candidate, not yet filed
separately.

**Cerberus consequence:** long-lived wrangler instances accumulate the
storm across dogfood runs; degraded windows make otherwise-healthy
missions flap (spurious task_error/re-dispatch), which is noise the
Examiner sees as SUT flakiness.

## 22. Bridge becomes a zombie after its reconnect time budget is exhausted — FIXED 2026-08-26 (bridge def0acd, open-agents 35ae557: endless-but-backed-off retry picked — after the 10-min budget the readLoop falls back to one attempt every 5 min, interruptible by shutdown; the exhaustion announcement logs/notifies exactly once via an atomic mode flag, cleared on successful reconnect)

Demo bridge b2 (evidence /tmp/demo-ui-b2.out line ~108): WS died with 1006
at 01:02:23, the reconnect loop logged "Reconnect time budget exhausted
(10m0s), giving up", and the process stayed alive indefinitely — no WS, no
heartbeats (server-side it shows offline), but also no exit and no further
reconnect attempts. It holds task sessions, worktrees, and the device slot
as a half-live process: any task dispatched to it queues forever, and an
operator sees a "running" bridge that is dead.

Root cause (bridge internal/bridge/bridge.go:513-523): on
`HasExhaustedBudget()` the readLoop sets StateFailed, fires a reconnect
EventMaxRetry, and RETURNS — nothing else exits the process or schedules a
slow-poke retry. StateFailed is terminal in state.go's model.

Fix directions (pick one deliberately):
- endless-but-backed-off retry: after the 10-min budget, fall back to a
  slow keep-alive cadence (e.g. every 5 min — same ceiling philosophy as
  #21) so the bridge self-recovers when the server returns; or
- exit non-zero so whatever launched the bridge (shell wrapper, dogfood
  harness, future supervisor) can restart it.

**Cerberus consequence:** dogfood bridge actors that hit a long gateway
flap go zombie mid-run; downstream cases see device-offline semantics
(dispatch backs off) rather than a crash, so this hides as "device went
away" instead of surfacing as a restart — masking the defect family the
process_restart coverage is meant to exercise.

## 23. `ws-realtime-wf-mission-fanout` recurring flaky failure — root cause not yet pinned — OPEN 2026-08-26

Multi-device fan-out mission case (`mission_seed_cases.go`, 3-subtask
mission spanning all real bridges) has failed in 3 of the last 4 dogfood
runs targeting it: run 25, run 26 ("recurring ws_match family"), run 28
(this filing). Only run 27 was clean.

Run 28 evidence: the case completed (executing → completed) in 0.13s —
far too fast for a real fan-out (`http_request` create mission → 3×
`ws_receive workflow:task_progress` → 3× `task_completed`/`job_status`,
normally minutes). No `POST /api/missions` appears in the wrangler log
near the case's execution window, and no step-level evidence rows exist
for its trace_id — the case appears to fail before or during its first
step, with no observable server-side request. This does NOT correlate
with the open-agents #20/#21 fixes (both landed and re-verified clean in
run 27) or with a mid-run interrupt (this case failed at 14:49:16, ~10
minutes before an unrelated SIGINT hit the run's tail-end judging phase —
see the dogfood run docs).

**Why root cause is still open:** cerberus's `info`-level run logging
does not capture step-level detail (which step failed, why) for `ws_flow`
cases — only case start/completion. Pinning this needs either a
debug-level rerun of just this case, or step-level evidence to be logged
at `info` for `ws_flow` cases the way `browser_flow` steps already are
(spec 2026-08-26 §Evidence: `ui_action`/`ui_observe` frames).

**Cerberus-side observability shipped 2026-08-26** (step logging in
`runSteps`): every executed step now emits one `case step` info line
(case_id, 1-based step position, action, connection/type/method/url,
passed, latency, truncated summary, http status_code), and — the actual
blind spot — step-RESOLUTION failures (`stepToAction`/`resolveBrowserStep`/
`resolveHTTPStep`/`browser_shot`/capture errors) now log before the
historical zero-evidence early-return; the case-completion line carries
`error` via NamedError. Note this reframes the leading hypothesis: the
run-28 failure shape (0.13 s, no POST /api/missions, no evidence rows)
matches a resolution-phase failure — e.g. placeholder/http-token
resolution — not a dispatch defect; the next recurrence's `case step`
line will say which.

**Cerberus consequence:** none confirmed yet (the failure may be a genuine
open-agents defect in multi-device dispatch under contention, or a
cerberus-side harness issue in the stepped-case executor — undetermined).
Treat as noise for coverage/pass-rate purposes until root-caused; do not
let it block merges. Next dogfood run should capture debug-level logging
for this case specifically if it recurs.

## 24. Six independent zustand stores each un-cached-fetch `/api/settings` on mount — FIXED 2026-08-26 (open-agents 32c4175: shared `lib/settingsClient` — in-flight dedupe + 30s TTL + patch invalidation; all six call sites swapped; web suite 1477/1477. The intermittent ~30s navigation hang stays UNCONFIRMED and out of scope — no fix attempted, see investigation note below)

`weather-cities.ts`, `onboardingStore`, `securityAlertStore`, `themeStore`,
`notificationsStore`, `storageStore` each independently call
`api.get('/api/settings')` (and their own `api.patch`) against the SAME
single settings blob, with no shared fetch/cache layer. Live-observed via
Playwright request tracing: navigating the dashboard app fires 6 near-
simultaneous identical `GET /api/settings` requests per page mount, and
the count is cumulative per new store instance across navigations (6, 12,
18, 24, 30, 36 observed across 6 successive route changes in one browser
session) — each store re-runs its init fetch on its own lifecycle, not
deduped against sibling stores already holding the same data.

Root cause: no shared settings store/selector; six independent Zustand
stores each own a private slice-fetch of the same server resource.

**Investigation note (2026-08-26, corrected):** discovered while chasing an
intermittent ~30s navigation hang on `/dashboard` and
`/dashboard/prompt-lab` (browser-leg UI vocab work). Extensive
reproduction testing (7 genuine executions with Go test caching
explicitly ruled out via `-count=1` and per-run request tracing) showed
the hang is NOT deterministic on any single code path: the identical
`cerberus`-executor call (`BrowserExecutor.gotoPage`, `wait_until: load`,
no timeout override) hung twice and completed cleanly in ~75ms a third
time in back-to-back runs against a freshly-restarted, otherwise-idle
wrangler+vite stack. An earlier working theory ("only the production
`be.Execute` path hangs, raw `page.Goto()` never does") was directly
falsified once caching was ruled out — both paths hang sometimes, succeed
sometimes. Root cause remains genuinely unconfirmed; this settings-fetch
storm is the one CONFIRMED, deterministically-reproducible finding from
the investigation — plausible (not proven) as a contributing factor when
its 6 near-simultaneous requests happen to land during the same window as
a page's `load` event resolution, not a standalone crash risk on its own.
No fix was attempted for the navigation hang itself: there is no reliable
way to force the failure on demand, so a "fix" (e.g. a goto retry) could
not be verified to actually help — shipping it would be an unverified
guess, not a resolution. If this recurs with enough frequency to be
worth the investigation cost, the next productive step is Chrome
DevTools Protocol-level network waterfall capture during a live-caught
hang, not another black-box repro attempt.

**Cerberus consequence:** none confirmed (no case currently asserts on
`/api/settings` call count); flagging for awareness since browser-leg
navigation timing assumptions (goto with `wait_until: load`) could
occasionally flake under real contention if this compounds with other
concurrent load. The two affected vocab assertions
(dashboard-home-quick-actions, prompt-lab-title) are kept in the dogfood
vocab rather than dropped — the flake rate observed (2 hangs in 7 runs,
~29%) is not zero but the pages themselves are not broken, and dropping
real coverage over an unconfirmed intermittent issue trades a known cost
(reduced UI surface coverage) for an unquantified one.
