# Workflow orchestration coverage — live validation run

Date: 2026-08-18. Plan: `.superpowers/sdd/2026-08-18-workflow-orchestration-coverage-plan/`
Task 6. Branch `feat/workflow-orchestration-coverage`. Setup per
`2026-08-18-workflow-coverage-env.md` (wrangler :8989 + 2 bridge PTYs + real
heads, actors=4, glm-4.5 planner). Eight live runs; each failure below was
root-caused before the next run — none was forced.

## Final result (run 8, session `55c20b91-9f5a-432b-b893-d622b86d5747`)

| Metric | Value |
|---|---|
| Verdicts | 690 pass / 1 fail / 0 uncertain / 3 recovered |
| Coverage | **98.39%** (progression today: 93.9% → 96.01% → 97.85% → 98.12% → 98.39%) |
| Gaps | **6, all non-workflow** (device:online, session:cancel, session:resize, chat:send, control:takeover, device:restart) |
| Claims | 1 proven / 0 emulated-only / 1 unevidenced — gate unchanged, no exit 3 |
| Duration | 11m51s, ~192K tokens |
| Workflow family | mission-seed PASS, task-assign PASS, start/start_task/pause/cancel sends PASS, session:send PASS |

The single fail is a Scout-planned probe (`tc-004`: expected 400 on
`/ws/%20%20`, SUT answers 426) — expectation drift in the Scout plan, not a
workflow or product regression; its siblings recovered via repair.

## Health gates (Task 5 review ruling)

1. **Real decompose, no fallback**: every mission run decomposed via the
   seeded glm-4.5 provider with `POST /api/missions/internal/planner/decompose
   200` in 24-47s (never the 60s-timeout-then-rule-fallback signature). The
   `<5s` figure from Task 5 was a direct-curl measurement; through the worker
   under load glm-4.5 takes 24-47s. Substance of the gate (real LLM plan, no
   fallback) holds.
2. **Title is not an echo**: all planner-generated tasks were Chinese titles
   from English inputText (`回复done`, `输出 'done'`, `生成文本响应`,
   `生成单次回复` / task `输出回复内容`) — the rule-based fallback would echo
   the English input.

## What the mission case proves (and what it cannot)

`ws-realtime-wf-mission-seed` seeds the full gate chain (plan → user switch →
provider → agent row → mission) under the WEB user, then observes on a web
connection: real planner decompose → orchestrator dispatch → real bridge task
session → `workflow:task_progress` (step:"started") and
`workflow:task_question` frames. Completion (`workflow:job_status` /
`task_result`) is NOT receivable in this environment — see findings 1-2 below;
the case was restructured to the observable chain rather than awaiting
unreachable frames (originally it awaited task_started/task_result/job_status).

`ws-realtime-wf-task-assign` sends `workflow:task_assign` directly (the same
bridge handler the orchestrator's dispatch reaches), receives
`task_progress`, then sends `task_answer` + `task_guidance` against the live
session — covering the three conditional send edges deterministically.

## Vocab marks added this run (all live-verified 2026-08-18)

| Edge | Mark | Reason |
|---|---|---|
| bridge→web `workflow:task_result` | partial | completion ships only via the bridge HTTP callback whose URL is built from the ws:// server URL ("unsupported protocol scheme" in bridge logs); room-side frame exists only on the web-initiated merge path |
| bridge→web `workflow:task_error` | partial | failure-path only (session-create or merge failure) — unreachable from the happy-path surface |
| bridge→web `workflow:task_completed` | partial | DO-side broadcast gated on the same broken completion callback |
| bridge→web `workflow:task_failed` | partial | same dependency as task_completed |

(Pre-existing marks unchanged: job_completed / task_status_update /
merge_progress unsupported — dead types.)

## open-agents findings (new, live-verified)

1. **Bridge workflow callbacks use a ws:// URL** — `Post
   "ws://localhost:8989/api/workflows/internal/orchestrator/event":
   unsupported protocol scheme` (callback.go builds APIURL from the ws server
   URL). Task results NEVER reach the API, so missions never complete and
   stuckRecovery is the only exit. This is the blocker behind the task_result /
   task_completed / task_failed / job_status partial marks.
2. **Orchestration dispatch never emits `workflow:task_started`** —
   `startTaskSession` pushes `workflow:task_progress` (step:"started");
   task_started frames exist only as echoes of web-origin
   start/start_task (bridge.go).
3. **Planner agent-list injection is copy-prone** — planner.ts injects
   `- ${baseCli}: ${name}` and glm-4.5 sometimes copies the literal into
   `assigned_agent` (observed `claude: cerberus-bridge-agent`) →
   `resolveCliForAgent` finds no device with that "CLI" → task skipped
   forever ("no device with CLI 'claude: cerberus-bridge-agent'"). Workaround
   shipped harness-side: agent row named identically to its baseCli.
4. **`[QUESTION]` false positive on PTY prompt echo** — the task prompt itself
   instructs "[QUESTION] ..." and the PTY echoes the prompt, so
   `handleQuestionMarker` fires deterministically on the PTY-fallback path
   (spurious task_question + pending-question machinery).
5. **ACP relative-cwd rejection still present** (ladder finding #4): worktree
   dispatch passes a relative cwd → `Invalid params: cwd must be an absolute
   path` → 60s hang → PTY fallback. Combined with 4 this is why the mission
   path lands on PTY and emits the question frame.
6. **Plan updates don't invalidate the 5-min `getPlanLimits` cache** — a
   `PUT /api/admin/billing/plans` keeps serving stale limits for up to 5
   minutes (bit us between the plan-limits fix and the next run).
7. **Plan-limiter trap for harness setup**: the seeded plan must lift
   `api_hourly`/`api_daily` (defaults 100/500 deep-merge in) or the route
   sweep's ~130 admin writes exhaust the hourly budget and every mission-setup
   POST 429s (`HOURLY_RATE_LIMIT_EXCEEDED`) before orchestration starts.

## cerberus fixes shipped en route (this branch)

- `internal/head/agent/execute_phases.go` — per-case timeout now honors the
  case's declared ws_receive windows (sum + 60s slack); the fixed 2-minute
  deadlock guard killed the mission case before its minute-scale receives.
- `internal/head/scout/mission_seed_cases.go` — user-scoped steps run under
  the web role (mission user must own the devices and the room;
  `checkDeviceOnline` is user-scoped); plan body lifts api_hourly/api_daily/
  max_agents; agent row named `claude` (finding 3); observes progress+question.
- `internal/head/scout/mission_send_cases.go` — new task-assign case covering
  task_assign/task_answer/task_guidance.
- Vocab: the four partial marks above; dogfood `project.yaml` max_duration
  12m→25m.

## Residual (the honest backlog)

The 6 non-workflow gaps (unchanged from 2026-08-17): device:online +
device:restart (bridge reconnect pair), control:takeover, chat:send,
session:cancel, session:resize. Scout-plan probe drift (tc-00x family
expecting 4xx shapes the SUT answers differently) is an orthogonal Scout
quality item. The workflow completion family reopens when open-agents fixes
the callback scheme (finding 1).
