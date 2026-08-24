# Runs 13-14 — human-in-the-loop question edge; coverage reaches 100%

Date: 2026-08-24 (follows the run-12 plan-limits doc, same day)
Run 13 session: 521968ba-432c-4bb9-857f-a2a0621df783, exit 0 — 703/1 (mission-seed fail, see below)
Run 14 session: c2cc0c38-8a89-40b5-889f-99a04e9ab16b, exit 0 — **700 pass / 0 fail / 1 recovered, coverage 100% (reached:true, gaps:0)**

## The last gap

`bridge→web workflow:task_question` was the final never-exercised required
edge (99.74% since 2026-08-17): the dogfood shim treated [QUESTION] purely
as the completion exit, and mission-seed asserted the type ExpectAbsent
(the 74734d7 false-positive guard — which stays, scoped to the completion
mission).

## Fix (cerberus ef1561c + run-14 follow-up)

- `shim/claude` ask mode: a CERBERUS_ASK line (mission text rides the task
  title/description, same placement trick as CERBERUS_FAIL) makes the shim
  print a line OPENING with `[QUESTION]` — extractQuestion's exact
  contract — park until the bridge injects the web user's answer
  ("User answered your question:"), then complete. Fail/completion
  semantics unchanged.
- mission-seed leg 12 (question mission): create the mission, receive
  workflow:task_question, answer via web→bridge workflow:task_answer with
  the deterministic `{missionId}_t0` id, observe workflow:task_completed.

## Run 13's lesson (the fail that shaped run 14)

The planner assigned the question mission to **codex** (whim) — the old
exit-1 stub can never ask; the retry ladder then burned 30s ACP connects
per rung and the 300s receive window expired. Fix: ALL PTY shims
(codex/kiro/cline) now carry the same marker-driven logic (task-prompt
markers, not the CLI identity, decide behavior — the fail mission still
exhausts, the question mission asks from ANY agent), and the
task_question window widened 300→600s for pool-queue + ACP-retry delays.
Run 14: `Task job_1787576809266_t0 asking question: what is the magic
word?` → web answer → completion; mission-seed PASS.

## Residuals / observations

- Run 14's 1 recovered: tc-004 got a transient 404 on the REAL /health
  route (3 attempts) — the same run's route-sweep /health case passed, so
  a momentary wrangler window; the repair loop recovered it.
- Run 13 observed a spurious task_question on the FAIL mission
  ("Should I use JW…", same millisecond as session create, process exit
  1 immediately): the PTY INPUT ECHO of buildTaskPrompt contains an
  example line opening with [QUESTION] at a genuine line start — line-prefix
  extraction cannot distinguish the prompt's own example from a real
  question. Harmless today (pending question dies with the session), but
  it is a real open-agents defect candidate: buildTaskPrompt's example
  should not open a line with the marker.
