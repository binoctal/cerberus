# tc-004 closure — process_bound role flag (2026-08-21)

Live verification run for the `process_bound` protocol-role flag
(cerberus `fix/scout-process-bound-role-connect`, commit 98c8625).

## Root cause (systematic-debugging)

Every run since 2026-08-18 carried exactly 1 fail: the LLM scout's
exploration case `tc-004` (web↔bridge relay ws_flow) connected as the
`bridge` role. The judge reported it as a "real SUT error" — it is not:
`ws auth: no token for actor "bridge-pty-1"` is cerberus's own
`injectAuth` error (`internal/head/agent/websocket.go`), because
`bridge-pty-1` is a real-process actor with `credentials: {}`. The
constraint "deterministic generators never connect as these roles"
existed only as a YAML comment in the dogfood protocol; nothing filtered
LLM exploration cases against it.

## Fix

- `ProtocolRole.process_bound` (schema): machine-readable "the executor
  must never dial as this role; a real process owns the connection".
- `filterProcessBoundConnects` (scout augmentPlan): drops LLM ws_flow
  cases whose steps `ws_connect` as a process-bound role — same idiom as
  `filterWSEndpointDrift` / `http_only`.
- Dogfood protocol: `bridge` / `bridge2` marked `process_bound: true`.

## Run results (session 4f91e5c9)

- **691 pass / 0 fail / 0 skip / 2 uncertain** — first zero-fail run.
- Coverage **1.0** (gaps 0, reached true). Tokens ~679K, 23m10s.
- Plan check: 695 cases, connect roles = {web} only, zero bridge-role
  connects (this run's scout authored tc-007/tc-008 HTTP GETs instead;
  the filter's deterministic behavior is unit-covered).
- Real actors: bridge-pty-1, bridge-pty-2. Claims: 1 proven /
  0 emulated-only / 1 unevidenced (unchanged).
- The 2 uncertain are honest low-confidence degradations, not
  regressions: `http-route-realtime-post-api-admin-content` (route
  reachable, 500 error body, judge 0.60) and
  `ws-realtime-wf-workflow-pause` (send-side credit, judge 0.50).

## Environment note

wrangler dev needs Node ≥22; the shell default was v20 — start it with
`PATH=$HOME/.local/share/fnm/node-versions/v22.22.3/installation/bin:$PATH npm run dev`
from `../open-agents/apps/api`.
