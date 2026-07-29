# Recovered Rendering + Tally Correction — Design

**Date:** 2026-07-29
**Follow-up to:** `2026-07-28-ws-a1-runtime-fallback-design.md` (A1 Phase 2)
**Scope decision:** fix the tally pollution Phase 2 introduced AND render `recovered` in human-facing output (correctness + rendering, together).

## Problem

A1 Phase 2 appends a lazy fallback as an extra `agent.StepResult` (its own trace, `Recovered=true`, `TestCase.FallbackFor` bound to the primary) after the primary's failing result. The primary and its fallback share the same `Target`. Downstream consumers are blind to `Recovered`/`FallbackFor`, which causes two classes of defect:

1. **Tally pollution (correctness).** `session.FromResults` sets `TotalCases = len(results)` and counts the fallback's pass into `Passed`, so one recovered role is counted once as fail (the primary) and once as pass (the fallback); `CoveragePct` is inflated in both numerator and denominator. `TestCasesPlanned` counts the lazy fallback as a planned case. `verdictByNormalizedTarget` lets the fallback's pass overwrite the primary's fail for the shared target, corrupting the procedural-effectiveness EMA. `writeEpisodicMemory` writes two episodic rows for one target.
2. **Invisibility (rendering).** `store.Verdict` carries no recovery signal, so CLI summary, Markdown/HTML reports, and JUnit XML never show that a role was recovered rather than cleanly passed.

## Goal

A recovered role is a distinct outcome category: it counts toward coverage (the deterministic fallback proved the role viable) but is visually distinct from a clean pass, and it never double-counts. The primary that was rescued is reclassified out of `Failed`.

## Outcome model (the core)

A "role unit" is either a standalone case, or a primary + its lazy-fallback pair. Pairing uses `TestCase.FallbackFor` (the fallback declares the primary it covers) — not `Target`, which is shared but not a precise link.

Given results `R`:

1. `recoveredPrimaryIDs = { r.TestCase.FallbackFor | r.Recovered && r.TestCase.FallbackFor != "" }` — primaries whose role was rescued.
2. A result is a **fallback result** iff `r.TestCase.FallbackFor != ""`. Fallback results are never independent tally units.
3. Tally iterates only **non-fallback** results (primaries + standalone cases):
   - if `r.TestCase.ID ∈ recoveredPrimaryIDs` → `Recovered++` (the primary is reclassified; it does NOT count as `Failed`);
   - else switch on status → `Passed` / `Failed` / `Skipped` / `Uncertain`.
4. `TotalCases = len(R) - fallbackResultCount`.
5. `CoveragePct = (Passed + Recovered) / TotalCases * 100`.

**Golden example.** Plan roles A, B, C; A has fallback A′, B has fallback B′, C standalone. Execution: A fail + A′ recovered; B fail + B′ fail (not recovered); C pass. Expected: `Passed=1, Failed=1, Recovered=1, TotalCases=3, Coverage=(1+1)/3=66.7%`. Non-fallback results are {A, B, C}; `recoveredPrimaryIDs={A}`; A→Recovered, B→Failed, C→Passed.

This model applies identically to the verdict path (`FinalVerdict` embeds `StepResult`, carrying both `Recovered` and `TestCase.FallbackFor`) and the raw-results fallback path (when the Examiner did not run).

## Encoding (zero-migration)

`recovered` is encoded as a 5th verdict status string, not a new column.

- New constant in `internal/head/examiner/types.go` alongside the existing `JudgeStatus` values: `StatusRecovered JudgeStatus = "recovered"`.
- In `examiner.PersistFinalVerdicts`, after computing `status := string(v.Status)`, translate a recovered fallback before storage:
  ```go
  if v.StepResult.Recovered {
      status = string(examiner.StatusRecovered)
  }
  ```
  The fallback already carries a non-zero `TraceID` (it runs `executeStep` → the steps path), so the existing `TraceID == 0` skip guard does not drop it; the recovered verdict persists as its own row with `status = "recovered"`.
- No `store.Verdict` schema change, no migration, no `CreateVerdict` signature change (`CreateVerdict` has exactly one production caller, `PersistFinalVerdicts`).

The in-memory verdict for a recovered fallback keeps `Status == StatusPass` (the Examiner judged it a pass, which is correct — it did pass). The `"recovered"` string exists only in the persisted row and anywhere that row is reloaded or rendered. `FromResults` reads `StepResult.Recovered` (in-memory) for the live path and the status string for the DB-reload path.

## Changes by site

### Counting (correctness — the load-bearing work)

| Site | Change |
|---|---|
| `session.SessionSummary` (`summary.go`) | Add `Recovered int json:"recovered"`. |
| `session.FromResults` (`summary.go:48`) | Implement the outcome model. Detect fallback results and `recoveredPrimaryIDs` from whichever slice is being iterated (verdicts via embedded `StepResult`; raw results directly). `TotalCases = len(results) - fallbackResultCount`. `CoveragePct = (Passed+Recovered)/TotalCases`. `Recovered` counted in both the verdict branch and the raw-results branch. |
| `TestCasesPlanned` call sites | `run_phases_lifecycle.go:78` and `resume_phases_helpers.go:80` both pass `len(rp.plan.Cases)`, which includes lazy fallback cases. Introduce a helper `plannedCaseCount(plan) = count of cases with FallbackFor == ""` and pass it at both sites so the planned count reflects real roles. |
| `verdictByNormalizedTarget` (`run_phases_consolidate.go:127`) | Committed loop: `if v.Status == "recovered" { continue }` before inserting into the map. In-memory loop: `if v.StepResult.Recovered || v.StepResult.TestCase.FallbackFor != "" { continue }`. Net effect: a recovered role's effectiveness signal comes from its **primary's fail** (the recalled strategy failed), not the deterministic fallback's pass. This is a latent-bug fix. |
| `writeEpisodicMemory` (`run_phases_consolidate.go:31`) | Skip fallback verdicts (`FallbackFor != ""`) so a target gets one episodic row (from its primary), not two. |

### Rendering (the visibility work — downstream of the status string)

| Site | Change |
|---|---|
| `summary.String()` (`summary.go:108`) | Append `, %d recovered` after `uncertain` in the `Verdicts:` line. |
| Markdown summary table (`markdown_render_summary.go`) | Add a `Recovered` column. |
| Markdown verdicts table + `statusEmoji` (`markdown_helpers.go`) | Add `case "recovered": return "♻️ recovered"`. |
| HTML (`html_template.go`) | Add a `Recovered` summary card and a `badge-recovered` CSS class (alongside `badge-pass`/`badge-fail` at lines 21–32). |
| JUnit `buildJUnitCase` (`junit_case.go`) | Add `case "recovered":` → a **passing** testcase (no `<failure>`/`<error>`, so the suite does not fail because of a rescue), with `tc.Name += " (recovered)"` and an optional `SystemOut` note. `suite.Tests` (=`len(verdicts)`) includes it. |

## Verification

- `FromResults` outcome model: a table-driven test using the golden example (A/B/C with A′ recovered, B′ not, C standalone) asserting `Passed=1, Failed=1, Recovered=1, TotalCases=3, CoveragePct≈66.7`, plus an all-recovered case and a no-fallback baseline.
- `PersistFinalVerdicts`: a recovered fallback's row is stored with `status="recovered"`; a non-recovered result keeps its original status.
- `verdictByNormalizedTarget`: with a primary fail and a recovered fallback sharing a target, the map entry is the primary's fail (recovered does not overwrite).
- `plannedCaseCount`: excludes `FallbackFor != ""` cases.
- Renderers: `statusEmoji("recovered")`, Markdown summary column, HTML badge class, and JUnit case name suffix each have a focused test.
- `make check` (fmt + lint + test -race) EXIT 0.

## Out of scope

- **Parallel progress event for the fallback.** The serial path emits a `case_complete` `ProgressEvent` for the activated fallback (`executor_run.go:117`); the parallel path (`parallel_execute_helpers.go`) does not. This is a pre-existing Phase 2 inconsistency and the production progress channel is unwired anyway (`SetProgressChannel` has no production callers). Deferred.
- **Examiner judge awareness of recovery.** The judge judging the fallback as `pass` is correct; no examiner reasoning change. Consistent with Phase 2's "Examiner out of scope".
- **Primary verdict's episodic label.** This design only de-duplicates episodic writes (one row per target, from the primary); it does not rewrite the primary's recorded outcome to `"recovered"`. Recording the primary as `fail` in episodic memory is acceptable for this follow-up; revisit if recall quality suffers.
- **Non-WS runtime fallback**, **agent-authored WS synthesis**, **in-session Scout↔Agent↔Examiner loop** — carried over from the Phase 2 out-of-scope.

## Files

- `internal/head/examiner/types.go` — `StatusRecovered` constant.
- `internal/head/examiner/verdict_persist.go` — translate `Recovered` → `"recovered"` status before `CreateVerdict`.
- `internal/session/summary.go` — `SessionSummary.Recovered`, `FromResults` outcome model, `String()`.
- `internal/session/run_phases_lifecycle.go`, `internal/session/resume_phases_helpers.go` — `plannedCaseCount`.
- `internal/session/run_phases_consolidate.go` — `verdictByNormalizedTarget` + `writeEpisodicMemory` skip rules.
- `internal/report/markdown_render_summary.go`, `markdown_render_verdicts.go`, `markdown_helpers.go` — recovered column/emoji.
- `internal/report/html_template.go` — recovered card + badge.
- `internal/report/junit_case.go` — recovered case.
- Tests alongside each.
