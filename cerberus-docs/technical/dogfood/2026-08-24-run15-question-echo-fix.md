# Run 15 — question-echo defect fixed; zero spurious task_question

Date: 2026-08-24 (follows the run-14 doc, same day)
Run 15 session: 5e624718-5b23-421f-b4fa-89f8f5df584d, exit 0 —
**699 pass / 0 fail / 1 uncertain / 0 recovered, coverage 100% (reached:true,
gaps:0)**, 24m55s, ~711K tokens.

## The defect (run-13 residual, root-caused)

buildTaskPrompt embedded the literal `[QUESTION]` marker TWICE in the
instruction line that every task prompt ends with. The PTY path echoes the
prompt back as CLI output, and terminals hard-wrap echoed text at the column
width — a wrap landing right before ANY marker occurrence puts it at a
genuine `\n` line start, where extractQuestion's line-prefix check (the
2026-08-21 known-issue-#9 guard) correctly-but-wrongly treats it as a real
question. Bridge logs 2026-08-18..24 show all variants:

- `Should I use JW` (run 13, 2026-08-24) — example sentence split mid-word
- `Should I use JWT or ses` / `or session-based authent` — other PTY widths
- `followed by your question. Example: [QUESTION] …` — the INSTRUCTION
  sentence's own marker (not the example) landed at a line start, proving a
  wording tweak of the example alone could never fix it

Chunk-boundary guard can't help: the wrap inserts a real newline, so the
marker IS at a true line boundary.

## Fix (systematic-debugging: source-level root cause)

- **open-agents bridge 9839ad1** (submodule branch
  `fix/task-prompt-no-literal-question-marker`, parent 5d429df bumps the
  pointer): buildTaskPrompt now DESCRIBES the marker ("the QUESTION marker
  enclosed in square brackets") instead of spelling it out — no echoed line
  can ever open with the literal. Detection contract unchanged. Regression
  tests: (a) the prompt must not contain the literal marker, (b) a
  hard-wrapped echo at any width 10–160 must never fire detection (this test
  was red at width 22 against the old wording — the live failure mode
  reproduced in a unit test).
- **cerberus 1e20463** (branch `fix/dogfood-question-echo-defect`): the four
  dogfood shims' completion leg keyed on the literal marker in the prompt;
  it now triggers on the `--- Instruction ---` header (present in every
  task prompt, arrives after the fail/ask markers that ride the
  title/description). Fail/ask semantics unchanged — 12/12 shim legs
  verified locally before the live run.

## Validation ladder

1. bridge `internal/bridge` unit tests green (new tests red-then-green);
   `TestProtocolManagerACPThenPTYFallback` (test/integration) fails
   identically on a CLEAN submodule main — pre-existing, unrelated.
2. apps/api vitest full: 142 files / 1775 tests green.
3. `make integration-openagents`: green.
   `TestRelayCoverage_ProbeTriggerable` flaked once in three full-suite
   runs (passed solo and on rerun; bare-WS-relay path, no task-prompt
   involvement) — same "transient wrangler window" family as run 14's
   /health 404, recorded as an observation, not a regression.
4. Live run 15 (above): **zero spurious question firings** — the only
   `asking question` log lines are the genuine `what is the magic word?`
   from the mission-seed question leg (fired twice for one job, the same
   pre-existing duplicate as run 14; the mission still completes). The
   fail mission exits cleanly with no task_question. mission-seed case
   pass (0.9).

## Residuals / observations

- The 1 uncertain: `http-route-realtime-post-api-admin-learning-metrics-
  update-priorities` (correctness 0.6, degraded 1) — judge
  evidence-quality hesitation on an admin route, unrelated to this fix.
- Run 14's transient /health 404 did NOT recur (0 recovered, tc-003 clean).
  Keep watching; if it recurs, compare against wrangler hot-reload windows.
- Repo-topology lesson recorded in memory: `open-agents/bridge` is a git
  submodule with its own branches/stash — a stash/checkout sequence run from
  `bridge/` hit the wrong repository (changes recovered from the submodule
  stash; the parent branch name doesn't exist there). Bridge fixes need a
  submodule commit PLUS a parent pointer-bump commit.
