#!/usr/bin/env bash
# Bring up the open-agents dev server (sibling repo) and run cerberus's live
# //go:build integration suite against it, then tear the server down.
#
# Reuses an already-running server on :8989 (and does NOT tear it down in that
# case — never kills a process we did not start). wrangler requires Node >=22;
# we select it via fnm.
#
# Usage:
#   make integration-openagents                       # whole integration suite
#   make integration-openagents TEST=TestVocabularyDriven   # one test (go -run regex)

OA_DIR="${OPENAGENTS_DIR:-../open-agents/apps/api}"
PORT=8989
PID=""

cleanup() {
  if [ -n "$PID" ]; then
    echo "→ stopping open-agents dev server (process group $PID)"
    # setsid made the server its own process-group leader (PGID == PID); kill the
    # whole group so npm → wrangler → miniflare all die, not just the npm parent.
    kill -- -"$PID" 2>/dev/null
    sleep 0.5
    kill -9 -- -"$PID" 2>/dev/null
  fi
}
trap cleanup EXIT INT TERM

if ! command -v fnm >/dev/null 2>&1; then
  echo "fnm not found; wrangler needs Node >=22 (install via fnm: 'fnm install 22')" >&2
  exit 1
fi

if curl -s -o /dev/null --max-time 1 "http://localhost:${PORT}/" 2>/dev/null; then
  echo "→ open-agents already reachable on :${PORT} — reusing (will NOT tear down)"
else
  echo "→ starting open-agents dev server (fnm 22 → wrangler :${PORT}) from $OA_DIR"
  log="$(mktemp -t cerberus-oa-XXXXXX.log)"
  # setsid: own process group so cleanup can kill the tree reliably.
  setsid bash -c "cd '$OA_DIR' && eval \"\$(fnm env --shell bash)\" && fnm use 22 >/dev/null 2>&1 && exec npm run dev" >"$log" 2>&1 &
  PID=$!
  for _ in $(seq 1 40); do
    if curl -s -o /dev/null --max-time 1 "http://localhost:${PORT}/" 2>/dev/null; then
      break
    fi
    if ! kill -0 "$PID" 2>/dev/null; then
      echo "→ open-agents server exited before becoming ready; log:" >&2
      cat "$log" >&2
      rm -f "$log"
      exit 1
    fi
    sleep 1
  done
  if ! curl -s -o /dev/null --max-time 2 "http://localhost:${PORT}/" 2>/dev/null; then
    echo "→ open-agents did not come up on :${PORT} within 40s; log:" >&2
    cat "$log" >&2
    rm -f "$log"
    exit 1
  fi
  echo "→ open-agents up (pgid $PID); dev log: $log"
fi

RUN_ARG=()
if [ -n "${TEST:-}" ]; then
  RUN_ARG=(-run "$TEST")
fi
go test -tags=integration -v -timeout=10m ./internal/head/agent/ "${RUN_ARG[@]}"
status=$?
echo "→ integration suite exit: $status"
exit $status
