# Run 12 — known-issue #12 fix validation (plan-limits completeness)

Date: 2026-08-24 (follows the run-11 triple-fix doc, same day)
Cerberus session: 79b19183-557c-4067-8d42-b0dc81a56e98, exit 0
Open-agents: d7378d0 (write-path validation + read-path fallback logging)
Cerberus: e3e6e06 (mission-seed payload sends complete sections)

## Summary

697 pass / 0 fail / 1 uncertain / 698 verdicts; coverage 99.74% (2 gaps,
unchanged OAuth-outbound pair); ~750K tokens; 23m28s.

## #12 validation points

- **Seed accepted:** the complete-sections plan POST created the
  cerberus-dogfood plan (a partial section would have 400ed and failed
  the step's 2xx expectation).
- **mission-seed PASS** with the new payload — the completion and
  fail-mission legs both ran (`workflow:task_failed` fired).
- **Read-path observability works:** wrangler logs now name the legacy
  partial rows' fallback keys verbatim, e.g.
  `plan 'free' limits JSON incomplete — keys falling back to code
  defaults: rate_limits.ws_per_min, rate_limits.max_concurrent_tasks,
  rate_limits.a2a_daily, feature_gates.worktree, ...` — the 2026-08-18
  silent-429 mystery would have been a one-line diagnosis.
- Unit ladder: `plan-limits.test.ts` +
  `plans-limits-validation.test.ts` (4 route cases); full apps/api suite
  1775/1775.

## The 1 uncertain

`ws-realtime-wf-task-assign` (correctness 0.5) — judge response degraded
("unable to determine verdict", Level 3 pending review by design), a
transient LLM-parse family, NOT the run-10 recipients=[] misread (that
one produced a substantive fail critique at 0.4; the run-11 fix holds —
the same case passed 0.88 there and the send-side reasoning elsewhere in
this run correctly treated empty recipients as scope).
