# Workflow Orchestration Coverage — Design

Date: 2026-08-18
Status: reviewed (2 adversarial review rounds, 23 findings incorporated)
Target: dogfood `realtime-e2e` (open-agents), coverage 93.9% → ~98.5%

## Background

After the 2026-08-17 gap burn-down (17c4607), 24 coverage gaps remain. 17 are
`workflow:*` WS edges of the mission-orchestration subsystem — the three-name
fork `workflow:*` (wire) ↔ `multiagent_*` (DB) ↔ `/api/missions` (HTTP)
(known issue #4). The rest: `web→web session:send`, `device:restart` /
`device:online`, `control:takeover`, chat-send/cancel/resize commands.

This design covers the 17 workflow edges plus `session:send` (18 of 24).
The remaining 6 stay honest gaps for a later cycle.

The full orchestration chain was proven feasible in M2 (mission → planner
decompose → `task_assign` → real bridge executes in worktree → `task_started` /
`task_progress` / `task_result` → merge). Nothing here re-proves that; it wires
the chain into cerberus case generation.

## Goal / Non-goals

**Goal:** one mission-seeded case family + a minimal web WS actor that
exercises the workflow family against the live open-agents stack, with
coverage attribution through existing mechanisms (receive-driven credit,
send-side credit, vocab partial/unsupported marks).

**Non-goals:** a real browser frontend actor; orchestration state-machine
modeling in protocol yaml; the 6 non-workflow gaps; any change to
attribution rules in `internal/session/coverage.go`.

## Verified facts (design constraints)

All file:line verified against both working trees, 2026-08-18.

### open-agents gating chain — everything must be seeded or the mission 403s/stalls

1. **Plan gate.** `POST /api/missions` checks `isFeatureEnabled(db, plan,
   'workflows')` (missions.ts:66-71). Dev-created users are plan `free`
   (dev.ts:58; admin-actor setup leaves the schema default). No active
   migration seeds `subscription_plans`, so the fallback gate is
   `workflows:false` (plan-limits.ts:49) — a bare mission POST 403s.
2. **Plan seed payload.** `POST /api/admin/billing/plans` (admin.ts:116 →
   billing/index.ts:10 → plans.ts:57), zod `{name, price_monthly, limits?}`
   (plans.ts:14-24). `limits` is one JSON record holding both
   `feature_gates` and `rate_limits` (plan-limits.ts:87-104 deep-merges with
   fallbacks). Safe payload:
   `{"feature_gates":{"workflows":true},"rate_limits":{"daily_missions":9999}}`.
   **Never include `max_concurrent_tasks`**: dispatch takes
   `min(planMax, dbMax)` (missions.ts:439-447) and `scheduleNextTasks`
   stalls at 0 (orchestrator.ts:143-146); the archived legacy pro seed JSON
   carries exactly the 0 trap.
3. **Plan/user wiring.** The plan id is server-generated (`generateId`,
   plans.ts:60) — read it back, then `PUT /api/admin/users/:id` with
   `{plan: "<id>"}` (users.ts:130-139). `isFeatureEnabled` reads the exact
   row `WHERE id = ? AND is_active = 1` (plan-limits.ts:87-95). Plan cache
   TTL is 5 min (plan-limits.ts:57): new plan ids are uncached, but seed
   plan → set user → create mission must happen within one run.
   Budget check passes with no `budget_limits` rows (ai-cost-service.ts:
   170-172) — not a blocker in dev.
4. **Planner provider.** `resolveAiConfig` reads `ai_providers`
   (ai-provider-service.ts:92-120), taking `models[0].id`. The admin create
   route `POST /api/admin/ai-providers` (admin.ts:152, ai-providers.ts:107)
   requires `PROVIDER_KEY_KEK` and encrypts at rest — plaintext pass-through
   exists only on the decrypt side for pre-existing rows. The dogfood harness
   must set `PROVIDER_KEY_KEK` in `.dev.vars` before wrangler starts. Minimal
   valid payload: `models: [{id, display_name, input_price_per_million,
   output_price_per_million}]` (min 1), valid `api_url`, plaintext
   `api_key` (ai-providers.ts:27-36).
5. **Agent row (stall guard).** The bridge never self-registers an `agents`
   row (verified: no such INSERT in bridge Go code). Dispatch filters devices
   by `cliEnabled` for `requiredCli` (orchestrator.ts:378-390);
   `resolveCliForAgent` falls back to matching agent **id or name scoped to
   the mission user** (orchestrator.ts:365-369); `getAvailableAgents` needs
   the user's rows plus a device `last_seen` within 60 s (agents.ts:154-165;
   bridges write `last_seen` at connect, worker.ts:385-402). Empty table →
   infinite "no device with CLI … skipping" — no `job_completed`, no
   `task_failed`, silent stall. Seed via the **user-scoped**
   `POST /api/agents` (worker.ts:299, agents.ts:66-100) with the mission
   user's JWT, `baseCli` matching one of the bridge PTY's `cliEnabled`
   capabilities.
6. **Mission create.** `POST /api/missions` with `{inputText, deviceIds:
   [bridgeId], autoConfirm: true}` — `resolveCandidateDevices` prefers
   `device_ids` (orchestrator.ts:309-326); `autoConfirm:true` → status
   `running` + immediate `startMission` (missions.ts:425, 436-448), false
   would park in `planning`. POST needs the Origin header (existing actors
   already set it). Planner failure broadcasts `mission:decompose_failed`
   (missions.ts:326) — **not a vocab type, credits nothing**: there is no
   cheap fallback; the seeding chain must work.

### Wire semantics — what the edges actually do

7. **Web-origin workflow sends are web→bridge relays, not web→server.**
   room.ts:420-441 routes `workflow:start/pause/cancel/start_task/
   task_assign/task_answer/task_guidance` through `sendToBridge` keyed on
   `payload.deviceId`. The vocab already models these as web→bridge
   (vocab:826-930). There is no `state_updated` reply to web sends.
8. **Bridge echo behavior.** `start` emits `workflow:task_started` only per
   `payload.tasks` item (bridge.go:2601-2627) — empty/missing `tasks` gives
   zero output and no error frame; the step payload must carry
   `{jobId, deviceId, tasks:[{id}]}`. `start_task` echoes `task_started`
   unconditionally (bridge.go:2657-2674). `pause`/`cancel` only log
   (bridge.go:2631-2649) — **no observable output**; their only coverage
   path is send-side credit. `task_answer` acts only on a pendingQuestion
   or live session (bridge.go:3097-3122); `task_guidance` only on a live
   session (3127-3140).
9. **Dead types.** `workflow:job_completed` (room.ts:375) and
   `workflow:task_status_update` (room.ts:381) exist only in the whitelist —
   no emitter in apps/api or bridge. Completion is signalled by
   `workflow:job_status {status: completed|failed}` (orchestrator.ts:949,
   finalizeMissionIfDone) and `workflow:state_updated` (orchestrator.ts:1073),
   both broadcast DO-side, outside the room.ts case handler. These belong to
   the DO-drop family (known issue #1).

### cerberus mechanics

10. **Send-side credit** (coverage.go:286-293) requires only that the
    sender's connection maps to the declared FromRole (`connRole`, from the
    case's own `ws_connect`, coverage.go:255-260) and that the edge's ToRole
    is a real process (`byFromTypeReal`, coverage.go:242-248). The sender
    itself need not be real: sends from the cerberus-owned web connection
    credit web→bridge edges as intended.
11. **Vocab marks.** `partial` / `unsupported` are VocabEdge fields
    (vocabulary.go:52-53); `requiredEdges` excludes both (coverage.go:365) —
    same mechanism as the existing 19 partial marks.
12. **Timeouts.** `WSReceiveAction.Timeout` is per-step seconds, default 10
    (actions_http.go:240-241, websocket.go:1093-1096). A failed receive
    returns OK:false — conditional waits must not be hard receives. The pump
    keeps connections alive across receive timeouts (websocket.go:167-186,
    1174-1177); frames persist as `StepResult.Evidence` events per case, so
    the Examiner sees them. No session-level cap kills a ~10-min case.
13. **Claims gate.** The ledger has 2 claims: `ws-relay-messaging` (critical,
    proven) and `schedule-real-cli` (wont-test). ReconcileClaims scores only
    cases bound to a claim (claims_reconcile.go:119-122) — coverage edges
    never touch the gate. The web actor is emulated-tier, but the mission
    case reaches `evidenceReal` via deviceId-routed sends if ever bound.
    Do not bind the mission case to `schedule-real-cli`.
14. **`session:send`.** Payload needs `deviceId` (else `MISSING_DEVICE_ID`,
    room.ts:449-460); `broadcastToWeb` excludes the sender, so a second web
    connection receives it. Both connections authenticate with `demo_token`
    (dev-only, room.ts:631-633; wrangler.toml `ENVIRONMENT="development"`).

## Design

### New case family: `missionSeedCases`

Attached in `WSCasesCovered` next to `httpRouteCases` / `realResponderCases`,
gated on the service declaring workflow-family vocab edges. One case per run
(single mission; coverage is session-wide, verdicts per-case — see isolation
note below). Steps, in order:

1. **Setup (HTTP, reusing the route-sweep JWT request machinery):**
   a. (admin JWT) create plan — payload as §2; read back the id
   b. (admin JWT) `PUT /api/admin/users/:id {plan}`
   c. (admin JWT) `POST /api/admin/ai-providers` — `api_key` from the run
      env's `ANTHROPIC_AUTH_TOKEN`, `api_url` pointing at the same
      Anthropic-compatible endpoint the run already uses; model id picked
      during implementation (see open items)
   d. (mission-user JWT) `POST /api/agents` — `baseCli` = a `cliEnabled`
      capability of the connected bridge
   e. (mission-user JWT) `POST /api/missions` `{inputText: minimal,
      deviceIds: [bridgeId], autoConfirm: true}`
2. **Web actor connects** (demo_token, `web` role) — plus a **second** web
   connection for `session:send`.
3. **Interleaved sends and receives** (single case; no cross-case timing):
   - `workflow:start_task` → hard receive `workflow:task_started`
     (deterministic echo)
   - `workflow:start` with `{jobId, deviceId, tasks:[{id}]}` → hard receive
     `task_started`
   - `workflow:pause`, `workflow:cancel` → sends only; credit is send-side
     (§10), no receive expectation (nothing observable, §8)
   - `session:send` from connection 1 → hard receive on connection 2
   - hard receives for the deterministic orchestration pushes:
     `task_started`, `task_progress`, `task_result`, and the completion
     signal `workflow:job_status` (out-of-band type — matched by type even
     though undeclared in vocab)
   - per-step `Timeout` explicitly large (minutes), per §12
4. **Conditional edges** (`task_question`, `task_answer`, `task_guidance`,
   `merge_conflict` family) are **not** hard receives; they stay as vocab
   partial marks with notes (§11). Frame captures still land in evidence if
   they occur.

Isolation note: the mission case is one Steps sequence so seeding, sends,
and receives share one verdict — no TOCTOU between cancel and assign, no
reliance on case ordering.

### Web actor

Not a new executor. A `web`-role WS connection created with the existing
`ActionWSConnect` machinery (demo_token auth), able to run declared
`ActionWSSend`/`ActionWSReceive` steps. This is the mirror of
`realResponderCases` on the web side: the real process there was the
responder; here the real bridge is the recipient and the web connection is
the declared-role sender. No change to honesty tiers: the web actor is
emulated-tier (project.yaml `real` lists only bridge processes), which is
sufficient because credit flows through §10/§11.

### Vocab / protocol updates (data only)

- Mark `workflow:job_completed`, `workflow:task_status_update`
  `unsupported` with notes naming the dead-code finding; add known-issue
  #5 to the open-agents issues doc (DO-drop family).
- Note the `workflow:job_status` / `state_updated` DO-side broadcasts in
  the vocab notes (undeclared but matchable).

### Examiner expectations

Honesty-tier expectations, route-sweep style: hard receives assert frame
arrival + type match; frame contents (progress values, result artifacts)
are judged, not string-compared. The mission case verdict is pass iff all
hard receives fire within their windows and the completion signal arrives.

### Success criteria (live realtime-e2e run)

- Mission reaches completion: `workflow:job_status {status: completed}`
  received (bridge executed a real task end-to-end)
- Coverage: 15 workflow edges covered + 2 marked unsupported +
  `session:send` covered; denominator shrinks with the marks
  (N≈393 → ≈391), residual 6 non-workflow gaps → expected ~98.5%
- 0 fail verdicts; claims gate unaffected (1 proven / 0 emulated-only)

## Error handling

- Any setup step 4xx/5xx fails the case with the response body in evidence
  (the seeding chain has no cheap fallback, §6)
- Completion window timeout → case fails (not partial) — a stalled mission
  is a real defect signal (§5's stall mode was a seeding bug, now guarded)
- Second run idempotency: plan/agent/provider creation is not idempotent —
  the family tolerates duplicates (list-then-create if a cheap list route
  exists; otherwise accept duplicate rows; daily_missions=9999 absorbs
  repeat runs)

## Testing

Unit (mock LLM, deterministic):
- setup step generation: plan payload shape (no `max_concurrent_tasks`),
  read-back wiring, user plan update ordering
- case assembly: single-case Steps ordering, per-step large timeouts,
  send/receive pairing per §8 semantics
- vocab marks: dead types excluded from requiredEdges

Live: one realtime-e2e run (wrangler :8989 + bridge PTYs + real heads),
success per above. Update the dogfood run doc with the coverage progression
and the new known issue.

## Open items

- Exact minimal `inputText` that decomposes to one short task reliably —
  pick during implementation against the real planner
- Planner model id + api_url (the provider payload's `models[0].id` must
  match what the endpoint actually serves)
- Whether a cheap admin list route exists for provider/plan idempotency —
  decide during implementation
- The 6 remaining gaps (device restart pair, takeover, chat commands) —
  next cycle
