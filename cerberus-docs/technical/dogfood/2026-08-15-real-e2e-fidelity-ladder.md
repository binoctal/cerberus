# Real E2E Fidelity Ladder — Session Report (cerberus ↔ open-agents)

Date: 2026-08-15
Branch: `feat/fidelity-manifest-real-e2e`
Spec: `cerberus-docs/superpowers/specs/2026-08-14-fidelity-manifest-real-e2e-design.md`

## What was proven

The fidelity ladder replaced self-played bridge coverage with real processes:

| Tier | Test | Proof |
|---|---|---|
| L1 | `TestRealBridge_L1_PTYSessions` (integration) | Real bridge process spawns a real subprocess in a real PTY (claude-pty path + deterministic shim), output flows through BOTH batching layers (bridge 500ms + DO 50ms) as `chat:response`, clean stop. 4/4 consecutive passes. |
| M1 | `TestRealBridge_M1_Orchestration` (integration) | Two real bridges; deterministic D1-seeded task graph (zero planner LLM); real orchestrator (via `POST /api/missions/:id/resume`) assigns tasks to real devices; real bridges create git worktrees, spawn the ACP CLI, and their task lifecycle events flow back DO→web. 2/2 passes. |
| L2 | `TestRealBridge_L2_RealCLIScheduled` (integration) | **The core promise**: real `claude` CLI scheduled via ACP (`npx @agentclientprotocol/claude-agent-acp`) with GLM credentials; web prompt → ACP session/new + prompt → real model output `"READY"` streams back (`protocol:"acp"`). 2/3 passes (one stop-case timeout, core assertion passed all runs). |

cerberus-side features shipped: per-actor `fidelity: emulated|real-process` manifest with validation, run-summary fidelity watermark (`Real actors: ...` / `emulated-only`), a generic external-process actor harness (setup/capture/start/ready/group-teardown), and deterministic-case suppression for roles occupied by real processes.

## open-agents findings (all discovered by the ladder, 2026-08-15)

1. **Protocol auto-detect blocks plain PTY sessions** — `session:start` with an arbitrary existing binary as cliType (e.g. `bash`) always picks the ACP stdio adapter (`internal/protocol/manager.go` tries ACP first for every command; only the hardcoded `claude-pty` forces PTY). Plain-command PTY sessions are unreachable from the message surface.
2. **Internal orchestrator endpoints are dead in production path** — `/api/missions/internal/orchestrator/*` are JWT-exempt (the DO's callback carries no JWT) but `getOrchestrator` binds `c.get('userId')` → `undefined` → every alarm 500s with `D1_TYPE_ERROR`. The DO alarm chain (decompose/orchestrator callbacks) cannot work as wired.
3. **Load balancer can assign to the busier device** — `selectDevice` sorts candidates least-loaded-first but then indexes with a persistent round-robin counter; observed both tasks of a mission landing on the same device while a second idle device was a candidate (`services/orchestrator.ts`).
4. **ACP adapter rejects relative cwd** — mission-dispatched tasks pass a relative worktree path; the ACP CLI answers `Invalid params: cwd must be an absolute path`, so every mission task fails at session creation on the bridge.
5. (Data, dev DB) `subscription_plans.pro` ships with `max_concurrent_tasks: 0` → the scheduler is a no-op until the value is lifted.

## How to run

```
# 1. api server (port 8989)
cd ../open-agents/apps/api && setsid npm run dev   # fnm node 22
# 2. .dev.vars: INTERNAL_SECRET must be set; API_BASE_URL must be EMPTY
#    (it hijacks DO→Worker callbacks to the Gap-E capture server)
# 3. bridge binary
cd ../open-agents/bridge && make build
# 4. ladder
go test -tags integration ./internal/head/agent/ -run 'TestRealBridge_(L1|M1|L2)' -v -timeout 8m
# L2 additionally needs ANTHROPIC_AUTH_TOKEN + ANTHROPIC_BASE_URL (GLM)
```

Env mutations left in place: `.dev.vars` gained `INTERNAL_SECRET=cerberus-dogfood-secret` and lost `API_BASE_URL` (backup at `/tmp/dev-vars-backup-20260814`); the dev D1's `pro` plan now has `max_concurrent_tasks: 3`.

## Deferred (explicit hand-off)

- **realE2E deterministic case generator + autonomous run in `dogfood/realtime-e2e/`** (plan Task 7 steps 3-4). Prerequisites now known: harness env needs the claude shim on PATH for zero-LLM runs (add a `{{env.X}}` template or a `shim` concept to ProcessSpec); message shapes are `{type, payload{...}}` with `payload.deviceId` for routing; `session:send` uses payload key `content`; PTY output returns as `chat:response`.
- Negative/exception case family, HTTP route vocab extraction, Examiner ordering/count dimensions (plan Task 8).
- M2 mixed ACP+PTY capability-matched scheduling.
- The four open-agents findings above → file as issues in the open-agents repo.
