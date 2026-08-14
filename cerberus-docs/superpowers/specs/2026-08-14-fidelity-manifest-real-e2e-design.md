# Fidelity Manifest & Real E2E Design (open-agents second-track dogfood)

Date: 2026-08-14
Status: approved (chat review, session 2026-08-14)

## Background

ws-realtime dogfood reached coverage 1.0 (40 pass / 0 fail), but the metric only
enumerates WS relay edges from `open-agents/apps/api/src/realtime/room.ts` — and
the bridge side is **played by cerberus itself**. open-agents' core promise —
*scheduling and orchestrating real AI CLIs across devices* — was never verified:
no real bridge process, no real CLI, single device. This design closes that gap
with a fidelity ladder while keeping the emulated layer (breadth, negatives).

## Core decisions

1. **Fidelity manifest (per-actor, declarative)** — `Actor.fidelity:
   emulated | real-process` in project.yaml. Default `emulated` (backwards
   compatible). A `process:` block is required iff `real-process`. Run summary
   carries the fidelity composition; a 100%-emulated run is watermarked
   `emulated-only` so "coverage 1.0" can never again silently mean "self-played".
2. **External process actor harness (generic, SUT facts live in YAML)** —
   cerberus learns nothing about open-agents. The harness runs a `setup` command
   (pairing), reads back a JSON file to capture runtime facts (deviceId) into
   the actor's path params, launches a `start` command in its own process group
   with an overridden env (isolated `HOME`), waits for a readiness pattern on
   stdout/stderr, and kills the group at session finalize.
3. **Cross-actor templating** — `{{<actor>.<param>}}` in ws_send bodies so the
   web actor can route to `{{bridge-pty-1.deviceId}}` (real bridge's captured id).
4. **Ladder, not a swamp** — L1 (real bridge + PTY `bash`: spawn → dual batching
   → persistence → stop) → M1 (2 real bridges, D1-seeded deterministic task
   graph + `scheduleNext` alarm: assignment routing / parallelism, zero LLM at
   orchestration tier) → L2 (real `claude` CLI via GLM credentials: one bounded
   prompt, real streaming back to web). M2 (mixed ACP+PTY capability matching)
   only if L2 is stable.
5. **Emulated layer stays** — negatives (rate limits, error frames, bad tokens)
   and breadth belong to the emulated track; deferred items are tracked in the
   plan, not dropped.

## Key SUT facts this design relies on (verified 2026-08-14)

- `POST /api/dev/setup` reuses the dev user and creates a NEW device per call —
  N bridges pair into the same user's DO room (`worker.ts:102-170`).
- Bridge config is `$HOME/.open-agents-bridge/config.json`; no env override, so
  per-instance `HOME` isolates instances (`internal/config/config.go:148`).
- `pair --dev --server <url> -d <name>` saves a named device; `start -d <name>`
  runs it (`cmd_pair.go:179`, `cmd_start.go:25`).
- `session:start` accepts an optional `command` (PTY arbitrary process) and the
  CLI registry maps `claude`→`claude` binary (`cli_detect.go:11`).
- Missions gate: `users.plan` must enable `workflows`; orchestrator alarm
  `scheduleNext {missionId}` drives deterministic scheduling
  (`routes/missions.ts:58`, `services/orchestrator.ts:84`).
- Local dev env gotchas: `.dev.vars` has NO `INTERNAL_SECRET` (internal routes
  403) and `API_BASE_URL` points at a cerberus capture server (hijacks DO
  callbacks) — both must be fixed in pre-flight, then restored.

## Scope / non-goals

Non-goals for this track: real Go-bridge reconnect/heartbeat soak, A2A, admin
surface, HTTP vocab extraction, Examiner ordering/count dimensions (deferred,
tracked in plan Task 8).
