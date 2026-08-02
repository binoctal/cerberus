# Plans — completed history (not a backlog)

> **Audited 2026-08-02.** Every plan in this directory was checked against the
> current code for its distinguishing file/symbol/behavior. Result: essentially
> all DONE. This directory is a **completed-work archive**, not a pending
> backlog. Do NOT pick "next work" from here — its unchecked `- [ ]` boxes are
> stale (work landed but boxes were never toggled).

## Audit summary

- **~53 / 57** plans fully DONE in code.
- **4 PARTIAL = deliberate design supersession** (not debt):
  - `2026-07-24-ws-scout-relay-generation` — replaced by the `assemblePlan`
    approach in `assembly.go`.
  - `2026-08-01-protocol-infer-grounding` — `validateGrounding` retired by the
    two-pass grounding plan (`twopass.go`).
  - `2026-07-27-scout-zero-case-deterministic-fallback` — the post-augment abort
    guard was replaced by session-layer graceful empty-plan handling
    (`session/resume_phases_helpers.go`).
  - `2026-06-16-runtime-management-refactor` — per-platform path functions
    dropped in favor of the single `.cerberus/runtime/` layout (see the project
    CLAUDE.md "Runtime Files" section).
- **1 PARTIAL = possibly real deferred debt** (confirm before acting):
  - `2026-06-16-architecture-issues-fix-plan` — broad refactor (Run →
    initialize/executeHead/finalize split, ValidationRule pattern, store Builder,
    llm RetryMiddleware) not applied; only SessionConfig (P0 #1) landed.

## If you are looking for next work

This directory will mislead you. Use an authoritative signal instead: a goal
stated by the human, a failing test, a dogfood doc's open gap, or a fresh spec.
The `specs/` siblings are design intent (some still active); this `plans/`
directory is execution history.
