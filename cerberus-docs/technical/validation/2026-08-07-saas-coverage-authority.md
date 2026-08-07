# SaaS Coverage Authority — Three-Authority Model

> Technical note. Date: 2026-08-07.
> Spec: see `cerberus-docs/superpowers/specs/2026-08-07-saas-coverage-authority-design.md`
> (this note documents the implemented model; it does not re-derive the design).

## The three authorities

"Testing completeness" for a SaaS/WS service is **not a property of one exploratory
run**. Scout samples cases; one run cannot exercise a 70-edge protocol. The change
implemented on this branch splits the old single confused question ("is coverage
reached?") into three distinct questions, each answered by a distinct authority:

| Question | Authority | Where it lives |
| --- | --- | --- |
| What should be tested? | The **declared protocol surface**: vocab message edges extracted from source (`session.requiredEdges`, `message_handled` && !`Unsupported` && !`Partial`). | `internal/session/coverage.go` |
| Is it complete? | **Exhaustive attainment** by the vocab-driven suite — `make integration-openagents`, NOT a single exploratory `cerberus run`. | CI / suite runner |
| What did THIS run cover / what's missing? | An **objective message-edge path-coverage measurement** computed from case evidence. | `session.pathCoverage`, `session.exercisedEdges` |

A run therefore answers "progress + gaps" objectively; the suite answers "complete?"
Both are honest. The hallucinated coverage verdict (`coverage_pct=0, reached=false,
gaps=N` invented by the LLM against an unmeasurable SUT) is removed.

## Honesty rule (Phase 1) — unmeasured is "N/A", not "0% / not-reached"

When no objective coverage can be measured for a SaaS/WS session (no local SUT
module to run go-test/jest/pytest against), `AssessCoverage` short-circuits
**before** any LLM call and reports coverage as not-applicable:

- `Assessment.Measured == false`.
- No LLM is invoked for a coverage verdict (the synthetic prompt bias
  `"Objective coverage of gated module: 0.00 ..."` is gone).
- `Reached` is not set to a fake `false`; `CoveragePct` is not set to a fake `0`;
  coverage-kind `Gaps` stay empty.
- `assessCoverageIfContract` logs `"coverage not applicable"` (with the reason
  `"no measurable local SUT (SaaS/WS session); outcome is verdict-based"`)
  instead of the old misleading `reached=false coverage_pct=0`.
- The session summary surface (log + markdown report) reads "coverage: N/A".

The session outcome is the verdict tally (pass/fail), never a fabricated coverage
gate.

## Objective path gate (Phase 2) — `Gate.PathThreshold`

For sessions whose service declares a WS vocabulary, coverage is the fraction of
**declared message edges** exercised by passing case evidence:

- `CoverageMeasurement{Pct: exercised/required, Unit: "path", Known: true}`.
  `Known` is true whenever at least one required edge is declared — a real
  measured 0%, not an unmeasured gap.
- `Gate.PathThreshold` (default **1.0** for has-vocab contracts; see
  `scout.assembleContract(..., hasVocab=true)`) is the required fraction of
  declared message edges that must be exercised. 1.0 = "every required edge must
  be exercised" — the strict completeness bar for the message surface.
- `examiner.AssessCoverage` compares by unit: `line`/`function` →
  `LineThreshold`; `path` → `PathThreshold`. The path branch is enforced **without
  the LLM**.
- Below threshold, the assessment emits a concrete headline `Kind:"path"` gap
  (`"<pct>% exercised < gate"`), and `assessCoverageIfContract` appends one
  concrete `Kind:"path"` gap per unexercised required edge
  (`"edge <from>→<to> <type> not exercised"`). `Kind:"path"` is intentionally not
  `Kind:"coverage"`, so these gaps do **not** feed the coverage repair loop
  (which would otherwise chase phantom coverage gaps).

Routing is structural, not mode-based: `assessCoverageIfContract` calls
`sessionHasVocab(sess)` — true iff any service declares a non-empty
`Vocabulary.Edges`. Has-vocab ⇒ `pathCoverage`; no-vocab ⇒ `sess.lineCoverage`
(unchanged). A session with both local code and a SaaS service still measures
path coverage for the SaaS surface.

### Zero regression for local-codebase sessions

The Phase 2 routing does not alter the line-coverage path:

- `assessCoverageIfContract` → `sessionHasVocab(sess)` is false for no-vocab
  sessions ⇒ `sess.lineCoverage(ctx)` runs exactly as before.
- `assembleContract(..., hasVocab=false)` skips the SaaS branch entirely; the
  gate is byte-identical to the pre-vocab local-codebase contract
  (`CoverageGate = {Module, LineThreshold, BranchThreshold}` from the LLM's
  `set_coverage_gate`; `PathThreshold` left at its zero value, which the line/
  function branch of `AssessCoverage` never reads).

This is locked by existing tests (see the zero-regression note in the task-8
report).

## Out of scope (Phase 3, future)

Non-message paths — auth-reject, silent-drop, orchestrator-callback,
`/broadcast` — are behavior assertions, not edge traversals. Matching them to
case evidence needs a case-intent/assertion mechanism that is not designed here.
Until then these paths stay covered by the **suite** (the completeness
authority), not measured by the **run**. They are not emitted as `Kind:"path"`
gaps; the path gate only ever counts `message_handled` edges.
