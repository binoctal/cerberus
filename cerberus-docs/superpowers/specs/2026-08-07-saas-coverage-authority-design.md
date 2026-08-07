# SaaS Coverage Authority — Design

> Status: design for planning. Date: 2026-08-07.
> Context: a live `cerberus run` against open-agents (a SaaS WS service) reported
> `coverage assessment reached=false, coverage_pct=0, gaps=5/10` — a hallucination.
> The coverage gate measures line/function coverage of a local SUT module; an
> external service has no local module, so the LLM emitted a fake 0% verdict with
> invented gaps. This spec replaces that with an honest, objective model.

## Problem (root cause)

`contract.CoverageGate` is `{Module, LineThreshold, BranchThreshold}` — a **code
module's line/branch coverage**. `coverageReportForSession` runs go-test/jest/pytest
against `sess.ProjectDir`. For a SaaS/WS session (the SUT is a remote service),
`ProjectDir` has no SUT source → the provider returns `Known=false`.

`examiner.AssessCoverage` then:
1. Calls the LLM anyway, feeding it `"Objective coverage of gated module: 0.00
   (unit: , gate: 0.00)"` — which biases the LLM toward a 0%/not-reached verdict.
2. Sets `a.CoveragePct = 0` and accepts the LLM's hallucinated `Reached=false` and
   `Gaps`.

The comment claims "do NOT bias the verdict," but the prompt itself is the bias.
The result is a confident-looking lie: `coverage_pct=0, reached=false, gaps=N`,
where N and the gap contents are fabricated.

This also corrupts the coverage-repair loop, which reuses `Assessment.Gaps` to
generate filler cases — chasing phantom gaps.

## The authority model (the coherent path)

"Testing completeness" for a SaaS service is **not a property of one exploratory
run** (Scout generates a sample of cases; one run cannot exercise a 70-edge
protocol). It is a property of the **declared protocol surface** being
exhaustively tested. Three distinct questions, three distinct authorities:

| Question | Authority |
| --- | --- |
| What should be tested? | The **declared protocol surface**: vocab message edges (extracted from source) + declared non-message paths. |
| Is it complete? | **Exhaustive attainment** of that surface — the vocab-driven suite (`make integration-openagents`), not a single run. |
| What did THIS run cover / what's missing? | An **objective path-coverage measurement** the run computes from case evidence (Phase 2). |

A run therefore answers "progress + gaps" objectively; the suite answers
"complete?" Both are honest. The hallucinated coverage verdict is removed (Phase 1).

## Scope

**Phase 1 — Honesty (implement now).** Stop the hallucinated coverage verdict
when objective coverage is unmeasured (SaaS / no local SUT). Coverage is reported
as not-applicable; the session outcome is verdict-based (pass/fail), not a fake
coverage gate.

**Phase 2 — Objective message-edge path coverage (implement now).** A path
coverage provider for WS/SaaS sessions: coverage = declared vocab message edges
that were exercised by passing cases. Gives the run an honest `X/N edges + gap
list`. No LLM in the measurement; no manual edge declaration (the vocab is the
declared surface).

**Phase 3 — Non-message paths (out of scope; open question).** Auth-reject,
silent-drop, orchestrator-callback, `/broadcast` are behavior assertions, not
edge traversals; matching them to evidence needs a case-intent/assertion
mechanism that is not designed here. Deferred to a follow-up brainstorm. Until
then these stay covered by the suite (the completeness authority), not measured
by the run.

**Non-goal:** changing the line/function coverage path for local-codebase
sessions (zero regression there). Auto-discovering required error paths (still
human-declared).

## Decisions

### Decision 1 — Unmeasured coverage is "not applicable," not "0% / not-reached"

When `CoverageMeasurement.Known == false`, `AssessCoverage` must NOT call the LLM
for a coverage verdict and must NOT emit a synthetic `Reached=false`/`CoveragePct=0`.
The assessment reports coverage as not-applicable; `assessCoverageIfContract` logs
`"coverage not applicable (no measurable local SUT)"` instead of the misleading
`reached=false coverage_pct=0`. The session outcome is the verdict tally.

This is implemented as: `!m.Known` ⇒ short-circuit before the LLM call, return an
assessment marked not-applicable (new `Assessment.Measured bool` = false; `Reached`
left unset/true so it cannot be mistaken for a coverage failure; `Gaps` empty for
the coverage kind). `assessCoverageIfContract` and the session summary surface
"coverage: N/A" honestly.

### Decision 2 — Message-edge path coverage provider (Phase 2)

A new coverage path for sessions whose service declares a WS vocabulary:

- **Required surface** = the service's `Vocabulary.Edges` filtered to
  `Trigger == "message_handled" && !Unsupported && !Partial`. This is the
  authoritative, source-extracted set — no manual declaration, no LLM.
- **Exercised** = derived from the session's case evidence. For each case, build
  the `connectionID → role` map from its `ws_connect` steps (Role field), then
  read the evidence: a `ws_send` of type T from role Rₛ and a matched `ws_receive`
  of T by role Rᵣ exercises edge `(Rₛ → Rᵣ, T)`. This reuses the correlation logic
  already proven in `examiner.deriveDimensions`.
- **Measurement** = `CoverageMeasurement{Pct: exercised/required, Unit: "path",
  Known: true}` (Known=true whenever a non-empty vocab is declared, even if 0
  exercised — a measured 0%, not an unmeasured one).

### Decision 3 — Gate carries a path threshold

Add `Gate.PathThreshold float64`. `AssessCoverage` compares by unit: line/function
→ `LineThreshold`; path → `PathThreshold`. Codebase sessions set only
`LineThreshold`; SaaS/WS sessions set `PathThreshold`. Default `PathThreshold`
semantics: 1.0 means "every required edge must be exercised" — the strict
completeness bar for the message surface. Configurable per project.

### Decision 4 — Routing: path coverage when a vocabulary is declared

A session uses the path-coverage provider when at least one service declares a
non-empty `Vocabulary`; otherwise it uses the existing line/function provider
(unchanged). Detection is structural (has-vocab), not mode-based, so a session
with both local code and a SaaS service still measures path coverage for the
SaaS surface. (Local-codebase-only sessions are unaffected.)

## Architecture

```
session (Examiner phase)
 ├─ measurement = lineCoverage()                          ── existing, local SUT
 │              OR pathCoverage()                          ── NEW, SaaS/WS
 ├─ AssessCoverage(contract, results, measurement)
 │    ├─ Known=false  ⇒ NOT APPLICABLE (no LLM, no fake 0%)   ── Phase 1
 │    ├─ Unit="line"  ⇒ compare vs LineThreshold (existing)
 │    └─ Unit="path"  ⇒ compare vs PathThreshold; Gaps = unexercised edges ── Phase 2
 └─ Assessment → summary ("coverage: N/A" | "X/N edges, gaps=[...]") + repair loop
```

## Components & Changes

- `internal/head/examiner/assess.go` — `AssessCoverage`: Phase 1 short-circuit on
  `!Known`; Phase 2 path-unit branch comparing `PathThreshold`, emitting concrete
  `Gap{Kind:"path", Detail:"edge <from>→<to> <type> not exercised"}`.
- `internal/head/contract/types.go` — `Gate.PathThreshold`; `Assessment.Measured`
  (or reuse `CoverageMeasurement.Known` propagated) to flag not-applicable.
- `internal/session/coverage.go` — `pathCoverage(ctx, sess)` provider: aggregate
  per-case `connectionID→role` + evidence into exercised edges; route by has-vocab.
- `internal/session/run_phases_*.go` — summary/log surfaces "coverage: N/A" for
  not-applicable; the existing reached/gaps log line only when measured.
- `internal/head/scout/...` — Scout's contract assembly sets `PathThreshold` for
  SaaS/WS contracts (and stops reporting a `LineThreshold`/`Module` that has no
  SUT). This also addresses the contract self-assessment's own finding
  ("CoverageGate non-functional: Module empty, thresholds 0").
- Docs: `cerberus-docs/technical/` note designating the vocab-driven suite as the
  SaaS completeness authority; update the open-agents test report's "confidence"
  section to the authority model.

## Testing

- **Phase 1 (unit):** `AssessCoverage` with `Known=false` ⇒ no LLM call (inject a
  failing/fake driver and assert it is NOT invoked), `Measured=false`, no
  `Reached=false`, no coverage-kind `Gaps`. Regression: `Known=true` below
  threshold ⇒ `Reached=false` with a coverage gap (unchanged).
- **Phase 2 (unit):** `pathCoverage` over a fixture session (vocab + case
  evidence) ⇒ correct `exercised/required` and gap list, including the
  `connectionID→role` mapping and multi-case aggregation. `AssessCoverage`
  path-unit ⇒ `PathThreshold` enforced, gaps name unexercised edges.
- **Routing:** has-vocab session ⇒ path provider; no-vocab session ⇒ line
  provider (unchanged).
- **Live:** a real `cerberus run` against open-agents reports either "coverage:
  N/A" (Phase 1 only) or "X/N edges, gaps=[...]" (Phase 2) — never a hallucinated
  0%/reached=false.

## Success Criteria

- No `cerberus run` against a SaaS/WS service ever emits a hallucinated
  `coverage_pct=0 / reached=false` again — unmeasured reads "N/A"; path-measured
  reads a real fraction.
- A Phase-2 open-agents run reports an objective message-edge coverage fraction
  and a concrete, accurate gap list (no LLM fabrication).
- Local-codebase sessions: byte-identical coverage behavior (zero regression).
- The authority model is documented: suite = completeness; run = objective
  progress/gaps.

## Risks

- **`connectionID→role` inference** assumes each case's `ws_connect` steps carry
  `Role`. Deterministic Scout cases do; LLM free-form cases may use bare
  connection IDs. Unmatched connections are excluded from the exercised set
  (conservative — under-counts rather than over-counts coverage).
- **"Exercised" ≠ "verified"** — an edge touched by a weakly-asserted case counts
  as covered (same limitation as line coverage). Accepted; documented.
- **PathThreshold default** of 1.0 (every edge) is strict; a single exploratory
  run will usually not reach it for a large protocol — which is honest. Projects
  may lower it. The completeness authority remains the suite.
