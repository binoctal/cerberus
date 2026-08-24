# Run 11 — triple-fix validation (alarm queue, probe-scope credit, vocab veto)

Date: 2026-08-24 (follows the #14 fix-run doc, same day)
Cerberus session: 95e3ed71-5753-40d5-9095-a1f5c530a804, exit 0
Open-agents: c038f39 (alarm queue, merged to main)
Cerberus: a555138 (examiner membership note + guidance; scout vocab-veto)

## Headline: 698 pass / 0 fail — first zero-fail run since 2026-08-21

698 verdicts, 0 uncertain, 0 recovered; coverage 99.74% (2 gaps — the
standing OAuth-outbound pair); ~741K tokens; 23m53s; exit 0.

## Fix 1 — DO alarm single-slot overwrite (open-agents c038f39)

Live evidence in the wrangler DO logs (new `queued=N` field only the
queue code emits):

```
Alarm scheduled: type=decompose       delay=1ms      queued=1
Alarm scheduled: type=stuckRecovery   delay=300000ms queued=1
Alarm scheduled: type=decompose       delay=1ms      queued=2
Alarm scheduled: type=stuckRecovery   delay=300000ms queued=2
Alarm scheduled: type=retryTask       delay=5000ms   queued=3   (×3)
Alarm scheduled: type=retryTask       delay=15000ms  queued=4
Alarm scheduled: type=retryTask       delay=15000ms  queued=3
Alarm scheduled: type=retryTask       delay=45000ms  queued=2
```

Concurrent decompose/stuckRecovery/retryTask alarms coexist and drain
in order — under the old single-slot code every one of these overwrote
the pending alarm. Fail-mission `job_1787551357276` ladder exhausted
(t0+t1 `failed, retry_count=5, codex`), mission → `partial`; no task
stuck pending. Unit ladder: alarm-queue.test.ts 6/6; full apps/api
suite 1771/1771.

## Fix 2 — probe-scope under-credit (cerberus a555138, examiner)

`ws-realtime-wf-task-assign`: **pass / correctness 0.88** (run 10:
fail / 0.4). The send-only membership dimensions now render the
"not directly probed" note and the dimension guidance states empty
recipients ≠ delivery failure + response frames are valid proof.

## Fix 3 — tc-00x drift family (cerberus a555138, scout)

Zero endpoint_drift fails; tc-001..tc-00N all pass. The planner did
not invent /healthz this run (whim variance), so the vocab veto was
not exercised live — it stands as the structural backstop: when a
service carries a vocab route surface, analyze-inferred model entries
no longer immunize invented paths against the downgrade (unit-proven
in TestDowngradeUnmodeledHTTPProbes_VocabVetoesInferredModelEntries).

## Residual

- The 2 coverage gaps are the long-standing OAuth outbound transport
  findings (wont-test tier), unchanged.
- mission-seed verdict: pass / correctness 0.95.
