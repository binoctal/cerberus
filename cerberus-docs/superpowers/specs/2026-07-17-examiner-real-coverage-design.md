# Examiner Real Line Coverage — Design

**Date:** 2026-07-17
**Status:** Approved (brainstormed)
**Scope:** `internal/autotest`, `internal/session`, `internal/head/examiner`, `internal/head/contract`, `internal/store`, `migrations`

## Background

The Examiner judges whether a test session met its coverage contract via
`AssessCoverage`, whose objective gate compares a coverage percentage against
`contract.Gate.LineThreshold`. Two latent defects undermine that gate today:

1. **Wrong unit.** `LineThreshold` is documented as *line* coverage, but every
   coverage number feeding the gate is *function*-level:
   - `autotest.autotest_helpers.pct()` returns `CoveredFuncs / TotalFuncs * 100`.
   - AutoTest's `BeforeCoveragePct` / `AfterCoveragePct` are derived from `pct()`.
   - `coverageForSession` path B independently runs a provider that also reports
     function-level coverage.
   The gate therefore compares a function-level percentage against a line-level
     threshold — a unit mismatch.

2. **Dead reuse + missing resume.**
   - `lifecycle_run.go` orders Examiner (Phase 3) *before* AutoTest (Phase 4), so
     during assessment `sess.LastAutoTestReport` is always nil and
     `coverageForSession` path A (reuse `BeforeCoveragePct`) is dead code on the
     run path; it always falls through to path B.
   - The resume path never calls `AssessCoverage`. Worse, it *cannot* without new
     work: the Contract is built only in the Scout phase (`run_phases_scout.go`),
     which resume skips entirely, and the Contract is **not persisted** —
     `SavePlan` stores the test plan (cases), not the contract, so on resume
     `rp.session.Contract` is always nil and any `if Contract != nil` guard is a
     no-op. (Contract currently survives only inside the finalized summary JSON,
     which an interrupted session never writes.)

A prior commit (`7aa79f9`) replaced the original pass-ratio proxy with a provider
run, so the pass-ratio proxy is already gone; the stale `TODO(Plan2)` and
"pass-ratio proxy" comment in `assess.go` are zombie comments.

## Goal

Make `AssessCoverage`'s objective gate compare a **real line-coverage
percentage** (Go) against `LineThreshold`, with correct semantics, correct
fallback when line coverage is unavailable, and coverage assessment that
actually executes on **both** the run and resume paths (which requires
persisting the coverage Contract and reloading it on resume).

## Non-Goals

- Upgrading Node/Python providers to true line coverage (they stay function-level,
  explicitly tagged).
- Changing AutoTest's coverage-driven generation behavior.
- per-service actor on the ReAct path (separate deferred item, out of scope).
- Decoding `LastAutoTestReport` from DB on resume. Under D1 the Examiner no
  longer reads it for the gate, the resume path never runs the AutoTest phase,
  and report rendering reads the DB JSON column directly — so the field is
  genuinely unused on resume. Left as-is (dead-but-harmless) to keep scope tight.

## Design Decisions

### D1 — Gate measures the Agent's tests, not AutoTest's (Approach B)

The phase order stays **Scout → Agent → Examiner → AutoTest**. The Examiner
runs its own line-coverage measurement against `ProjectDir` *after* the Agent has
written tests and *before* AutoTest modifies anything. This measures purely the
Agent's session work.

AutoTest's `Before/AfterCoveragePct` is **decoupled** from the gate and continues
to serve only AutoTest's own gap-driven generation report.

Rationale: moving AutoTest before the Examiner (Approach A) would make the gate
use `AfterCoveragePct`, which conflates AutoTest-generated tests with the Agent's
work — measuring "Agent + AutoTest" against a contract that judges the Agent.

Cost: when AutoTest is enabled, coverage is measured twice (once by the Examiner,
once inside AutoTest's `Run`). Accepted; AutoTest is optional.

### D2 — Go provider reports true line (statement) coverage

`go test -coverprofile` emits a profile whose blocks carry `numStmts`. Go's own
coverage model is statement-based; the total percentage `go tool cover -func`
reports is `coveredStatements / totalStatements`. We compute the same value in
`parseCoverProfile` and store it as `CoverageReport.LineCoveragePct`.

`pct()` prefers line coverage **when line data is present** — discriminated by
`totalStmts > 0`, not by `LineCoveragePct > 0`. A project with genuinely 0% line
coverage (`coveredStmts == 0`) is a valid measurement (0%), not "missing";
gating on `> 0` would wrongly fall back to function-level. `Before/AfterCoveragePct`
thus become line-level for Go automatically, including the 0% case.

Node/Python providers are unchanged; their reports carry function-level
percentages. A new `CoverageReport.CoverageUnit` (`"line"` | `"function"`)
records which unit each report uses, so the gate knows what it is comparing.

### D3 — CoverageMeasurement struct replaces the float parameter

`AssessCoverage`'s 4th parameter changes from `coveragePct float64` to:

```go
type CoverageMeasurement struct {
    Pct   float64 // measured coverage in the report's unit
    Unit  string  // "line" | "function"
    Known bool    // false when no measurement could be obtained
}
```

`Known=false` expresses "coverage unmeasured" (distinct from a measured 0%) — the
gate must **not** force not-reached on unknown coverage (see D4).

### D4 — Unknown coverage degrades to LLM judgment (bug fix)

Today a failed provider run returns `0`, and `0 < LineThreshold` forces
not-reached regardless of the LLM — a false negative. With `CoverageMeasurement`:

- **`Known == false`** (provider failed / no provider / empty profile): the
  objective gate is **skipped entirely** — `Reached` is not overridden, **no gap
  is appended**, `CoveragePct` is set to `0`, and a warning is logged. The LLM's
  judgment stands untouched. (Appending an "unmeasured" gap would bias toward
  not-reached and contradict leaving the LLM in charge.)
- **`Known == true`** (Go line-level **or** Node/Python function-level): the
  existing override applies — `Pct < LineThreshold` → forced not-reached with a
  `coverage` gap. When `Unit == "function"` the gap detail records the unit
  mismatch so reports stay honest, but the gate still fires.

A measured 0% is `Known == true, Pct == 0` and correctly forces not-reached; it
must not be confused with unknown coverage.

### D5 — Persist the coverage Contract so resume can assess (scope expansion)

The Contract is built in the Scout phase and currently lives only in memory, so
resume (which skips Scout) has no Contract and cannot run `AssessCoverage`. Fix
the storage, not just the call site:

- New `sessions.contract TEXT` column (migration `V010__session_contract.sql`,
  mirroring `V005__autotest_report.sql`).
- `store.SaveContract(ctx, sessionID, *contract.Contract)` (UPSERT JSON) and
  `store.LoadContract(ctx, sessionID) (*contract.Contract, error)`. Typed on
  `*contract.Contract`: `store` → `contract` is acyclic (contract imports only
  `encoding/json`, `fmt`).
- Scout writes the Contract via `SaveContract` right after `BuildCoverageContract`
  succeeds (`run_phases_scout.go`).
- Resume loads it via `LoadContract` during initialization and assigns
  `rp.session.Contract`, so the resume `AssessCoverage` call has a real Contract
  to judge against.

`store.Session` (the row struct) gains a `Contract string` field so `GetSession`/
`ListSessions` keep reading the column without breaking.

## Components

### `internal/autotest/types.go`
- Add `LineCoveragePct float64` and `CoverageUnit string` to `CoverageReport`.

### `internal/autotest/coverage_go.go`
- In `parseCoverProfile`, accumulate `totalStmts` and `coveredStmts` across
  blocks (`count > 0` → covered) and set `LineCoveragePct = covered/total*100`
  when `totalStmts > 0`.
- Set `CoverageUnit = "line"`.
- Existing function-level counting is retained for gap detection.

### `internal/autotest/autotest_helpers.go`
- `pct()` returns `LineCoveragePct` when `totalStmts > 0` (line data present,
  including the 0% case), else function-level fallback.

### Node/Python providers
- Set `CoverageUnit = "function"` on their reports. No percentage change.

### `internal/head/examiner/assess.go`
- Replace `coveragePct float64` param with `contract.CoverageMeasurement`
  (placed in `contract` — see Open Question resolution).
- Gate logic per D4:
  - `!Known` → skip override, append **no** gap, set `a.CoveragePct = 0`, warn.
  - `Known` → compare `Pct` to `LineThreshold`; below → force not-reached +
    `coverage` gap (detail notes unit when `Unit != "line"`). Set
    `a.CoveragePct = Pct`.
- Remove the stale `TODO(Plan2)` / pass-ratio comment; document real semantics.

### `internal/session/coverage.go`
- Rewrite `coverageForSession` to run the provider and return a
  `contract.CoverageMeasurement` (Pct, Unit, Known). Delete the dead path A (reuse
  `BeforeCoveragePct`) — under D1 the Examiner no longer reuses AutoTest's report
  for the gate.
- Translation rule from `*autotest.CoverageReport` → `CoverageMeasurement`:
  - `Unit = report.CoverageUnit` (`"line"` for Go, `"function"` for Node/Python).
  - `Pct = report.LineCoveragePct` when `Unit == "line"`, else function-level
    (`CoveredFuncs/TotalFuncs*100`).
  - `Known = true` only when the denominator is non-zero — Go: `totalStmts > 0`;
    Node/Python: `TotalFuncs > 0`. Provider error / nil report / zero-denominator
    → `Known = false`. (A measured 0% has a non-zero denominator → `Known = true`.)
- `lineCoverage` and the `coverageFn` injection are updated to the
  measurement-returning signature.

### `internal/session/lifecycle_types.go` / `lifecycle_factory.go`
- `SessionConfig.CoverageFn` and `Session.coverageFn` change from
  `func(ctx, *Session) float64` to `func(ctx, *Session) contract.CoverageMeasurement`.
- **Impact:** all test stubs that currently return `float64` must return a
  `CoverageMeasurement` — ~8 call sites across `autotest_integration_test.go`,
  `contract_integration_test.go`, `reflexion_integration_test.go`, and
  `resume_idempotency_test.go` (most return `100.0` → become
  `{Pct:100, Unit:"line", Known:true}`).

### `internal/session/run_phases_examiner.go`
- Build a `CoverageMeasurement` via the updated `lineCoverage` and pass it to
  `AssessCoverage`.

### `internal/session/run_phases_scout.go`
- After `BuildCoverageContract` succeeds, persist via
  `store.SaveContract(ctx, session.ID, contract)` (best-effort, warn on error) so
  resume can reload it.

### `internal/session/resume_phases_run.go` / `resume_phases_lifecycle.go`
- During resume initialization, `store.LoadContract(ctx, session.ID)` → assign
  `rp.session.Contract` (was always nil on resume before).
- In `examineResults`, mirror the run path: build a `CoverageMeasurement` via
  `lineCoverage` and call `AssessCoverage` when `rp.session.Contract != nil`.
  Assign to `rp.session.Assessment`.

### `migrations/V010__session_contract.sql` + `internal/store/session.go`
- `ALTER TABLE sessions ADD COLUMN contract TEXT NOT NULL DEFAULT '';`
- `SaveContract` / `LoadContract` methods (mirror `UpdateSessionAutoTest`).
- `store.Session` row struct gains `Contract string`; extend the `GetSession` /
  `ListSessions` `SELECT`/`Scan` lists (mirroring how `autotest_report` was added
  in V005).

## Data Flow (run path)

```
Agent writes tests → Examiner phase:
  lineCoverage(ctx) → coverageForSession(ctx, sess)
    → provider.RunCoverage (Go: go test -coverprofile → LineCoveragePct)
    → CoverageMeasurement{Pct, Unit:"line", Known:true}
  AssessCoverage(ctx, contract, results, measurement)
    → LLM judges; objective gate applies when Known
  sess.Assessment set
→ AutoTest phase (unchanged, decoupled from gate)
```

Resume path is identical from the Examiner step onward.

## Error Handling

- Provider fails / no provider available / empty profile (`totalStmts == 0`) →
  `Known=false` → objective gate skipped, no gap, `CoveragePct=0`, LLM judgment
  stands, warn logged.
- Node/Python (function-level measurement available) → `Known=true`,
  `Unit="function"` → gate applies, gap detail notes the unit mismatch.
- Measured 0% line coverage → `Known=true, Pct=0` → gate correctly forces
  not-reached (not treated as unknown).
- AutoTest off (`AutoTestSafety == "off"`) → no effect on gate; Examiner measures
  independently.
- `SaveContract`/`LoadContract` failure → best-effort warn; resume degrades to
  "no contract" (gate skipped), never aborts the resume.

## Testing (TDD — failing test first for each)

1. `parseCoverProfile`: a fixture profile with known covered/total statements →
   expected `LineCoveragePct`; `CoverageUnit == "line"`. Include a 0%-covered
   fixture → `LineCoveragePct == 0`, still `Known=true`.
2. `pct()`: prefers `LineCoveragePct` when `totalStmts > 0` (incl. 0%), else
   function-level fallback.
3. `coverageForSession`: returns `CoverageMeasurement` with correct unit;
   provider-failure / empty-profile path returns `Known=false` (not `Pct=0`).
4. `AssessCoverage`: known-below-threshold → forced not-reached + gap;
   known-0% → forced not-reached; unknown → **no** force, **no** gap,
   `CoveragePct=0`; function-unit → gap detail notes unit.
5. Contract persistence: `SaveContract`/`LoadContract` round-trip; `GetSession`/
   `ListSessions` still scan the new column.
6. Resume: Contract loaded from DB via `LoadContract`; `AssessCoverage` invoked;
   `Assessment` populated; measurement built via the same `lineCoverage` path as
   run. (Regression guard: today resume never assesses.)
7. Phase-order regression guard: Examiner measurement still occurs before
   AutoTest (AutoTest mutations do not affect the gate value) — asserts existing
   unchanged order under D1.

## Open Question (resolved during writing)

**Where does `CoverageMeasurement` live?** The `examiner` package imports
`contract`; `session` imports both. Placing `CoverageMeasurement` in
`internal/head/contract` (next to `Gate`/`Assessment`) avoids an import cycle and
keeps coverage-contract types together. Adopted.

## Verification

`make check` (fmt + lint + test) must pass.
