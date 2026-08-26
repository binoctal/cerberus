# Run 27 — All Green: 710/0, Coverage 100%, Browser Leg 4/4

Date: 2026-08-26
cerberus main (post ee8cadf); open-agents main e2b0a83 (api c3a0071 +
bridge 8766f72); stack: wrangler :8989 + vite preview :5183

## Result

**710 pass / 0 fail / 0 skip / 0 uncertain / 0 recovered, coverage 100%
(gaps: 0), 45m27s, ~858K tokens.**

First fully-green run since the browser leg landed (run 25: 705/9 with the
leg failing on a missing chromium build; run 26: 715/3, cov 99.74%).

## What this run validates

1. **devices-page-populated green** — the card-grid selector retarget
   (`text=dev-device`, 42da4d4) passed; the four ui-vocab assertions
   (missions-conn-status / missions-device-counter / missions-list-renders
   / devices-page-populated) are 4/4 for the first time.
2. **Coverage back to 100%** — the run-26 gap was exactly the failing
   devices assertion; with it green, gaps: 0.
3. **#21 (alarm storm backoff) — no regression, no storm.** Fresh wrangler
   state means no stale no-device missions this run, so the backoff path
   (`alarm retry in Ns (attempt N)`) had no positive trigger — the fix is
   unit-verified; absence of storm is consistent but not a live trigger
   test. A long-lived wrangler dogfood would exercise it.
4. **#20 (re-dispatch guard) — no regression.** The mission-fanout family
   (failed in runs 25/26 on ws_match/auth transients) passed; the
   "Re-dispatch for live task" guard had no trigger this run (no
   re-dispatches occurred), consistent with a healthy run.

## Notes

- Total case count 710 vs run 26's 718: the mission-seed/question legs
  vary per run (planner-dependent); zero-fail is the signal, not the
  denominator.
- Claims: 2 proven / 0 emulated-only / 0 unevidenced / 1 wont-test.
- Process hygiene post-run: zero bridge/chromium/claude-agent-acp residue;
  the only kiro process is the user's desktop instance (gnome-shell
  parent), untouched per policy.

## Ledger state after this run

19 filed items: 18 FIXED, #4 open by design (naming fork). #22 (zombie
bridge after reconnect-budget exhaustion) observed but not filed.
