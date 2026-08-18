# Non-workflow gap closure — coverage 100%

Date: 2026-08-18. Session `5d0df93b-535f-433b-9edb-bc041ea8e03e`, 8m12s,
live environment identical to `2026-08-18-workflow-coverage-env.md` (wrangler
:8989, bridge-pty-1 + bridge-pty-2, glm-4.5-air planner provider).

| Metric | Before (run 8) | This run |
|---|---|---|
| Coverage | 98.39% (6 gaps) | **100% (gaps=0, reached)** |
| Verdicts | 690 pass / 1 fail | 687 pass / 6 fail / 2 skip |
| Claims | 1 proven / 0 emulated-only / 1 unevidenced | unchanged |

## New case families (both pass, live-verified)

- `ws-realtime-session-family-lifecycle` (scout/session_send_cases.go):
  real session start → `chat:send` (chat:response observed) →
  `session:resize` / `control:takeover` / `session:cancel` (send-side
  credit). Closes 4 web→bridge edges that the payload-less generic steps
  could never reach (all need the payload.deviceId route).
- `ws-realtime-bridge2-restart-pair` (scout/device_restart_cases.go):
  `device:restart` routed at the sacrificial second bridge (the process
  exits — handleDeviceRestart os.Exit(0)), new `process_restart` DSL step
  relaunches it via the session harness (setup NOT re-run; same deviceId),
  and the web connection receives the reconnect `device:online` broadcast.
  Closes the last 2 edges. **The reconnect-broadcast hypothesis held**: the
  relaunched bridge re-registers with the same deviceId and the DO
  broadcasts device:online.

## Harness/executor additions

- `process_restart` step (execute_phases_steps.go): resolves the declared
  role → credential_ref actor, delegates to `Session.RestartActor` via the
  new `ActorRestarter` hook. Nil hook or failed relaunch = step failure.
- `harness.Restart` (session/harness.go): stop-if-running → re-capture →
  restart → re-wait ready_pattern. Setup does not re-run (pairing persists
  in the isolated HOME).
- Sacrificial-role rule: ≥2 real bridge roles required; the alphabetically
  LAST is restarted (every other case routes at the primary `bridge`); the
  case is emitted last in the sequential plan.

## Honest caveats

- The run's network to open.bigmodel.cn dropped mid-run: every Examiner
  judge call failed ("network is unreachable") and verdicts degraded to
  step status (examiner.go:77 fallback). The two new cases are
  deterministic frame assertions, so their passes stand without the judge,
  but LLM-judged drift was NOT measured this run (last measured 2026-08-18
  morning: 0/676).
- 6 fails are all environmental/known, not SUT regressions:
  4× oauth-github/google `unreachable` (the same network drop; these routes
  redirect to external IdPs) and tc-003/tc-004 Scout probe drift (known
  pre-existing item). Findings backflow recorded them (4 findings).
- 4 workflow completion-family vocab edges remain `partial` marks (ws://
  callback bug — known-issues item 6); partials are not gaps.

## open-agents finding (filed)

- `session:cancelled` missing from the DO bridge→web whitelist — added to
  known-issues item 1 (bridge→web drops).
