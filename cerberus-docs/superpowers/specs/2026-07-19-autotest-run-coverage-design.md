# AutoTest RunCoverage Injectable-Runner Refactor — Design

**Date:** 2026-07-19
**Status:** Proposed
**Scope:** `internal/autotest/` — Node / Python / Mocha coverage providers only. Go provider unchanged.

## Problem

`internal/autotest` package coverage is 54.8%, but the 0% figure on the Node / Python / Mocha `RunCoverage` paths is a **symptom**, not the problem. Two real problems sit underneath.

### Problem 1 — Dual exec implementation, dead code, divergent commands

Each of Node / Python has **two** shell-out implementations that disagree, and neither duplicate is exercised by a test:

| Provider | `RunCoverage` inline exec | `DefaultXxxCoverageRunner` |
|---|---|---|
| Node | `config.TestCommand + CoverageArgs` → `npm test -- --coverage --coverageReporters=json`; reads project-local `coverage/coverage-final.json` | `npm test -- … --outputCoverage=<tmpdir>`; reads tmpdir |
| Python | 5-phase helper chain (`determinePythonCommand` → `buildPythonTestCommand` → `applyTimeout` → `executeTestCommand` → `generateCoverageReport`); reads `config.OutputPath` | `coverage run -m pytest --cov-report=json:<tmpdir>`; reads tmpdir |
| Mocha | inline exec like Node | **no Default runner exists** |

`grep` confirms `DefaultNodeCoverageRunner` and `DefaultPythonCoverageRunner` have **zero call sites** outside their definitions — they are dead code. The providers' `RunCoverage` methods inline their *own* exec and never call the Default runners, so the two implementations drift with no test to catch the divergence.

### Problem 2 — Zero regression protection on a core execution path

`RunCoverage` is the path cerberus actually walks when testing Node/Python projects: it builds the command, applies timeout, reads the coverage file, and parses it. It has **no tests**. A broken npm flag, a moved output path, or a parse-path typo would ship silently.

### Symptom (measurable, not the goal)

Audit (2026-07-18) flagged `RunCoverage` for Node/Python/Mocha at 0%. Raising that number is only worth doing as a side-effect of fixing Problems 1 and 2 — not as an end in itself.

## Goals

1. Make Node / Python / Mocha providers use an **injected runner**, identical in shape to `GoCoverageProvider` (`coverageRunner = func(ctx, projectDir) ([]byte, error)` returning coverage-report bytes).
2. Add **hermetic unit tests** (mock runner) covering every `RunCoverage` branch.
3. **Wire up** `DefaultNodeCoverageRunner` / `DefaultPythonCoverageRunner` (no longer dead) and **add** `DefaultMochaCoverageRunner` — one exec path per language, no divergence.
4. Preserve actual shell-out behavior (commands stay equivalent).

## Non-goals

- Do not touch the Go provider, parse functions (already 100%), or detector / routing / language selection.
- Do not fix the fixture tests' "0 verdicts" issue (separate problem; the fixtures run the upper session flow and never reach `RunCoverage` regardless of this refactor).
- No new toolchain dependencies in the default test run.

## Architecture

### Unified model

Every provider holds a `run coverageRunner`. `RunCoverage` narrows to: nil-check → `data, err := p.run(ctx, projectDir)` → parse → set `Pass` / `CoverageUnit` / counts. All "make output dir / assemble args / exec / timeout / read file" logic moves into the language's `DefaultXxxCoverageRunner`. Timeout becomes the runner's internal concern (it owns `ctx`).

Reference implementation: `GoCoverageProvider` in `coverage_go.go` — already exactly this shape.

### Per-language changes

**Node**
- Add `run coverageRunner` field to `NodeCoverageProvider`.
- `NewNodeCoverageProvider(cfg, run, logger)` — **no internal default** (matches `GoCoverageProvider`); `RunCoverage` errors on `p.run == nil`.
- `RunCoverage`: delete the inline exec (dir creation, arg assembly, `exec.CommandContext`, timeout block, file read). Body becomes `nil-check → p.run → p.parseJestCoverage → set Pass/Unit`.
- `DefaultNodeCoverageRunner`: keep as-is (already correct shape: tmpdir + `npm test` + read JSON); it is now the default and the single exec path.

**Python**
- Same structural change (`run` field, constructor takes `run`; **no internal default** — matches Go).
- `DefaultPythonCoverageRunner`: keep (returns JSON bytes); now wired as default.
- `RunCoverage`: `nil-check → p.run → p.parseJSONCoverage → set Pass/Unit`.
- **Decision D1 (see below):** remove `parseCoverageData` (the JSON→SQLite fallback) and `parseSQLiteCoverage`. The injected runner guarantees JSON, so the fallback has no trigger and no test.
- **Decision D2 (see below):** the 5 exec helpers (`determinePythonCommand`, `buildPythonTestCommand`, `applyTimeout`, `executeTestCommand`, `generateCoverageReport`) serve only the old inline-exec model. Delete the four exec-flow helpers; keep `determinePythonCommand` (cross-platform `python3`/`python` selection) and call it inside `DefaultPythonCoverageRunner`.

**Mocha**
- Same structural change.
- **Add** `DefaultMochaCoverageRunner` (mirror Node: tmpdir + `npm test` + read Istanbul JSON).
- `RunCoverage`: delete inline exec → `p.run → p.parseIstanbulCoverage → set Pass/Unit`.

### `provider_factory.go` unification

Today `NewCoverageProviderForLanguage(lang, runner interface{}, logger)` accepts `runner` as `interface{}` but only the Go branch uses it; Node/Python ignore it. Refactor:

- Change `runner` to the concrete `coverageRunner` type (drop `interface{}`).
- Factory selects the per-language Default runner when `runner == nil`, so callers pass one uniform value (or nil).
- Update the two session call sites (`internal/session/run_phases_autotest.go:53`, `internal/session/coverage.go:75`) — they currently hardcode `autotest.DefaultGoCoverageRunner` for every language; simplify to pass `nil` and let the factory pick.

### Decisions requiring confirmation

- **D1 — Remove Python SQLite fallback.** `parseCoverageData` + `parseSQLiteCoverage` (both 0%, untested) are deleted. Rationale: the injected runner always produces JSON; the SQLite path existed only as a recovery when JSON wasn't generated, which can't happen under the new model. This is a **capability removal** — the only behavior change in the refactor. If keeping SQLite support matters, the alternative is to make the runner return a discriminated `{JSON bytes | SQLite path}`, rejected here as over-engineered.
- **D2 — Delete the four Python exec-flow helpers**, keep `determinePythonCommand` inside the Default runner. They become dead code once `RunCoverage` stops inlining exec.
- **D3 — Factory owns Default-runner selection** (callers pass `nil`), rather than callers passing per-language runners. Keeps session code simple; matches Go's existing "default if nil" pattern.

## Testing

### Hermetic unit tests (mock runner) — primary coverage

For each provider's `RunCoverage`:
1. `config == nil` → error.
2. `p.run == nil` → error (matches Go's nil-runner guard).
3. `run` returns `error` → wrapped error.
4. `run` returns valid coverage JSON (reuse existing parse-test fixtures) → success, `Pass == true`, `TotalFuncs`/`CoveredFuncs` populated correctly.
5. `run` returns garbage bytes → parse error.

Node / Mocha additionally:
6. `run` returns a `context.DeadlineExceeded`-wrapped error → timeout error path.

Python additionally:
7. `parseJSONCoverage` happy path is already 100% via `coverage_python_test.go`; the new test reuses its fixture JSON as the mock-runner return value.

### Default-runner end-to-end (skip-if-no-toolchain)

Mirror the existing `DefaultGoCoverageRunner` end-to-end test (`coverage_go_runner_test.go`):
- `DefaultPythonCoverageRunner`: pytest is present on the dev machine → real run against a tiny testdata module.
- `DefaultNodeCoverageRunner` / `DefaultMochaCoverageRunner`: jest/mocha absent → `t.Skip` via `exec.LookPath`. These run only where the toolchain exists.

### Coverage target

`internal/autotest` 54.8% → ~70%+ expected, driven by `RunCoverage` (3 langs), the surviving Python helpers, `DefaultXxxCoverageRunner` (Python real, Node/Mocha skipped-but-declared), and the still-uncovered `config.go` constructors (`DefaultNodeCoverageConfig`, `DefaultMochaCoverageConfig`, `DefaultPythonCoverageConfig`, `NodeCoverageConfig`, `PythonCoverageConfig`) which are trivial pure-function adds in the same pass.

## Behavior-equivalence guarantee

The actual shell command each Default runner executes must match what the old inline `RunCoverage` ran:

| Lang | Old inline command | New Default-runner command | Equivalent? |
|---|---|---|---|
| Node | `npm test -- --coverage --coverageReporters=json` (jest default output location) | `npm test -- --coverage --coverageReporters=json --outputCoverage=<tmp>` | yes — same jest invocation, explicit output path |
| Python | `coverage run -m pytest` (+ json report via helpers) | `coverage run -m pytest --cov-report=json:<tmp>` | yes |
| Mocha | `npm test` (+ istanbul json) | new `DefaultMochaCoverageRunner`: `npm test` (+ istanbul json to tmp) | yes (newly unified) |

Only divergence: Python D1 (SQLite fallback removed).

## Risks

- **Production code changes.** Mitigation: the pattern is mechanical and proven by `GoCoverageProvider`; parse functions are already tested and unchanged, so a mis-wired runner surfaces as a parse error in tests, not a silent regression.
- **D1 removes a capability.** Mitigation: the runner guarantees JSON, so the removed SQLite path had no trigger under the new model. Flagged for explicit user sign-off in spec review.
- **Factory signature change** touches two session call sites. Mitigation: small, mechanical; `make check` gates.

## Out of scope (recorded for later)

- Fixture tests (`test/fixtures/*`) pass but report `0 verdicts / ~0K tokens` — they never reach the AutoTest phase (phase default-off + per-package coverage accounting). Independent problem; not addressed here.
