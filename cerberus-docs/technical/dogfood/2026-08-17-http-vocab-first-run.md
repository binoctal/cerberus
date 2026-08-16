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
