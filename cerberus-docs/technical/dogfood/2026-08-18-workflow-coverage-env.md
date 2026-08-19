# Workflow-orchestration coverage — live environment wiring

Date: 2026-08-18. Plan: `.superpowers/sdd/2026-08-18-workflow-orchestration-coverage-plan/` Task 5.
Scope: everything the mission seed chain (`internal/head/scout/mission_seed_cases.go`)
needs from the LIVE environment. Verified end to end by hand with curl (see the
task-5 report for the transcript); nothing here is a committed Go test.

## Env script

`scripts/dogfood-realtime-e2e-env.sh` — source it from `dogfood/realtime-e2e/`
before `../../build/cerberus run`. It exports the head env
(`ANTHROPIC_BASE_URL` default, `CERBERUS_MIGRATION_DIR`) and the planner row env:

- `CERBERUS_PLANNER_API_KEY=$ANTHROPIC_AUTH_TOKEN` (same GLM coding-plan key)
- `CERBERUS_PLANNER_API_URL=https://open.bigmodel.cn/api/coding/paas/v4/chat/completions`
- `CERBERUS_PLANNER_MODEL=glm-4.5`

Why these values (all measured live, 2026-08-18):

- open-agents' planner (`apps/api/src/services/planner.ts` `callLLM`) sends an
  OpenAI chat-completions body (`messages` + `Authorization: Bearer`) and fetches
  `api_url` VERBATIM — so the URL must be the full OpenAI-compatible path. The
  GLM **coding-plan** endpoint (`/api/coding/paas/v4/...`) is the one this key
  has balance on; the general `/api/paas/v4/...` endpoint returns 1113
  (insufficient balance).
- Latency (UPDATED 2026-08-18, full decompose prompt — system prompt + mission
  text + agent list, 3 runs each): glm-4.5 14.6-46.0s (median 38 — this is
  what the live worker runs actually paid; the earlier "3-5s" was the bare
  1.7KB system prompt), glm-4.5-air 4.3-12.0s (median 10; clean plans,
  `recommendedAgent: "claude"` — no agent-literal copy), glm-4.5-flash ≈27s,
  glm-4.7/4.6 30-40s on the bare prompt alone. **Picked: glm-4.5-air**
  (`CERBERUS_PLANNER_MODEL` in the env script). The `callLLM` 60s cap was
  also raised to 180s upstream (open-agents branch
  `fix/planner-llm-timeout-headroom`, commit b0458e6) as headroom insurance.
- Provider-row tie trap (2026-08-18): every run POSTs a fresh
  `cerberus-planner` row and `resolveAiConfig` orders by `priority ASC,
  created_at ASC` — with the default priority 0 the OLDEST row wins forever
  (7 glm-4.5 rows had piled up; the 2026-08-17 one silently pinned the
  model). Fixed seed-side: the POST now carries `priority: -unix(now)`, so
  each run's row wins; no D1 cleanup needed.
- `glm-5.3[1m]` (the interactive env's sonnet mapping) is a Claude-Code-side
  tag — the raw API rejects it (1214 modelCode 不存在).

## Sibling repo live state (NOT committed — `apps/api/.dev.vars` is tracked but secret)

- `PROVIDER_KEY_KEK=<hex>` appended (SEC-4: `POST /api/admin/ai-providers`
  returns 500 "Provider key encryption not configured" without it; the key is
  then stored `enc:v1:` and decrypts for the planner).
- `API_BASE_URL=http://localhost:8989` (was neutralized/empty since 2026-08-14).
  `UserRoom.alarm()` returns silently when `API_BASE_URL` is empty — missions
  stay `decomposing` forever. It must point at the dev worker itself, never at
  a capture server.

## One-time dev-D1 fixes (persist in `.wrangler` state)

The local D1 predates the current schema baseline; run from `apps/api`:

```
npx wrangler d1 execute open-agents --local --command "ALTER TABLE agents ADD COLUMN steering TEXT"
npx wrangler d1 execute open-agents --local --command "ALTER TABLE cost_records ADD COLUMN request_type TEXT"
npx wrangler d1 execute open-agents --local --file migrations/0002_hitl_questions.sql   # added 2026-08-19: task_question callbacks 500 on the missing multiagent_task_questions table
```

- `agents.steering`: `POST /api/agents` 500s (D1_ERROR: no column named steering).
- `cost_records.request_type`: decompose's cost recording errors after a
  successful plan (non-fatal but noisy, and it happens on EVERY decompose).
- Legacy agent rows (added 2026-08-19, after the completion-callback reopen):
  pre-2026-08-19 runs seeded `{name:"claude", baseCli:"claude"}` rows. Those
  resolve to the ACP adapter (`npx @agentclientprotocol/claude-agent-acp`),
  which never finishes offline, so the planner must not be able to pick them:

  ```
  npx wrangler d1 execute open-agents --local --command "DELETE FROM agents WHERE base_cli = 'claude'"
  ```

  The mission-seed case now seeds `{name:"claude-pty", baseCli:"claude-pty"}`
  (PTY transport = the dogfood shim, which exits after the task prompt and so
  fires the completion callback). Rows with the same base_cli accumulate
  harmlessly; only heterogeneous base_clis are a problem.

## provider_test landmine

The seeded `provider_test` row (openai, `api.openai.com/v1`, created
2026-05-25) is ACTIVE and wins the `created_at ASC` tie in
`resolveAiConfig` (ai-provider-service.ts orders by `priority ASC, created_at
ASC`; the seeded planner row lands at the same priority 0 but later). The
planner then fetches api.openai.com — unreachable from this network — and
hangs until callLLM's 60s timeout, silently falling back to rule-based
decomposition. Fix (data, persists):

```
PUT /api/admin/ai-providers/provider_test  {"is_active":false}
```

Any future re-seed of the dev D1 needs this re-applied. If a live dogfood run
shows missions completing suspiciously fast with rule-shaped single tasks,
check this first.

## Smoke verdict (2026-08-18, wrangler 4.95.0 on :8989)

All 2xx, chain complete: dev/setup 200 (superadmin JWT) → /api/auth/me 200 →
billing/plans 201 → admin/users PUT 200 → ai-providers 201 (KEK path) →
/api/agents 200 → /api/missions 201 `status:"decomposing"`, and after the DO
alarm (~5s) the mission flipped to `running` with ONE LLM-generated task
(`generate`, title 回复done) — the planner call itself succeeded through the
seeded provider row.
