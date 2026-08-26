#!/usr/bin/env bash
# Run 29 launcher — fully detached (setsid) so the run survives the Claude
# session lifecycle (run-28 lesson: a run tied to the session got SIGTERM'd
# by /exit and clipped the judging window). Brings up wrangler :8989, waits
# for health, then sources the dogfood env script and runs cerberus.
set -u
CERBERUS_ROOT="/home/mason/Documents/code_projects/private/cerberus"
OA_API="/home/mason/Documents/code_projects/private/open-agents/apps/api"
LOG_DIR="$CERBERUS_ROOT/dogfood/realtime-e2e/.cerberus/runtime/logs"

echo "[run29] $(date -Is) starting wrangler :8989" >> "$LOG_DIR/run29-launcher.log"
cd "$OA_API" || exit 1
eval "$(fnm env)" && fnm use 22 >/dev/null 2>&1
(nohup npm run dev > "$LOG_DIR/run29-wrangler.log" 2>&1 &)

# Wait for the API to answer (up to 120s).
for i in $(seq 1 60); do
  code=$(curl -s -o /dev/null -w "%{http_code}" --max-time 2 http://localhost:8989/api/health 2>/dev/null)
  if [ "$code" != "000" ] && [ -n "$code" ]; then
    echo "[run29] $(date -Is) wrangler up (health $code)" >> "$LOG_DIR/run29-launcher.log"
    break
  fi
  sleep 2
done

cd "$CERBERUS_ROOT/dogfood/realtime-e2e" || exit 1
source ../../scripts/dogfood-realtime-e2e-env.sh >> "$LOG_DIR/run29-launcher.log" 2>&1
echo "[run29] $(date -Is) launching cerberus run" >> "$LOG_DIR/run29-launcher.log"
../../build/cerberus run > "$LOG_DIR/run29-stdout.log" 2>&1
echo "[run29] $(date -Is) cerberus run exited rc=$?" >> "$LOG_DIR/run29-launcher.log"
