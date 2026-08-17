# Source this before the realtime-e2e dogfood run (workflow-orchestration
# coverage, 2026-08-18 plan Task 5). It wires the head LLM env and the planner
# provider env consumed by missionSeedCases (internal/head/scout/mission_seed_cases.go).
#
# Usage:
#   cd dogfood/realtime-e2e
#   source ../../scripts/dogfood-realtime-e2e-env.sh
#   ../../build/cerberus run
#
# Prerequisites OUTSIDE this script (live env, not code — see
# cerberus-docs/technical/dogfood/2026-08-18-workflow-coverage-env.md):
#   1. ../open-agents/apps/api/.dev.vars (gitignored-but-tracked sibling file,
#      NEVER stage it) must contain:
#        PROVIDER_KEY_KEK=<openssl rand -hex 32>   # POST /api/admin/ai-providers 500s without it
#        API_BASE_URL=http://localhost:8989        # empty disables DO alarm callbacks: missions stay "decomposing" forever
#   2. wrangler dev running on :8989 from ../open-agents/apps/api (npm run dev).
#   3. One-time dev-D1 fixes (persist in .wrangler state):
#        wrangler d1 execute open-agents --local --command "ALTER TABLE agents ADD COLUMN steering TEXT"
#        wrangler d1 execute open-agents --local --command "ALTER TABLE cost_records ADD COLUMN request_type TEXT"
#        PUT /api/admin/ai-providers/provider_test {"is_active":false}
#      (the seeded provider_test row points at api.openai.com, which is
#      unreachable from this network; it wins the created_at-ASC tie in
#      resolveAiConfig and silently starves the planner — see the doc.)

# Heads: same GLM coding-plan gateway the interactive environment uses.
: "${ANTHROPIC_AUTH_TOKEN:?ANTHROPIC_AUTH_TOKEN must be set (GLM coding-plan key)}"
export ANTHROPIC_BASE_URL="${ANTHROPIC_BASE_URL:-https://open.bigmodel.cn/api/anthropic}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export CERBERUS_MIGRATION_DIR="${CERBERUS_MIGRATION_DIR:-$REPO_ROOT/migrations}"

# Planner provider row (missionSeedCases step 3). open-agents' planner
# (apps/api/src/services/planner.ts callLLM) speaks the OpenAI chat-completions
# shape and fetches api_url VERBATIM, so this is the GLM coding-plan OpenAI
# endpoint (full path), NOT the anthropic base URL above. glm-4.5 measured
# 3-5s on the real 1.7KB planner prompt (glm-4.7/4.6 take 30-40s and flirt
# with callLLM's 60s AbortSignal cap); glm-5.3[1m] is a Claude-Code-side tag
# the raw API rejects.
export CERBERUS_PLANNER_API_KEY="$ANTHROPIC_AUTH_TOKEN"
export CERBERUS_PLANNER_API_URL="https://open.bigmodel.cn/api/coding/paas/v4/chat/completions"
export CERBERUS_PLANNER_MODEL="glm-4.5"
