#!/usr/bin/env bash
# Run 38 launcher — validates the run37 follow-up fixes:
#   cerberus  e29eff8  browser re-login after SPA routing settles + expect rescue
#   cerberus  94bbc55  quota-fallback verdicts re-judged after the provider window resets
#   open-agents 1bf2920 white-label INSERT arity + D1 UNIQUE->409 / FK->422
#   open-agents 9a44ca4 dev/setup accepts a plan (dogfood seeds pro)
#   cerberus  ce29c3a  plan: pro + /api/sessions/admin role-map prefix
#   cerberus  3754abe  min_query (ai-comparison/compare, blacklist/check)
#
# Unlike run33-37 launchers this one runs the FULL env ritual first:
#   - loop-kill stale wrangler/workerd/vite trees under open-agents (a
#     run35-era wrangler survived run37's launcher: it answered the health
#     check instantly, so the fresh `npm run dev` lost the port silently —
#     the health check cannot tell old from new; here the fresh wrangler is
#     verified by pid, not by health alone)
#   - rotate .wrangler/state, apply migrations against the DEAD wrangler
#     (run31 lesson: never migrate a live dogfood wrangler)
# Fully detached (setsid) so the run survives the Claude session lifecycle.
set -u
CERBERUS_ROOT="/home/mason/Documents/code_projects/private/cerberus"
OA_ROOT="/home/mason/Documents/code_projects/private/open-agents"
OA_API="$OA_ROOT/apps/api"
LOG_DIR="$CERBERUS_ROOT/dogfood/realtime-e2e/.cerberus/runtime/logs"
LOG="$LOG_DIR/run38-launcher.log"
log(){ echo "[run38] $(date -Is) $*" >> "$LOG"; }

log "=== env ritual start ==="

# 1. Loop-kill stale trees (they respawn via npm parents; loop until gone).
for i in $(seq 1 10); do
  PIDS=$(pgrep -f "wrangler dev --port 8989|workerd|vite preview --port 5183|npm run dev" 2>/dev/null || true)
  if [ -z "$PIDS" ]; then break; fi
  echo "$PIDS" | xargs -r kill 2>/dev/null
  sleep 2
done
PIDS=$(pgrep -f "wrangler dev --port 8989|workerd|vite preview --port 5183|npm run dev" 2>/dev/null || true)
if [ -n "$PIDS" ]; then
  log "escalating kill -9: $PIDS"
  echo "$PIDS" | xargs -r kill -9 2>/dev/null || true
  sleep 2
fi
log "stale trees killed"

# 2. Rotate D1 state; 3. migrations against the dead wrangler.
cd "$OA_API" || exit 1
if [ -d .wrangler/state ]; then
  mv .wrangler/state ".wrangler/state.bak-run38-$(date +%s)"
  log "state rotated"
fi
eval "$(fnm env)" && fnm use 22 >/dev/null 2>&1
if ! npx wrangler d1 migrations apply DB --local >> "$LOG" 2>&1; then
  log "FATAL: migrations failed"; exit 1
fi
log "migrations applied"

# 4. Fresh wrangler; verify it is NEW by pid, not just by health.
PRE_PIDS=$(pgrep -f "wrangler dev --port 8989" 2>/dev/null || true)
(nohup npm run dev > "$LOG_DIR/run38-wrangler.log" 2>&1 &)
FRESH_OK=0
for i in $(seq 1 90); do
  code=$(curl -s -o /dev/null -w "%{http_code}" --max-time 2 http://localhost:8989/api/health 2>/dev/null)
  NOW_PIDS=$(pgrep -f "wrangler dev --port 8989" 2>/dev/null || true)
  if [ "$code" != "000" ] && [ -n "$code" ]; then
    for p in $NOW_PIDS; do
      if ! echo "$PRE_PIDS" | grep -qw "$p"; then FRESH_OK=1; break; fi
    done
    [ "$FRESH_OK" = "1" ] && break
  fi
  sleep 2
done
if [ "$FRESH_OK" != "1" ]; then
  log "FATAL: no fresh wrangler came up (pre=$PRE_PIDS now=$NOW_PIDS health=$code)"
  exit 1
fi
log "fresh wrangler up (health $code, pid verified)"

# 5. Dogfood run.
cd "$CERBERUS_ROOT/dogfood/realtime-e2e" || exit 1
source ../../scripts/dogfood-realtime-e2e-env.sh >> "$LOG" 2>&1
log "launching cerberus run"
../../build/cerberus run > "$LOG_DIR/run38-stdout.log" 2>&1
log "cerberus run exited rc=$?"
