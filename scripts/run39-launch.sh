#!/usr/bin/env bash
# Run 39 launcher — re-validates the run37 follow-up fixes that run38 could
# not (run38 was invalidated twice: open-agents d6e5390 had removed the
# /api/auth/dev/setup the dogfood admin-actor still used — 285 admin cases
# failed before reaching the SUT — and the wrangler on 8989 was killed 4.5
# minutes into execution, almost certainly a parallel session's env ritual).
#
# New guards vs run38-launch.sh:
#   - open-agents HEAD is pinned: the build must contain REQUIRED_OA_BASE
#     (git merge-base --is-ancestor), and the resolved HEAD is logged so a
#     post-mortem can prove which commits the wrangler binary carried.
#     run38 launched six minutes before 9a44ca4 landed and silently ran
#     without it.
#   - a wrangler watchdog shadows the whole cerberus run: if the pid-verified
#     wrangler dies mid-run (parallel session loop-kill, run34/run38 pattern),
#     the moment is logged instead of silently corrupting the run.
# Parallel sessions cannot be prevented from killing 8989 — launch only when
# no other session is mid env-ritual.
#
# Full env ritual (as run38): loop-kill, state rotate, migrations against the
# DEAD wrangler, pid-verified fresh wrangler. Fully detached (setsid) so the
# run survives the Claude session lifecycle.
set -u
RUN=run39
CERBERUS_ROOT="/home/mason/Documents/code_projects/private/cerberus"
OA_ROOT="/home/mason/Documents/code_projects/private/open-agents"
OA_API="$OA_ROOT/apps/api"
# 5e9c3b1 = dev/setup accepts role:'admin' — the guarded admin seeding the
# dogfood admin-actor now depends on (see project.yaml).
REQUIRED_OA_BASE=5e9c3b1
LOG_DIR="$CERBERUS_ROOT/dogfood/realtime-e2e/.cerberus/runtime/logs"
LOG="$LOG_DIR/$RUN-launcher.log"
log(){ echo "[$RUN] $(date -Is) $*" >> "$LOG"; }

log "=== env ritual start ==="

# 0. Pin open-agents HEAD: log it, and refuse to build older than required.
if ! git -C "$OA_ROOT" merge-base --is-ancestor "$REQUIRED_OA_BASE" HEAD 2>/dev/null; then
  log "FATAL: open-agents HEAD lacks $REQUIRED_OA_BASE (need >= for role:'admin' seeding)"
  exit 1
fi
log "open-agents HEAD $(git -C "$OA_ROOT" rev-parse --short HEAD): $(git -C "$OA_ROOT" log -1 --pretty=%s)"

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
  mv .wrangler/state ".wrangler/state.bak-$RUN-$(date +%s)"
  log "state rotated"
fi
eval "$(fnm env)" && fnm use 22 >/dev/null 2>&1
if ! npx wrangler d1 migrations apply DB --local >> "$LOG" 2>&1; then
  log "FATAL: migrations failed"; exit 1
fi
log "migrations applied"

# 4. Fresh wrangler; verify it is NEW by pid, not just by health.
PRE_PIDS=$(pgrep -f "wrangler dev --port 8989" 2>/dev/null || true)
(nohup npm run dev > "$LOG_DIR/$RUN-wrangler.log" 2>&1 &)
FRESH_OK=0
WRANGLER_PID=""
for i in $(seq 1 90); do
  code=$(curl -s -o /dev/null -w "%{http_code}" --max-time 2 http://localhost:8989/api/health 2>/dev/null)
  NOW_PIDS=$(pgrep -f "wrangler dev --port 8989" 2>/dev/null || true)
  if [ "$code" != "000" ] && [ -n "$code" ]; then
    for p in $NOW_PIDS; do
      if ! echo "$PRE_PIDS" | grep -qw "$p"; then FRESH_OK=1; WRANGLER_PID="$p"; break; fi
    done
    [ "$FRESH_OK" = "1" ] && break
  fi
  sleep 2
done
if [ "$FRESH_OK" != "1" ]; then
  log "FATAL: no fresh wrangler came up (pre=$PRE_PIDS now=$NOW_PIDS health=$code)"
  exit 1
fi
log "fresh wrangler up (health $code, pid $WRANGLER_PID verified)"

# 5. Watchdog: flag the moment the verified wrangler dies mid-run. The cerberus
# run itself keeps going (degrades to unreachable verdicts), but the harvest
# knows exactly when the run stopped being trustworthy.
( while kill -0 "$WRANGLER_PID" 2>/dev/null; do sleep 15; done
  log "WRANGLER LOST: pid $WRANGLER_PID gone at $(date -Is) — verdicts after this point are suspect (parallel-session kill?)" ) &

# 6. Dogfood run.
cd "$CERBERUS_ROOT/dogfood/realtime-e2e" || exit 1
source ../../scripts/dogfood-realtime-e2e-env.sh >> "$LOG" 2>&1
log "launching cerberus run"
../../build/cerberus run > "$LOG_DIR/$RUN-stdout.log" 2>&1
log "cerberus run exited rc=$?"
