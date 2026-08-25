# Run 18 — dispatch defects #15/#16 fixed and live-validated; fan-out executes

Date: 2026-08-25
Run 18 session: 0434cba7-1e91-4911-8767-342b27c314cb, exit 0 —
**699 pass / 0 fail / 1 skip / 2 uncertain / 0 recovered, coverage 100%,
~758K/800K**. `ws-realtime-wf-mission-fanout` EXECUTION PASSED first try
(attempts:1) for the first time.

## The three SUT fixes (run 17's findings), all live-validated

1. **Dispatch claim CAS** (`markTaskDispatched`, pending→assigned atomic):
   the fan-out job's assigns show NO 600ms duplicates anymore — the only
   re-assigns are error-driven retries 90s/225s later, which are legitimate.
2. **ISO last_seen** (bridge session route): bridge-pty-2 received t0's
   retry — b2 is back in the candidate rotation (run 17 never dispatched
   anything to it).
3. **Question-timeout session stop** (bridge): no pool leak; the mission
   finalized — job_status arrived, the run-17 wedge is gone.

Dispatch spread for fan-out job_1787637012118: t0→b1 (retry→b1→b2),
t1→b3, t2→b3 — all three devices touched.

## The 2 uncertain verdicts are budget starvation, not evidence

The fan-out verdict degraded with "unable to determine verdict" — the
judge ran out of session budget at ~758K/800K (same failure shape as the
2026-08-19 200K starvation: last verdicts degrade to step status). Budget
bumped 800K→900K; the next run (ACP package validation) doubles as the
confirmation that the fan-out case judges cleanly.

## Disposition

- open-agents `fix/fanout-dispatch-defects` (parent f674ac1 + bridge
  submodule): merge to main after this run.
- cerberus `feat/mission-fanout`: fan-out case + hardening + this doc;
  merge to main (the case is now green at execution level; ledger #15/#16
  → RESOLVED).
