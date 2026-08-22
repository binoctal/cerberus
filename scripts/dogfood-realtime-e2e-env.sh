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
# endpoint (full path), NOT the anthropic base URL above.
#
# Model choice (measured 2026-08-18 on the FULL decompose prompt — system
# prompt + mission text + agent list; the earlier 3-5s figure was the bare
# 1.7KB system prompt and does not hold):
#   glm-4.5      14.6-46.0s (median 38) — flirts with callLLM's 60s cap
#   glm-4.5-air   4.3-12.0s (median 10) — clean plans, recommendedAgent OK
#   glm-4.5-flash ~27s — no advantage over air
#   glm-4.7/4.6  30-40s on the bare prompt alone
# glm-5.3[1m] is a Claude-Code-side tag the raw API rejects (1214).
export CERBERUS_PLANNER_API_KEY="$ANTHROPIC_AUTH_TOKEN"
export CERBERUS_PLANNER_API_URL="https://open.bigmodel.cn/api/coding/paas/v4/chat/completions"
export CERBERUS_PLANNER_MODEL="glm-4.5-air"

# Bridge completion-callback secret (open-agents fix/workflow-callback-url,
# 2026-08-19): the bridge POSTs task results to
# /api/missions/internal/orchestrator/event, which the /internal/* middleware
# guards with X-Internal-Secret. The bridge reads it from this env var
# (inherited by the dogfood-launched bridge processes). Parsed from the
# sibling .dev.vars — that file is a secret, NEVER stage it or echo the value.
_DEVVARS="${REPO_ROOT}/../open-agents/apps/api/.dev.vars"
_BRIDGE_SECRET="$(grep -E '^INTERNAL_SECRET=' "${_DEVVARS}" 2>/dev/null | head -1 | cut -d= -f2-)"
if [ -n "${_BRIDGE_SECRET}" ]; then
  export OPEN_AGENTS_INTERNAL_SECRET="${_BRIDGE_SECRET}"
else
  echo "WARNING: INTERNAL_SECRET not found in ${_DEVVARS} — bridge completion callbacks will be 403"
fi

# Rebuild the sibling bridge binary the actors exec: project.yaml starts
# ./build/open-agents-bridge verbatim, so a stale binary silently runs
# pre-fix code (2026-08-22: three dogfood runs "verified" fixes that were
# never in the binary). Cheap no-op when already current.
_BRIDGE_DIR="${REPO_ROOT}/../open-agents/bridge"
if [ -d "${_BRIDGE_DIR}" ]; then
  echo "building sibling bridge binary (${_BRIDGE_DIR})"
  (cd "${_BRIDGE_DIR}" && go build -o build/open-agents-bridge ./cmd/open-agents-bridge) \
    || echo "WARNING: bridge build failed - dogfood will run the existing binary"
fi
