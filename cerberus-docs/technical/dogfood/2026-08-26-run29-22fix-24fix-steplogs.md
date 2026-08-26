# Run 29 — 724/4, Coverage 99.5%, Step Logging Live, #22/#24 Fixes In

Date: 2026-08-26
cerberus main (7fd1f37, per-step logging); open-agents main 32c4175
(api 35ae557 pointer → bridge def0acd #22 slow-retry; web 32c4175 #24
settingsClient); stack: wrangler :8989 + vite preview :5183.

Launched fully detached (setsid wrapper `scripts/run29-launch.sh`) per the
run-28 lesson — the run survived independent of the session lifecycle,
exited rc=0 on its own after judging.

## Result

**724 pass / 4 fail / 0 skip / 0 uncertain, coverage 99.5% (gaps: 3),
54m49s, ~892K tokens, 728 verdicts (full judging, no SIGTERM clip).**

## What this run validates

1. **Per-step info logging (cerberus 7fd1f37) — first live run.** 871
   `case step` lines; every ws_flow/http case's steps and outcomes are now
   diagnosable from the run log alone (verified while the run was live).
2. **#23 (mission-fanout flake) — did NOT recur, 4th clean run out of 5.**
   The case executed a REAL 4.5-minute fan-out this time (step logs show
   steps 8–10 as genuine 24s/35s `workflow:task_progress` receives, then
   `mission-fanout pass`), not the run-28 0.13s instant-fail. Still no
   root cause pinned — the failure has not recurred since the step logging
   landed, so the resolution-path hypothesis stays untested. Next
   recurrence will be caught by the `case step` lines.
3. **#22 (zombie bridge) — no regression; slow-retry path not triggered**
   (no long gateway flap this run — bridges stayed connected). The fix is
   unit-verified; absence of zombie is consistent but not a live trigger
   test. Same status the #21 backoff had after run 27.
4. **#24 (settings storm) — fix live in the served bundle (fresh `vite
   build`), no functional regression.** The 4 failures below are NOT
   settings-related.

## The 4 failures — one shape, all browser-leg

`ui-vocab-dashboard-home-quick-actions` ×2 and `ui-vocab-prompt-lab-title`
×2 (case + fallback each). Step logs pin the shape precisely:

- step 1 `browser_goto /dashboard` (or `/dashboard/prompt-lab`) **passes in
  ~50 ms** — navigation is NOT hanging at goto;
- step 2 `browser_expect text=…` fails at the **30.0s hard cap**
  (`browser error … (30.002s)`).

So the page's load event resolves but the expected text never renders
within 30s — the same intermittent family from the #24 investigation
("/dashboard SW navigation hang"), now localized by step logging to the
render/expect phase, NOT the goto. It recurred WITH the settingsClient fix
live, which further weakens (does not eliminate) the request-storm
contributing-factor theory. Candidate next steps if it recurs often enough
to justify cost: CDP network-waterfall capture during a live-caught hang;
or a browser_expect retry (only if a repro is catchable — an unverified
retry would be a guess, same discipline as #24's investigation note).

Coverage 99.5% (3 gaps) — presumably the two failed ui-vocab assertions'
edges plus one knock-on; consistent with the failures.

## Environment notes

- The two one-off preconditions were already persisted in `.wrangler` D1
  state (provider_test inactive, steering column) — fresh wrangler boot
  needed no manual fixes.
- Launcher + logs: `dogfood/realtime-e2e/.cerberus/runtime/logs/run29-*`.
