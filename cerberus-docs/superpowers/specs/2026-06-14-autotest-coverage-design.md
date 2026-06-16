# AutoTest: Coverage Gap Detection + Test Generation — Design Spec

**Date**: 2026-06-14
**Status**: Draft (pending user review)

## Goal

Extend `cerberus run` with a post-Examiner phase that runs the project's tests
with coverage, finds uncovered code, AI-generates tests to fill the gaps, and
re-runs to verify. cerberus moves from "judge whether code satisfies a goal" to
"automatically raise test coverage". The phase runs by default; writing the
generated tests is gated by `EscalationGate`.

## Context

cerberus is an AI cognitive test framework today: Scout→Agent→Examiner judges
whether code satisfies a test goal. It does not run `go test` or measure
coverage. Dogfooding exposed this: a broad goal ("build and test pass") yields
a fail verdict because the Agent only runs `go build`, never `go test`, and
cerberus cannot answer "which code is untested".

AutoTest adds: (1) real test + coverage run, (2) gap detection, (3) AI test
generation, (4) re-run verification (generated tests must pass and raise
coverage, else revert).

## Design Principles

1. **Never leave a test that breaks the suite.** Generated tests either pass and
   raise coverage (kept) or are reverted.
2. **Gate is mandatory.** `run` writes code by default; `EscalationGate`
   (`destructive_risk`) sits between gen and write, with three modes.
3. **Language-agnostic interfaces, Go first.** `CoverageProvider`/
   `TestGenerator` interfaces; Go provider implemented first (dogfood cerberus
   itself); Node/Python providers added later without touching the coordinator.
4. **AutoTest is a run phase, not a head.** Post-processing after Examiner; the
   three-head architecture is untouched.

## Architecture

New package `internal/autotest/`. Invoked as Phase 4 of `cerberus run`, after
Examiner.

```
Scout → Agent → Examiner (verdicts)
   → AutoTest: coverage → gaps → gen → gate → write → verify → report
```

### Interfaces

```go
// CoverageProvider runs tests, parses coverage, finds uncovered code.
type CoverageProvider interface {
	RunCoverage(ctx context.Context, projectDir string) (*CoverageReport, error)
	Gaps(report *CoverageReport) []CoverageGap
}

type CoverageReport struct {
	Pass                     bool
	Profile                  []CoverageLine
	TotalFuncs, CoveredFuncs int
}
type CoverageLine struct {
	File         string
	Start, End   int
	Count        int
}

type CoverageGap struct {
	File, Func string
	Reason     string // "0% covered" | "no test file"
}

// TestGenerator produces a test file for one gap.
type TestGenerator interface {
	Generate(ctx context.Context, gap CoverageGap, source []byte) (TestFile, error)
}
type TestFile struct {
	Path    string
	Content []byte
}
```

### AutoTest coordinator

```go
type SafetyMode string
const (
	SafetyApprove SafetyMode = "approve" // default: gate prompts
	SafetyAuto    SafetyMode = "auto"    // write directly
	SafetyDryRun  SafetyMode = "dry-run" // report only, no write
)

type AutoTest struct {
	coverage CoverageProvider
	gen      TestGenerator
	gate     escalation.Gate
	mode     SafetyMode
	logger   *zap.Logger
}

func (a *AutoTest) Run(ctx context.Context, projectDir string) (*AutoTestReport, error)
```

Run flow:

1. `report = coverage.RunCoverage(ctx, dir)` — run existing tests + coverage.
2. `if !report.Pass → abort("existing tests failing; fix before autotest")`.
3. `gaps = coverage.Gaps(report)`.
4. For each gap: read the gap function's source (`os.ReadFile(gap.File)` +
   `go/parser` to extract the func body), then
   `testFile = gen.Generate(ctx, gap, source)`.
5. Gate switch (`mode`):
   - `dry-run`: append testFile to report + stdout preview; do not write.
   - `approve`: `gate.Request(destructive_risk, files, preview)` → approved writes / denied skips.
   - `auto`: write directly.
6. `write(testFile)`; record path for possible revert.
7. Re-run `go test`: keep only if **pass AND `AfterCoveragePct > BeforeCoveragePct`**;
   on compile error / assertion fail (or a test that passes but adds zero coverage)
   → revert (`os.Remove`). A generated test that does not actually raise coverage
   is reverted too — it did not fill the gap.
8. Emit report (before/after coverage).

### Gate modes

| mode | behavior | scenario | flag |
|---|---|---|---|
| `approve` (default) | pause after gen, list files, user confirms before write | dev / Claude Code | `--auto-test-safety=approve` |
| `auto` | write directly, report git diff after | CI | `--auto-test-safety=auto` |
| `dry-run` | report gaps + generated content only, no write | audit / preview | `--auto-test-safety=dry-run` |

Reuses the existing `EscalationGate` `destructive_risk` checkpoint.

## Error handling

| failure | handling |
|---|---|
| existing tests fail | abort (don't build on a failing baseline) |
| coverage cannot run | abort + report cause |
| AI generation fails | skip gap, record `failed` |
| write fails | skip, record `failed` |
| re-run fails (compile / assertion) | revert (`os.Remove`), record `reverted` |
| gate denied | skip, record `skipped` |

Revert: every write records its path; step 7 failure `os.Remove`s. Does not
depend on git (cerberus does not assume a git worktree).

## Output

```go
type AutoTestReport struct {
	Gaps                  []CoverageGap
	Generated             []TestFile
	Written               []string
	Skipped, Failed       []string
	Reverted              []string
	BeforeCoveragePct     float64
	AfterCoveragePct      float64
	Duration              time.Duration
}
```

Printed after the phase, stored in the session record (dashboard-visible).

## Go provider (first)

### GoCoverageProvider
- `go test -coverprofile=<tmpdir>/cover.out ./...` (written to a temp dir, not
  projectDir, so the project tree stays clean)
- `go tool cover -func=<tmpdir>/cover.out` for function-level coverage
- Gaps: (1) 0%-covered functions (count=0 blocks); (2) source file `foo/bar.go`
  with no `bar_test.go` in the same directory → "no test file"

### GoTestGenerator
- `go/parser` reads the gap function's signature + body
- LLM prompt: signature + body + package name + existing `_test.go` style in the
  same package
- Emits `_test.go` (table-driven)

### YAGNI skips
- cgo / generated code (`*.pb.go`, mocks)
- `main` packages (`cmd/`)
- `_test.go` files themselves
- cross-package generation
- fuzz / benchmark

Node/Python providers arrive later under the same interfaces
(`jest --coverage` / `pytest --cov`).

## Testing

| layer | method |
|---|---|
| CoverageProvider | fixture `cover.out` + expected gaps (pure parse, no real `go test`) |
| TestGenerator | `MockClient` + fixed source → expected `_test.go` structure |
| AutoTest.Run | in-memory fs + mock provider/gen/gate → four paths: dry-run / approve-deny / auto / revert |
| gate integration | mock `Gate` (approve/deny/auto) |
| end-to-end | cerberus autotest dogfood (cerberus fills its own test gaps) |

run integration: `AutoTest.Run` after Examiner in lifecycle; mock verifies phase
invocation and that existing run behavior is unchanged.

## Out of scope (this spec)

- Node/Python providers (later spec, same interfaces)
- fuzz / benchmark generation
- cross-package test generation
- iterative test refinement (gen→fail→LLM-repair→retry): single-shot first,
  multi-round later

## Verification

- `make check` green
- dogfood: `cerberus autotest` on cerberus itself shows a measurable coverage
  gain with no broken tests left behind
