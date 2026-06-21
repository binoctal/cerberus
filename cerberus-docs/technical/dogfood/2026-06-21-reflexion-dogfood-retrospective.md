# Reflexion Loop — Dogfood Verification Retrospective

**Date:** 2026-06-21
**Scope:** Dogfooding cerberus on itself to verify the reflexion learning loop and surrounding features.
**Outcome:** All features verified working end-to-end; 10 problem classes found and fixed or documented.

---

## Context

Cerberus's "reflexion" learning loop is meant to let later `cerberus run` sessions benefit from earlier ones: Scout planning recalls per-target history (L1 episodic), recovery recalls learned strategies by embedding (L3 procedural), and strategy effectiveness evolves with real outcomes (grouped atomic EMA). This retrospective records the dogfood verification rounds performed during/after implementing that loop, the problems each round surfaced, and how they were resolved.

Total this effort: **4 dogfood verification rounds**, ~**12 real-LLM `cerberus run` executions**, **29 commits** on `main`. The design itself went through **4 independent adversarial review passes** (spec v1→v4) and was implemented via a **14-task subagent-driven** plan.

---

## Round 1 — Initial dogfood (session start)

Ran cerberus against itself with the real GLM LLM. Found 3 classes of bugs.

| # | Problem | Root cause | Resolution (commit) |
|---|---|---|---|
| 1 | `cerberus architecture` / `cerberus regression` deterministically SIGSEGV | `processTypeSpec` passed a nil `*ast.File` to `countImplementations`, which dereferenced it | Thread the parsed `*ast.File` through + nil guard (`259ee51`) |
| 2 | ReAct action parsing failed ~30×/run | `ActionEnvelope.Raw` JSON tag `"action"` disagreed with the steer prompt's `"payload"` wrapper → Raw empty; empty-payload validation errors were treated as hard failures | Tolerant `ActionEnvelope.UnmarshalJSON` (payload > action > flat) + `actionFromEnvelope` falls back on any error; field-type tolerance for `WaitAction.Duration` / `HTTPAction.Body` / `ProcessExecAction.Timeout`; tolerant `contract.Priorities` type (`f4f4e29`) |
| 3 | 5 complexity regression tests failing (masked by the crash) | Runner matched by exact `issue.File` path, which went stale after refactors | Match by issue type, consistent with SOLID/abstraction runners (`db16b1c`) |

**Effect:** action parse failures 30 → 2; verdicts 3p/6f → 4p/3f.

---

## Interlude — discovery that the loop was not closed

While answering "what differs across repeated runs," investigation revealed the reflexion loop was **not actually closed**: the L1 episodic write function had no caller, L3 recall used an inert substring predicate, and effectiveness never evolved. This triggered:

- **4 adversarial review passes** iterating the spec v1 → v4. Each pass found real issues: v1 over-built (re-implemented planning recall that already existed); v2 fixed two fatal gaps; v3 fixed three more (case_id not globally unique, `Learn` duplication, phase-ordering contradiction) plus a migration landmine and per-row EMA over-application; v4 fixed one blocker (resume consolidate missing EMA + governance).
- **14-task subagent-driven implementation** (migration, normalization helpers, upsert + always-embed, embedding recall, grouped atomic EMA, governance, `cerberus memory` CLI, resume idempotency), merged to `main`.

---

## Round 2 — Post-implementation verification (run 1 + run 2)

Confirmed the loop closes: run 2 recalled run 1's strategies (12 `memory_usage` rows), effectiveness evolved (0.5 → 0.35), all rows consolidated. Surfaced 3 new issues.

| # | Problem | Resolution (commit) |
|---|---|---|
| 4 | Environmental failures (localhost down → HTTP-0) penalized strategies | Added `FailureReasonUnreachable`; classifier detects transport errors; EMA counts only strategy-relevant failures via `CountsAsStrategyEvidence` (`c8ac4f7`) |
| 5 | `memory list --type episodic` unsupported | Added episodic branch to the CLI (`b95a5c4`) |
| 6 | L3 recall threshold (0.1) hardcoded | Moved to `ReActConfig.ProceduralRecallThreshold` / `ProceduralRecallTopK`, configurable (`d6dba82`) |

---

## Round 3 — Post-attribution-fix verification (run 3 + run 4)

Found fix ① incomplete and a deeper design limitation.

| # | Problem | Resolution |
|---|---|---|
| 7 | ① missed the common case: HTTP-0 (connection refused) has `stepResult.Error == nil`; the status-0 signal lives in `Result.Summary`, which the classifier did not inspect | Extended `checkUnreachable` to also scan `stepResult.Result.Summary()` for `"http 0"` (`8e26e4c`). Verified: 5/11 localhost-down failures now correctly `unreachable` |
| 8 | **Design-level limitation:** cases that enter recovery are judged on the *recovery's final action*, not the original failure — so an originally-environmental failure (HTTP-0) is masked when recovery ends on a non-HTTP action (navigate → playwright-missing, wait) and mis-classified `assertion_failed` | Assessed and **documented** in the spec §10 as a known limitation (`19d62e8`). Proper fix is case-level environmental attribution (see Recommendations). A broader string-matching band-aid was rejected as non-general |

---

## Round 4 — Final confirmation (run 5)

All features confirmed working: 4 `unreachable` + 1 `llm_quality` excluded from strategy penalty, 45 `memory_usage` rows all consolidated, effectiveness continuing to evolve, CLI episodic working. Known limitation present as documented.

---

## Problem taxonomy

- **Crash (1):** architecture nil-deref.
- **Parse/tolerance (2, 4–7):** LLM output shape / field-type diversity → resolved with tolerant deserialization at every cross-boundary unmarshal.
- **Design/data (3, 8, loop internals):** path matching, per-row EMA, migration landmine, case_id uniqueness, resume wiring, recovered-case attribution.
- **Tooling/UX (5, 6):** CLI gap, hardcoded constant.

**Recurring lesson:** when targeting multiple LLMs, every cross-boundary deserialization must be tolerant; failure classification cannot rely on `stepResult.Error` alone, because executors often surface failures inside `Result`.

---

## Recommendations

### Short term (high value, low risk)
1. **Case-level environmental attribution** (spec §10 records the proper fix): have the examiner consider attempt history when classifying — if any attempt hit an environmental failure, classify the whole case as environmental. Fully resolves the recovered-case mis-attribution. Prioritize once a real running-service scenario exists.
2. **Real-service dogfood.** This effort's dogfood combined two environment anomalies (no playwright + service down) — atypical. Run cerberus against a real web service to exercise typical paths (browser flows, genuine assertion failures).
3. **CI-automated dogfood.** `.github/workflows/dogfood.yml` is currently disabled (missing secrets). Configure `ANTHROPIC_*` secrets and re-enable the weekly run so dogfooding becomes a regression net rather than a manual exercise.

### Medium term
4. **Finer effectiveness attribution:** upgrade from case-level pass-fraction to per-attempt `recExecResult` attribution (spec §10 follow-up), crediting only strategies whose action actually executed and succeeded — eliminates free-rider/scapegoat noise.
5. **L3 recall observability:** add `cerberus memory stats` showing recall hit-rate, effectiveness distribution, archive ratio, so the memory system is inspectable.
6. **Embedding model upgrade path:** trigram is a local approximation. If a stronger embedder is adopted, `reembed` + the model filter are already in place, but an end-to-end re-validation pass is needed.

### Long term / architectural
7. **Reflection/target alignment:** current reflections skew toward "test-framework-own problems." Tuning the reflection prompt toward target-anchored conditions would improve L3 recall relevance for real business failures.
8. **Governance closed-loop validation:** archival is implemented but the dogfood window was too short to trigger it (no >30d memories). After running for a while, audit `cerberus memory list --all` to confirm the archive policy does not prematurely retire useful memories.

---

## Artifacts
- Design spec: `cerberus-docs/superpowers/specs/2026-06-20-reflexion-loop-closure-design.md` (v4, incl. §10 known limitation)
- Implementation plan: `cerberus-docs/superpowers/plans/2026-06-20-reflexion-loop-closure-plan.md`
- Commits: 29 on `main` between `142e7db` and `19d62e8`
