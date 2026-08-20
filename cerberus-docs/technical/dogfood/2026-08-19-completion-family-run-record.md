# Completion-family reopen — live run record (2026-08-19)

Verification run for reopening the workflow completion coverage family
after the open-agents `fix/workflow-callback-url` branch (six stacked
defects — see known-issue #13 in
`2026-08-16-openagents-known-issues.md`).

## Setup deltas vs the 2026-08-18 run

- open-agents on `fix/workflow-callback-url` (parent 46a6a2e/7dbaf97 +
  bridge submodule 6ed067b/1210225/c6bee73), bridge binary rebuilt.
- `scripts/dogfood-realtime-e2e-env.sh` now exports
  `OPEN_AGENTS_INTERNAL_SECRET` (parsed from the sibling `.dev.vars`).
- One-time dev-D1 cleanup: `DELETE FROM agents WHERE base_cli = 'claude'`.
- mission-seed agent row `{name:"claude-pty", baseCli:"claude-pty"}`;
  dogfood shim exits 0 two seconds after echoing a `[QUESTION]` line.

## Run 4 (definitive) results

- Verdicts: **689 pass / 1 fail / 1 uncertain** (fail = `tc-001`, a
  scout exploration stub, auth; not part of the deterministic suite).
- Coverage: **1.0 (gaps 0, reached true)** with the completion family
  reopened (`workflow:task_result`, `workflow:task_completed` un-partial;
  `task_error`/`task_failed` stay partial — failure triggers not
  inducible from the happy-path surface, transport fixed).
- mission-seed **PASS**: task_progress → task_question → task_completed →
  job_status received; web-origin task_merge answered with
  `workflow:task_result` (merged).
- Bridge log: `Callback successful for task job_*_t0` — the completion
  HTTP callback delivered for the first time in this environment.
- Claims: 1 proven / 0 emulated-only / 1 unevidenced (schedule-real-cli,
  wont-test, evidenced by the L2 integration suite).

## Examiner drift

**No usable drift aggregate this run.** The judge was budget-degraded
("insufficient budget: remaining ~9.1k, need up to 10k") for 458/691
verdicts, so step-status fallback decided them. Same degradation mode as
the 2026-08-18 run (network-degraded). Last full drift datum remains
0/676 (2026-08-17). Getting a clean drift reading needs either a budget
bump for the examiner head or a smaller suite; not addressed here.

## Iteration log (why four runs)

1. Run 1: callback 500s (D1_TYPE_ERROR) → found defect 5 (internal
   routes lack user context) + shim never exits.
2. Run 2: still npx ACP session (old seed) → confirmed base-cli
   resolution as the blocker.
3. Run 3: claude-pty seed + shim exit landed, but natural CLI exit
   didn't fire the session exit callback → defect 6.
4. Run 4: all six fixes in → PASS, coverage 100%.

## Leftovers

- ~~open-agents branch is NOT merged to main (awaiting review).~~
  RESOLVED 2026-08-19: merged to main as 146164c.

## Follow-up rounds (same day, failure family + drift)

Runs 5-9 chased the remaining leftovers; four more open-agents defects
surfaced and were fixed on the same branch:

7. **Alarm route had no user either** (`0c62e11`): `scheduleAlarm` now
   stamps the orchestrator's userId into the alarm data; the alarm route
   resolves it like the event route.
8. **recoverStuckTasks filtered multiagent_tasks by user_id** — a column
   that table doesn't have; every user-resolved stuckRecovery alarm 500'd
   (`de920a5`, JOIN to missions now).
9. **Planner copied the injected agent-list line verbatim** into
   recommendedAgent ("claude-pty: claude-pty") — task matched no device
   CLI, skipped forever (`de920a5`, `sanitizeRecommendedAgent`).
10. **Internal routes crowded the rate limiter**: secret-authed callbacks
    (no JWT) all keyed into one 'anonymous' IP bucket; a task-output burst
    429'd the retryTask alarms mid-chain (`ab4c17f`, internal routes skip
    the limiter). Also `392f87c`: the event route tolerates unknown
    missions instead of 500-ing the bridge into retry/cache storms.

Dogfood companions: fail-fast stubs for the whole retry fallback ladder
(npx + codex/cline/kiro — the ladder otherwise lands on the HOST's real
codex, which stays alive and stalls retry exhaustion at count 3); the
task-assign case rides the live claude-pty shim (5s post-marker window)
after the judge rightly flipped its dead-session behavior at 0.3.

**Run 9 (final): coverage 100% (gaps 0), mission-seed PASS with the full
failure chain (CERBERUS_FAIL mission → retry exhaustion →
workflow:task_failed; branchless merge → workflow:task_error), 692 pass /
1 fail (tc-004 scout exploration stub, auth) / 1 recovered, 0 judge
degradations — ALL 694 verdicts judged with correctness, judge/step
disagreements 0 (only tc-004 fails, where executor and judge agree).**

**Drift datum (2026-08-19, run 9): 0 incorrect / 694 judged, 0 degraded,
0 under-confidence flips.** The 2026-08-18/19 budget starvation is gone
(session_total_tokens 700K + max_duration 60m in project.yaml).
