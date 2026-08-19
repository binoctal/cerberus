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

- `/internal/orchestrator/alarm` still resolves no user (alarm payloads
  carry neither userId nor missionId) — open, unfixed on the branch.
- open-agents branch is NOT merged to main (awaiting review).
