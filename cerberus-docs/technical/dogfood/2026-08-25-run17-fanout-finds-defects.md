# Run 17 — fan-out case's first live run finds two real orchestration defects

Date: 2026-08-25
Run 17 session: ec8e9cdf-e9f3-4530-9bf4-3e813f099ff1, exit 0 —
**703 pass / 1 fail / 0 recovered, coverage 100%, ~758K/800K**.
The 1 fail is `ws-realtime-wf-mission-fanout` (first live run of the new
multi-device case) — and it did its job: it exposed two open-agents defects.

## Evidence chain (all from live artifacts, no speculation)

D1 ground truth for the fan-out mission `job_1787621959416`
(device_ids correctly `[b1, b2, b3]`, all three devices cliEnabled-complete
in D1, all heartbeating 200 OK every 30s):

```
t0 → device_5de04ec35c5b41c1 (bridge-pty-1)  status=assigned   STUCK
t1 → device_b6a6b5bb39ba4d77 (bridge-pty-3)  status=completed
t2 → device_b6a6b5bb39ba4d77 (bridge-pty-3)  status=completed
mission status=running forever
```

- bridge-pty-1's log has ZERO `Workflow task assign` entries for this job:
  the orchestrator dispatched t0 to b1 and the task_assign was LOST between
  the DO and b1's WS — silently (no ack, no retry, no error). b1 was
  heartbeating fine (HTTP) the whole time, so D1 said "online".
- bridge-pty-3's log shows t1 AND t2 each assigned TWICE (09:40:27.6 and
  09:40:28.2 — two dispatch passes 600ms apart); the second assign dies on
  `git worktree add ... fatal: branch already exists`.
- Minor mystery: round-robin skipped b2 (idx pattern b1→b3→b3 despite equal
  loads and full cliEnabled) — unresolved, secondary to the two above.

## Filed defects

- **#15 task_assign silent loss + mission wedge**: DO sendToBridge drops a
  dispatch when the target bridge's WS is (half-)dead while HTTP heartbeats
  keep D1 last_seen fresh — online-by-D1 diverges from
  connected-by-DO. No delivery ack for task_assign, so the task sits in
  `assigned` until stuck-recovery; the mission never finalizes (job_status
  never emitted).
- **#16 duplicate dispatch race**: two dispatch passes re-assign the same
  task within ~600ms (status write not visible to the second pass), and the
  second worktree creation fails fatally on the existing branch name.

## Cerberus-side observations

- The repair case for a ws_flow failure failed with
  `ws connect: unknown role "web"` — the repair generator loses the
  protocol role registry (framework bug, cerberus-side; filed for fix).
- The examiner judge read the partial evidence correctly (fail,
  correctness 0.85, precise reasoning naming both the missing job_status
  and the single-device concentration).

## Disposition

- `feat/mission-fanout` stays OPEN until #15/#16 are fixed (a correct
  fan-out is also the user-visible orchestration effect we want to demo);
  the branch carries the case + this doc.
- Case hardening queued regardless of SUT fixes: job_status window 120→600,
  and 3× task_completed receives before job_status (per-task completion,
  not first-of-N).
