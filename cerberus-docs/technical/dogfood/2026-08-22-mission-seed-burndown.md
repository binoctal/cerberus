# Mission-seed burndown — four live runs, five defects (2026-08-22)

Follow-up to the 2026-08-21 whitelist-alignment run. Verification of the
day's cerberus fixes via fresh dogfood runs instead surfaced a stack of
mission-orchestration and harness defects. All are fixed and unit/integration
verified; the remaining mission-seed dogfood failure is an environment-state
issue (see Open).

## Runs

| # | session | verdicts | fail |
|---|---------|----------|------|
| 1 | 0b31fbbd | 695 pass / 1 fail / 1 uncertain / 1 recovered | mission-seed (task_progress await) |
| 2 | 2d7993e4 | 694 pass / 1 fail / 2 uncertain | mission-seed (task_question arrived) |
| 3 | 1a8eb805 | 696 pass / 1 fail / 1 recovered | mission-seed (task_question arrived) |
| 4 | e32c9d59 | 696 pass / 1 fail / 1 recovered | mission-seed (task_completed await) |

Runs 1–3 ran a **stale bridge binary** (mtime Aug 21 08:56, predating every
bridge fix — project.yaml execs `./build/open-agents-bridge` verbatim and
nothing rebuilt it). The env script now rebuilds it (`fix(dogfood)` commit;
the discovery itself is the meta-lesson below).

## Defects found and fixed

**open-agents bridge** (all merged to bridge main, submodule bumped):

1. `90febc3` — task queue was a black hole: pool-full `Enqueue` never
   drained (`DequeueNext` had no caller), and `QueueItem` lacked `JobID` so a
   drained task would also have lost its completion callback. Manager now
   fires a capacity callback after every session close; `drainTaskQueue`
   starts queued tasks FIFO through the shared `launchTaskSession` lifecycle.
2. `4bc5e1e` — `[QUESTION]` detection (74734d7's line-prefix fix) had a
   chunk-boundary hole: a PTY read splitting the echoed instruction exactly
   before `Example: [QUESTION]` hands the detector a chunk that *starts*
   with the marker without being at a line start (caught live by the
   mission-seed ExpectAbsent probe — the probe design paid off on day one).
   Detection now tracks per-session line-boundary state across chunks.

**cerberus** (merged to main):

3. `bf7b56a` — mission-seed observer connected *after* mission create; a
   fast planner can dispatch before the connect lands. Connect now precedes
   the create. (This is what actually fixed the task_progress await in
   runs 3–4.)
4. `bf7b56a` — the case awaited `workflow:task_question` authored against
   known-issue #9's spurious PTY-echo emission; it is now an `ExpectAbsent`
   probe (absence confirmed on timeout), a live regression guard for the
   bridge fix.
5. repair-loop cases inherited nothing: an emission omitting `service`
   detached the replacement from its protocol roles (`ws connect: unknown
   role "web"`, empty target — runs 2–3). Repair cases now inherit
   Service/Target/Method from the replaced case.

## Verified live (run 4, all fixes actually in the binary)

- task_started + task_progress arrive (connect-ordering + queue drain);
- ExpectAbsent task_question probe passes (marker fixes);
- claims line `1 proven / 0 emulated-only / 0 unevidenced / 1 wont-test`;
- 699 verdicts persisted, session `completed`, no tc-003/tc-004 findings.

## Open

- ~~mission-seed completion await~~ **RESOLVED 2026-08-23** (run 5, session
  9346fb3a: mission-seed **PASS** in a full dogfood run — first time). Sixth
  and final defect: `readMatching`'s single-match path drops non-matching
  frames (deliberate loss semantics), so the ExpectAbsent probe's 60s window
  silently consumed the completion frames flowing on the connection — run 4's
  task_completed await starved 600s with seen=0 while the frames had arrived
  during the probe (the "seen=12 then seen=0" step evidence was the smoking
  gun). Fixed by `readMatchingPassive` (cerberus
  fix/absent-probe-passive merge): an absence probe records and REQUEUES the
  frames it observes — an observation, not a consumption.
- Findings after run 5: only 2 open remain (OAuth-callback transport errors —
  outbound network limitation). The mission-seed family (8 signatures),
  repair-* and tc-00x entries are resolved: every root cause is fixed and
  live-verified. tc-005 (planner-authored "root page loads" probe against the
  API-only worker) is case-authoring noise; the recurring tc-00x legacy-probe
  family is worth a planner-prompt guard someday, not a SUT defect.
- Run 5 hit the GLM 5-hour rate limit during examination (judge degraded to
  step-status verdicts by design; outcome unaffected — 697 pass, coverage
  99.74%, the best yet).

## Run 5 (final)

| session | verdicts | notes |
|---------|----------|-------|
| 9346fb3a | **697 pass / 1 fail / 1 skip / 1 recovered** | mission-seed PASS; only fail = tc-005 probe noise; coverage 99.74% (2 gaps) |

## Meta-lesson

Three runs "verified" fixes that were never in the executed binary. When a
harness execs a prebuilt artifact, either rebuild it at launch or assert its
freshness — silence is indistinguishable from success.

## Epilogue — runs 6–7 (2026-08-23)

| # | session | verdicts | mission-seed | dominant fail |
|---|---------|----------|--------------|----------------|
| 6 | f899a29c | 693 pass / 1 fail / 3 skip / 3 unc | fail (last leg only) | tc-002 planner probe |
| 7 | b69d4b03 | 696 pass / 1 fail / 1 unc / 3 rec | **pass** | tc planner probes |

Run 6 isolated the final nondeterminism: the no-branch merge probe targeted
the FAILING mission's jobId, but the first `task_failed` frame only proves
one task exhausted retries — sibling tasks were still running worktree git
ops, and the concurrent merge blocked past the 60s window (48 sibling frames
seen, no task_error; a manual live probe showed task_error arrives in ~1s
when nothing else runs). Fix `036a04d`: the probe targets the COMPLETED
mission — no concurrent activity. Run 7: mission-seed passes, second
consecutive full-run pass.

The planner-prompt guard (246fda6) shipped but was NOT in run 6's binary —
the stale-artifact trap recurred for cerberus's own binary. The env script
now rebuilds both `build/cerberus` and the sibling bridge before every run.
With the guard actually compiled in (run 7), it only partially helps:
expectations shifted from "200/page loads" toward "expect the rejection"
(tc-001), but the planner still invents endpoints (/health/live, /readyz).
Soft prompt guidance has a ceiling; the repair loop now routinely fixes
these (repair-tc-001 pass), so the family costs a few verdicts per run, not
correctness. A structural fix (dropping planner cases whose path is absent
from the project model) is the eventual answer — recorded, not urgent.

Findings after run 7: only the 2 OAuth outbound-transport observations
remain open (environmental).
