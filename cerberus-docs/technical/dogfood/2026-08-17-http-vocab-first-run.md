# HTTP Route Vocabulary — First Live Run

Date: 2026-08-17
Session: `fd71b36b-58c6-4517-9b7f-4c07ba2117d9` (realtime-e2e, all-real:
wrangler :8989 + 2 bridge PTYs, glm-5.3[1m] heads)
Log: task output `bb17atkpg` / `.cerberus/runtime/logs/cerberus-2026-08-17.log`

## Setup

- Vocab: `dogfood/realtime-e2e/.cerberus/vocab/open-agents.vocab.yaml` — first
  merged extraction (d16fc14): 70 WS edges (room.ts) + 337 HTTP routes
  (worker.ts, 60 files hashed). Multi-entry `--from` shipped for this (0de560a).
- This is the first run whose path-coverage denominator includes the HTTP
  surface.

## Results

| Metric | Value |
|---|---|
| Verdicts | 2 pass / 0 fail / 0 uncertain (correctness 0.85, 0.93) |
| Coverage | **0.75%** (was 1.0 pre-vocab — honest collapse, by design) |
| Gaps | 398 = 61 WS edges + **337 HTTP routes (0 exercised)** |
| Required | ~400 (63 WS edges qualified + 337 routes) |
| Claims | 1 proven / 0 emulated-only / 1 unevidenced (gate OK) |
| Duration | 4m04s, ~21K tokens |

The 0-exercised routes are expected: this run's generated cases were WS-only
(the HTTP generator is deliberately v2). Attribution itself was exercised in
unit tests; the live gap list is the feature working as intended.

## Route gap distribution (generator input)

By method: GET 133, POST 131, PUT 30, DELETE 37, PATCH 5, ALL 1.

By top path segment:

| Segment | Gaps | Note |
|---|---|---|
| /api/admin/* | 166 (49%) | admin-gated; needs an admin-role credential before any generator can touch half the surface |
| /api/missions/* | 32 | core product surface, workflow:* ↔ multiagent_* ↔ /api/missions fork (known issue #4) |
| /api/sessions/* | 15 | |
| /api/teams/* | 15 | |
| /api/auth/* | 14 | public + JWT; /api/auth/dev/setup returns a JWT directly |
| /api/devices/* | 13 | |
| others (20 segments) | 82 | prompts, skills, bridge, agents, permissions, … |

163 of 337 routes carry `:param` path segments (extraction-time count) — a
generator needs param provisioning (capture/reuse from case setup) for half
the surface.

## Judge drift under new dimensions (thin evidence)

2 cases, 0 uncertain/fail — no drift regression observed from the new
count/ordering dimensions (7442490). Two cases is too thin to claim
improvement; the fail/uncertain/under-conf split needs a wider run (e.g. the
negative-family suite) before closing the drift question.

## Next steps

1. HTTP case generator (v2) — start with non-admin, non-param routes
   (GET/POST on flat paths ≈ 60 routes), the cheapest honest coverage.
2. Admin credential in dogfood project.yaml to unlock the 166-route admin
   block.
3. Wider run for the drift split.

---

## Update 2026-08-17 (evening): route sweep generator live

Branch `feat/http-route-generator` (c358963): `httpRouteCases` emits one
bare-client reachability smoke per non-exempt vocab route
(`expect_status_class: any` — transport errors fail, any response passes),
honesty-tier expectations, `ALL`→GET, emitted independent of the WS protocol
gate.

Second live run (same setup, 24m25s, ~194K tokens):

| Metric | Before sweep | After sweep |
|---|---|---|
| Verdicts | 2 pass | **676 pass / 0 fail / 0 uncertain** |
| Coverage | 0.75% | **85%** |
| HTTP route gaps | 337 | **0** |
| WS edge gaps | 61 | 61 (unchanged) |

All 337 routes exercised, zero transport failures. The 61 residual gaps are
WS edges (same set as pre-sweep — server-push/batch edges needing live
conditions), untouched by the sweep.

Drift: 0 uncertain across 676 LLM-judged cases (route cases judged at
correctness 0.95) — the count/ordering dimensions plus the sweep show no
drift regression on this corpus (route cases are simple; still not a
stress test of the dims).

Side-effect note: public mutation routes were really hit with empty bodies
(dev DB junk accepted, as designed).

Next: admin credential to upgrade the 166 admin routes from 401-reachability
to authenticated semantics; WS-edge gap burn-down.

---

## Update 2026-08-17 (night): gap burn-down + admin + drift

Branch `feat/real-role-send-credit` (1cd4a4f..): three backlog items closed
in one validation cycle (runs 4-9; runs 4-6 hit two harness bugs — client-role
selection picked the http_only admin role; stale binary — both fixed en route).

| Move | What |
|---|---|
| Send-side credit | edges whose ToRole is a real-process actor credit on the web-side `ws_send` (the recipient's socket is unobservable); emulated recipients stay receive-driven |
| Real-responder cases | declarative sync-family exchanges the REAL bridge answers (`responses:` in protocol yaml; request_payload values parse as raw JSON — the handlers type-assert arrays/objects and silently skip on shape mismatch) |
| Admin JWT | admin-actor (superadmin via `/api/auth/dev/setup`, distinct email) + `http_only` protocol role; 166 admin routes now send authenticated requests |
| Vocab marks | 19 partial (needs ACP CLI / real-CLI events / encryption / scanner events — notes in vocab yaml), 4 unsupported (device:listDir pair, merge_progress, scanner:rules:synced — new DO-drop found: web→bridge `scanner:rules:sync` is not whitelisted, added to known-issues #1) |

Final run (run9): **683 pass / 0 fail / 0 uncertain / 3 recovered, coverage
93.9%** (24 gaps), 6m35s, claims 1 proven / 0 emulated-only.

Coverage progression today: 0.75% → 85% (route sweep) → 88.7% (send-credit +
sync pairs) → 93.9% (marks + fixes).

Drift corpus: ~683 LLM-judged cases across 5 runs, 0 uncertain — no drift
regression from the count/ordering dimensions on this corpus. Route cases are
individually simple; the drift question stays open for richer exchanges.

The 24 residual gaps are the honest product backlog: 17 `workflow:*` edges
(need mission-orchestration seeding, the M2 flow), `web→web session:send`
(second web connection), `device:restart`/`device:online` (bridge reconnect
pair), `control:takeover`, and the chat-send/cancel/resize session commands.
