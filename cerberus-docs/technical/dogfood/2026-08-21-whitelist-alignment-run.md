# DO whitelist alignment — verification run (2026-08-21)

Closes known issues #1 (rest) and #5 (`2026-08-16-openagents-known-issues.md`),
reopening the session resume / scanner / listDir families in the dogfood vocab.

## Changes

**open-agents** `fix/ws-whitelist-alignment` (merged to main as 9e1c342;
bridge submodule UNCHANGED — every handler already existed):

- room.ts whitelist extracted into exported `BRIDGE_TO_WEB_TYPES` /
  `WEB_TO_BRIDGE_TYPES` Sets; `handleMessage` switches to membership checks
  (semantics identical, contract unit-testable without a DO harness).
- Forwarded web→bridge: `session:resume`, `session:resume-with-context`,
  `scanner:toggle`, `scanner:rules:sync`.
- Forwarded bridge→web: `session:resumed`, `session:resume:failed`,
  `session:resumed-with-context`, `session:resume-with-context:failed`,
  `scanner:status` (`session:cancelled` landed in the earlier burndown).
- Dropped dead whitelist entries `workflow:job_completed` /
  `workflow:task_status_update` (#5 drop decision) + the three no-op web
  listeners for `job_completed` (`job_status` is the live completion signal).
- New contract test `room-whitelist.test.ts`; apps/api 1761 pass, apps/web
  1470 pass.

**cerberus** (main aac3b83, 744259f):

- vocabextract resolves `NAME.has(msg.type)` Set-membership gates and
  `if (msg.type === 'X')` literals when no CaseClause encloses the relay
  call — the no-switch relay style extracts concrete edges instead of
  collapsing to `(dynamic)` (fixture `set-membership.ts`).
- realtime-e2e vocab: +6 bridge→web ack edges, +4 web→bridge command edges,
  dead edges deleted; `scanner:rules:synced` / `device:listDirResult`
  un-marked (handler exists since bridge 7ab073e — the known-issues text
  was stale); `session:resume-with-context:failed` partial (env-dependent).
- protocol roles: declarative realResponder pairs — `scanner:toggle →
  scanner:status`, `scanner:rules:sync → scanner:rules:synced`,
  `session:resume → session:resume:failed` (unknown id),
  `session:resume-with-context → session:resumed-with-context` (claude-pty
  shim), `device:listDir → device:listDirResult`.
- session-family case: resume the live session (await `session:resumed`)
  and await `session:cancelled` after cancel.
- Guard test `TestWhitelistAlignmentEdgesRequired`.

## Integration-suite side findings (fixed in 744259f)

- ws-realtime vocab carried the same two dead edges → TestVocabularyDriven
  relay subtests timed out; deleted there too.
- M1 orchestration flake, two stacked causes: (1) seed agent `claude` →
  npx ACP real-LLM latency blew the 90s window / 5m budget — seeded
  `claude-pty` (zero-LLM PTY shim); (2) with events ~5s after trigger they
  landed inside the connect step's optional 5s device:online auto-await,
  which consumes frames — the trigger POST is now an http_request step
  between connect and receive.
- Bridge harness: `NPM_CONFIG_CACHE` redirected outside the per-bridge
  t.TempDir (ACP child's cache writes raced teardown RemoveAll).

Full live integration suite: **526 subtests pass / 0 fail** (276.6s).

## Dogfood run

Session `46e1e918` (2026-08-21 19:54–20:07 local): **698 pass / 0 fail /
2 recovered**, 13m31s, ~247K tokens, real actors bridge-pty-1/2. The
reopened families executed clean — no new open-agents defects surfaced.

Three tails from this run, all triaged:

1. **Anthropic 429 degradation**: from 12:05:54Z the judge LLM hit 429
   rate limits; 72 judge calls failed over 108s. Examiner degraded as
   designed (step status as verdict, fallback verdicts maximal
   confidence) — outcome counts unaffected.
2. **SIGINT data loss (cerberus defect, fixed)**: a SIGINT arrived at
   12:07:42 mid-examination. The run context canceled the verdict
   persist, every consolidate memory write, and both finalize status
   updates: **0 of 698 verdicts reached the DB** and the session row is
   stuck at `status=running` with empty stats (left as-is — the lost
   rows are the evidence). Fixed in cerberus `17c6f57` (merged to main):
   verdict persistence, both finalize paths, and both consolidate phases
   now run under `context.WithoutCancel`; regression test
   `TestPersistFinalVerdicts_SurvivesCanceledContext`.
3. **tc-003 / tc-004 findings were false positives (fixed)**: these are
   the plan's negative probes (`GET /ws/` expect-400, `GET /health/extra`
   expect-404). The executor's legacy smoke gate flags the expected 4xx
   as a step failure; the examiner then judges the case pass
   (expectation met) — but findings backflow only looked at the step
   failure, so tc-003/tc-004 signatures accumulated in findings.yaml
   across every run (11 entries). All 11 marked `resolved`; `17c6f57`
   also makes backflow skip judge-passed cases and fallback-rescued
   primaries, mirroring the summary's recovered reclassification.

Remaining: claims reconciliation still reports 1 unevidenced claim
(constant across all recent runs, unchanged by this run) — next-run
follow-up. Known issues #4 / #6 / #9 / #12 from the 2026-08-16 list
remain open (of these, #6's family was fixed 2026-08-19; #4 is naming
debt).
