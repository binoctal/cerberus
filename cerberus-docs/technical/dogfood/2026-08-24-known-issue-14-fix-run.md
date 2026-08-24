# Known Issue #14 Fix Run — stale progress-0 resurrection of retried tasks

Date: 2026-08-24 (scheduled autonomous session, started 2026-08-24 ~09:00 CST)
Open-agents commits: 7fcf9cd (fix) merged to main in 12088cb (no bridge
submodule change — fix is apps/api only)
Cerberus session (run 9): effaf13c-aa9a-413c-9004-937a6f8f09c2

## Root cause (final — supersedes the suspects in the 2026-08-23 entry)

The 2026-08-23 suspects (in-memory slot leak keyed by pre-switch agent,
getReadyTasks filtering) were both wrong. Actual chain, confirmed from D1
+ bridge tee logs + code:

1. `launchTaskSession` emits `task_started` + `task_progress{progress:0}`
   immediately after `CreateWithIDAndSize` returns — including when the
   session dies in the same millisecond (ACP init fail → PTY stub exit-1).
2. Those start frames travel bridge→WS→DO→fire-and-forget-fetch, while the
   `task_error` callback travels bridge→direct HTTP with 1s/2s/4s retry
   backoff. Two unordered transports.
3. Run 8 (2026-08-23): error handler won the race — wrote `pending`,
   switched codex→claude, scheduled the 15s retryTask alarm (200 at
   23:02:58.415). The stale progress-0 then landed and
   `handleTaskProgress(progress=0)` unconditionally set status back to
   `running`.
4. `getReadyTasks` selects only `status='pending'` → the 23:03:13 alarm
   found zero ready tasks → the ready-loop body never executed (hence zero
   log lines — the "silence" was the smoking gun misread as a scheduling
   bug) → task stuck forever.
5. D1 proof: t0 `status=running, retry_count=2, progress=0`, `updated_at`
   in the same second as the error callback, `timeout_at` still equal to
   attempt 2's dispatch + 30min (no attempt-3 dispatch ever reset it).

## Fix

`handleTaskProgress(progress=0)` reads the task and only advances it to
`running` when its status is `assigned` or `running`; stale frames are
dropped with a log line (raw progress frame still forwarded for UI).
Tests: `apps/api/src/test/services/orchestrator.test.ts` —
pending/completed dropped, assigned/running advance.

Ladder results:
- apps/api vitest: 1765/1765 (the one pre-existing contract test
  "progress=0 → running" updated to seed an `assigned` task — intentional
  behavior change).
- `make integration-openagents TEST=TestRealBridge_M1_Orchestration`: PASS
  (10.9s, exit 0).

## Dogfood run 9 (fix live, planner picked codex — the #14-triggering whim)

- 695 pass / 1 fail / 1 uncertain / 1 recovered; 699 verdicts; coverage
  99.74% (2 gaps); 722K tokens; 26m02s; exit 0.
- The 1 fail is tc-003 (`/health` smoke negative-probe judge noise, known
  family) — unrelated to #14.
- mission-seed `ws-realtime-wf-mission-seed`: PASS (correctness 0.82).
- D1: fail-mission `job_1787536695241` t0+t1 both reached
  `status=failed, retry_count=4, assigned_agent=codex` — the retry ladder
  ran to exhaustion and `workflow:task_failed` fired, instead of the
  run-8 silent loss at retry 3. Completion mission
  `job_1787536626416` t0 completed via claude-pty.
- The guard itself did not log this run (the race is timing-dependent and
  error consistently won); the point is the ladder now converges either
  way.

Pre-run cleanup: run-8's two stuck missions (`job_1787414294133`,
`job_1787497234966`) marked failed in dev D1 so stuckRecovery would not
retry dead missions during the run.

## Also closed / noticed

- The #14 side note (periodic stuckRecovery alarms 400 on empty payload)
  was already fixed by 0c62e11 (2026-08-19): `scheduleAlarm` stamps
  `userId` into every alarm payload. Marked RESOLVED in the known-issues
  doc.
- NEW adjacent defect (unfixed, documented): the user-room DO keeps a
  single alarm slot (`__alarm_type`/`__alarm_payload` + one `setAlarm`),
  so concurrent alarm schedules from the same user's orchestrator
  overwrite each other — last writer wins. Did not bite in runs 8-9
  (serial timing) but a retryTask scheduled while a stuckRecovery is
  pending would silently cancel it.

## Run 10 (stability confirmation)

Cerberus session dbf87f5a-2aa0-4b45-be22-11c044a126dc, exit 0.

- 697 pass / 2 fail / 0 uncertain / 0 recovered; 699 verdicts; coverage
  99.74% (2 gaps, same as run 9); ~709K tokens; 24m34s.
- **The #14 stability point holds:** fail-mission `job_1787544598534`
  t0+t1 both reached `status=failed, retry_count=4, assigned_agent=codex`,
  mission → `partial` — the retry ladder again ran to exhaustion and
  `workflow:task_failed` fired (mission-seed PASS). Completion mission
  `job_1787544529518` t0 completed via claude-pty. Two consecutive runs
  (9: codex pick, 10: codex pick again) converge; the run-8 silent-loss
  path is gone.
- The 2 fails are both known judge-noise families, not SUT defects:
  - `tc-001` (/healthz smoke negative-probe, endpoint_drift) — same
    family as run 9's tc-003.
  - `ws-realtime-wf-task-assign` (correctness 0.4, critique) — judge
    read the routing probe's `recipients=[]` on task_assign/answer/
    guidance as "delivery to bridge unconfirmed", but the evidence
    itself contains the bridge's round-trip frames (task_started,
    task_progress, task_output for t-assign-seed with matching
    deviceId), which is direct proof of receipt. Probe-scope
    under-credit; the case passed run 9 with the same evidence shape.
- Pre-run cleanup: `job_1787538326005` (created after run 9 by an
  integration verification, t3 pending) marked failed in dev D1; its
  t0-t2 were subsequently drained to retry_count=4/failed by
  stuckRecovery on the worker restart, confirming the post-0c62e11
  stuckRecovery path works on restart.
