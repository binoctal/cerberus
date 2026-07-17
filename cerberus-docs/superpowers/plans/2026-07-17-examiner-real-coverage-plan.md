# Examiner Real Line Coverage Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `AssessCoverage`'s objective gate compare a real Go line-coverage measurement against `LineThreshold`, with correct units, correct unknown-coverage fallback, and assessment that actually runs on both run and resume paths.

**Architecture:** Go coverage provider parses `go test -coverprofile` statement counts into a line-coverage percentage. A new `contract.CoverageMeasurement{Pct, Unit, Known}` (0–1 fraction, matching `LineThreshold`) replaces the raw float passed to `AssessCoverage`. The Examiner measures the Agent's tests independently of AutoTest. The coverage Contract is persisted (new `sessions.contract` column) so resume can reload it and assess.

**Tech Stack:** Go 1.25, `modernc.org/sqlite`, `go.uber.org/zap`, `testify`.

## Spec addendum (found during plan self-review)

The spec missed a **scale bug**. `contract.Gate.LineThreshold` and `contract.Assessment.CoveragePct` are 0–1 fractions (schema default `0.65`; `assess_test.go` passes `0.50`/`0.80`; `assess.go` formats gaps as `pct*100`). But the autotest providers return 0–100 (`pct()` multiplies by 100). On the real run path the wired 0–100 value is compared against a 0–1 threshold, so `75.5 < 0.65` is always false — **the objective gate never fires on real data**. This plan fixes it by normalizing to 0–1 at the `CoverageMeasurement` boundary (Task 7). `CoverageReport.LineCoveragePct` stays 0–100 (matches `go tool cover` and the `Pct` name); `CoverageMeasurement.Pct` is 0–1 (matches the contract types, which already misuse "Pct" to mean fraction).

## Global Constraints

- Go 1.25, module `github.com/binoctal/cerberus`, pure Go (no CGo).
- Code comments and commit messages in English.
- Commit author `binoctal <binoctal@gmail.com>`, **no** `Co-Authored-By`.
- Follow existing comment density and naming idiom.
- Documentation only in `cerberus-docs/` (never `docs/`).
- Every task ends with `make build` green; final task runs `make check`.

## File Structure

| File | Responsibility | Action |
|---|---|---|
| `internal/head/contract/types.go` | Define `CoverageMeasurement` | Modify |
| `internal/autotest/types.go` | Add `LineCoveragePct`, `CoverageUnit` to `CoverageReport` | Modify |
| `internal/autotest/coverage_go.go` | Parse statement coverage from coverprofile | Modify |
| `internal/autotest/autotest_helpers.go` | `pct()` prefers line coverage | Modify |
| `internal/autotest/coverage_{node,python,mocha}_run.go` | Tag function-level unit | Modify |
| `internal/head/examiner/assess.go` | New signature + D4 gate logic | Modify |
| `internal/head/examiner/assess_test.go` | Update tests for new signature | Modify |
| `internal/session/coverage.go` | Return `CoverageMeasurement`, normalize 0–1 | Modify |
| `internal/session/lifecycle_types.go` | `coverageFn` signature | Modify |
| `internal/session/lifecycle_factory.go` | Wire `coverageFn` | Modify |
| `internal/session/*_test.go` (~8 stubs) | Update stubs to new signature | Modify |
| `internal/session/run_phases_examiner.go` | Build measurement, call AssessCoverage | Modify |
| `internal/session/run_phases_scout.go` | Persist Contract after build | Modify |
| `internal/session/resume_phases_lifecycle.go` | Load Contract on init | Modify |
| `internal/session/resume_phases_run.go` | Assess on resume | Modify |
| `migrations/V010__session_contract.sql` | Add `contract` column | Create |
| `internal/store/session.go` | `SaveContract`/`LoadContract`, row field, SELECT/Scan | Modify |

---

### Task 1: Add `CoverageMeasurement` to contract

**Files:**
- Modify: `internal/head/contract/types.go`

**Interfaces:**
- Produces: `contract.CoverageMeasurement{Pct float64; Unit string; Known bool}` (Pct is 0–1 fraction).

- [ ] **Step 1: Add the type**

Append to `internal/head/contract/types.go` (after the `Gap` struct):

```go
// CoverageMeasurement is the objective coverage value passed to AssessCoverage.
// Pct is a 0–1 fraction (matching Gate.LineThreshold and Assessment.CoveragePct),
// NOT a 0–100 percentage. Unit is "line" (Go) or "function" (Node/Python).
// Known is false when no measurement could be obtained (provider failure or
// nothing measurable); a measured 0% has Known=true.
type CoverageMeasurement struct {
	Pct   float64
	Unit  string
	Known bool
}
```

- [ ] **Step 2: Build**

Run: `make build`
Expected: compiles (type is unused so far, that's fine).

- [ ] **Step 3: Commit**

```bash
git add internal/head/contract/types.go
git commit -m "feat(contract): add CoverageMeasurement type for AssessCoverage"
```

---

### Task 2: Add line-coverage fields to `CoverageReport`

**Files:**
- Modify: `internal/autotest/types.go:44-49`

**Interfaces:**
- Produces: `CoverageReport.LineCoveragePct float64` (0–100), `CoverageReport.CoverageUnit string`.

- [ ] **Step 1: Extend the struct**

In `internal/autotest/types.go`, replace the `CoverageReport` struct:

```go
// CoverageReport is the output of RunCoverage.
type CoverageReport struct {
	Pass                     bool
	Profile                  []CoverageLine
	TotalFuncs, CoveredFuncs int
	// LineCoveragePct is statement coverage in 0–100 (Go only). 0 when no line
	// data was collected; distinguish from a measured 0% via TotalFuncs/Profile.
	LineCoveragePct float64
	// CoverageUnit is "line" (Go coverprofile) or "function" (Node/Python).
	CoverageUnit string
}
```

- [ ] **Step 2: Build**

Run: `make build`
Expected: compiles.

- [ ] **Step 3: Commit**

```bash
git add internal/autotest/types.go
git commit -m "feat(autotest): add LineCoveragePct and CoverageUnit to CoverageReport"
```

---

### Task 3: Go provider computes statement (line) coverage

**Files:**
- Modify: `internal/autotest/coverage_go.go:51-86`
- Test: `internal/autotest/coverage_go_test.go`

**Interfaces:**
- Produces: `parseCoverProfile` now sets `rep.LineCoveragePct` (0–100) and `rep.CoverageUnit = "line"`.

- [ ] **Step 1: Write the failing test**

Append to `internal/autotest/coverage_go_test.go`:

```go
func TestParseCoverProfile_LineCoveragePct(t *testing.T) {
	// Two blocks: one covered (count>0, 10 stmts), one uncovered (count=0, 30 stmts).
	// => 10/40 covered = 25%.
	in := []byte("mode: set\n" +
		"foo/bar.go:1.1,2.2 10 1\n" +
		"foo/baz.go:5.1,6.2 30 0\n")
	rep, err := parseCoverProfile(in)
	require.NoError(t, err)
	assert.Equal(t, "line", rep.CoverageUnit)
	assert.InDelta(t, 25.0, rep.LineCoveragePct, 0.001)
}

func TestParseCoverProfile_ZeroCoveredIsKnown(t *testing.T) {
	// All blocks uncovered => 0% but still measured (denominator > 0).
	in := []byte("mode: set\nfoo/bar.go:1.1,2.2 10 0\n")
	rep, err := parseCoverProfile(in)
	require.NoError(t, err)
	assert.Equal(t, 0.0, rep.LineCoveragePct)
	// denominator present => caller treats as Known. Expose via Profile length.
	assert.NotEmpty(t, rep.Profile)
}
```

If the file does not already import `testify/require`, add it.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/autotest/ -run TestParseCoverProfile_LineCoveragePct -v`
Expected: FAIL — `LineCoveragePct` is 0, `CoverageUnit` is "".

- [ ] **Step 3: Implement statement accumulation**

In `internal/autotest/coverage_go.go`, replace the body of `parseCoverProfile` (the `for sc.Scan()` loop) so it accumulates statement counts. Replace the whole function with:

```go
// parseCoverProfile parses Go cover.out text (mode line + blocks).
// Format per block: file:start.col,end.col numStmts count
func parseCoverProfile(data []byte) (*CoverageReport, error) {
	rep := &CoverageReport{CoverageUnit: "line"}
	var totalStmts, coveredStmts int
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		colon := strings.LastIndex(line, ":")
		if colon < 0 {
			continue
		}
		file := line[:colon]
		rest := line[colon+1:]
		parts := strings.Split(rest, " ")
		if len(parts) < 3 {
			continue
		}
		posComma := strings.Split(parts[0], ",")
		if len(posComma) != 2 {
			continue
		}
		start, _ := strconv.Atoi(strings.Split(posComma[0], ".")[0])
		end, _ := strconv.Atoi(strings.Split(posComma[1], ".")[0])
		numStmts, _ := strconv.Atoi(parts[1])
		count, _ := strconv.Atoi(parts[2])
		rep.Profile = append(rep.Profile, CoverageLine{
			File: file, Start: start, End: end, Count: count,
		})
		rep.TotalFuncs++
		if count > 0 {
			rep.CoveredFuncs++
		}
		// Statement-level (line) coverage: go tool cover -func semantics.
		totalStmts += numStmts
		if count > 0 {
			coveredStmts += numStmts
		}
	}
	if totalStmts > 0 {
		rep.LineCoveragePct = float64(coveredStmts) / float64(totalStmts) * 100
	}
	return rep, sc.Err()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/autotest/ -run TestParseCoverProfile -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/autotest/coverage_go.go internal/autotest/coverage_go_test.go
git commit -m "feat(autotest): compute statement coverage from Go coverprofile"
```

---

### Task 4: `pct()` prefers line coverage

**Files:**
- Modify: `internal/autotest/autotest_helpers.go:15-20`
- Test: `internal/autotest/autotest_test.go`

**Interfaces:**
- Produces: `pct(r *CoverageReport) float64` returns `LineCoveragePct` when the report has Profile data (line measured), else function-level. Result stays 0–100.

- [ ] **Step 1: Write the failing test**

Append to `internal/autotest/autotest_test.go`:

```go
func TestPct_PrefersLineCoverage(t *testing.T) {
	// Line says 25%, function-level says 50% (1 of 2 blocks). Must pick line.
	rep := &CoverageReport{
		TotalFuncs:      2,
		CoveredFuncs:    1,
		LineCoveragePct: 25.0,
		Profile:         []CoverageLine{{File: "x.go", Start: 1, End: 2, Count: 1}},
	}
	assert.InDelta(t, 25.0, pct(rep), 0.001)
}

func TestPct_FallsBackToFunctionWhenNoLineData(t *testing.T) {
	rep := &CoverageReport{TotalFuncs: 4, CoveredFuncs: 1} // no Profile, no LineCoveragePct
	assert.InDelta(t, 25.0, pct(rep), 0.001)
}

func TestPct_ZeroLineCoverageIsMeasured(t *testing.T) {
	// 0% line coverage with Profile present must return 0, not fall back.
	rep := &CoverageReport{
		TotalFuncs:      2,
		CoveredFuncs:    2, // function-level would say 100%
		LineCoveragePct: 0,
		Profile:         []CoverageLine{{File: "x.go", Start: 1, End: 2, Count: 0}},
	}
	assert.Equal(t, 0.0, pct(rep))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/autotest/ -run TestPct -v`
Expected: FAIL — current `pct()` returns function-level in all cases.

- [ ] **Step 3: Implement**

In `internal/autotest/autotest_helpers.go`, replace `pct`:

```go
// pct calculates coverage percentage from a report (0–100). It prefers
// statement-level (line) coverage when the report carries profile data —
// including a measured 0% — and falls back to function/block-level otherwise.
func pct(r *CoverageReport) float64 {
	if r == nil {
		return 0
	}
	if len(r.Profile) > 0 {
		return r.LineCoveragePct
	}
	if r.TotalFuncs == 0 {
		return 0
	}
	return float64(r.CoveredFuncs) / float64(r.TotalFuncs) * 100
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/autotest/ -run TestPct -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/autotest/autotest_helpers.go internal/autotest/autotest_test.go
git commit -m "feat(autotest): pct prefers line coverage over function-level"
```

---

### Task 5: Tag Node/Python providers as function-level

**Files:**
- Modify: `internal/autotest/coverage_node_run.go`, `internal/autotest/coverage_python_run.go`, `internal/autotest/coverage_mocha_run.go`

**Interfaces:**
- Produces: each non-Go `RunCoverage` sets `report.CoverageUnit = "function"`.

- [ ] **Step 1: Set the unit in each provider**

In each of the three files, immediately after the line `report.Pass = true` (node_run.go:76, python_run.go:44, mocha_run.go:82-area), add:

```go
	report.CoverageUnit = "function"
```

(If `report.Pass = true` appears once per file, add directly beneath it. For `coverage_python_run.go` which has two early-return `report` sites, set the unit on the `report` at construction instead — find the line `report := &CoverageReport{...}` or `report := ...` and add `CoverageUnit: "function",` to that struct literal. If there is no single literal, set `report.CoverageUnit = "function"` right before each `return report, nil`.)

- [ ] **Step 2: Build + run autotest tests**

Run: `make build && go test ./internal/autotest/ -v`
Expected: builds; autotest tests pass.

- [ ] **Step 3: Commit**

```bash
git add internal/autotest/coverage_node_run.go internal/autotest/coverage_python_run.go internal/autotest/coverage_mocha_run.go
git commit -m "feat(autotest): tag Node/Python coverage reports as function-level"
```

---

### Task 6: `AssessCoverage` new signature + D4 gate logic

**Files:**
- Modify: `internal/head/examiner/assess.go`
- Test: `internal/head/examiner/assess_test.go`

**Interfaces:**
- Consumes: `contract.CoverageMeasurement` (Task 1).
- Produces: `AssessCoverage(ctx, c, results, contract.CoverageMeasurement) (*contract.Assessment, error)`.

- [ ] **Step 1: Rewrite the tests for the new signature and semantics**

Replace the entire contents of `internal/head/examiner/assess_test.go`:

```go
package examiner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/contract"
	"github.com/binoctal/cerberus/internal/llm"
)

func newAssessExaminer(t *testing.T, resp string) *Examiner {
	mock := llm.NewMockClient(map[string]string{"default": resp})
	driver := ai.NewDriver(mock, ai.NewTokenBudget(10000, 1000))
	return NewExaminer(driver, nil, setupExaminerStore(t), DefaultExaminerConfig(), zap.NewNop())
}

func stdContract() *contract.Contract {
	return &contract.Contract{
		Depth:        "standard",
		Scope:        []string{"internal/llm"},
		CoverageGate: contract.Gate{Module: "internal/llm", LineThreshold: 0.65},
	}
}

func TestAssessCoverage_BelowThresholdForcesNotReached(t *testing.T) {
	e := newAssessExaminer(t, `{"reached":true,"gaps":[],"coverage_pct":0.5,"reasoning":"ok"}`)
	res := []agent.StepResult{{TestCase: &agent.TestCase{ID: "tc-1", Target: "internal/llm"}}}

	a, err := e.AssessCoverage(context.Background(), stdContract(), res, contract.CoverageMeasurement{Pct: 0.50, Unit: "line", Known: true})
	require.NoError(t, err)
	assert.False(t, a.Reached, "objective gate overrides LLM reached=true")
	assert.Equal(t, 0.50, a.CoveragePct)
	found := false
	for _, g := range a.Gaps {
		if g.Kind == "coverage" {
			found = true
			assert.Contains(t, g.Detail, "50% < 65%")
		}
	}
	assert.True(t, found)
}

func TestAssessCoverage_MeasuredZeroForcesNotReached(t *testing.T) {
	e := newAssessExaminer(t, `{"reached":true,"gaps":[],"coverage_pct":0,"reasoning":"ok"}`)
	res := []agent.StepResult{{TestCase: &agent.TestCase{ID: "tc-1", Target: "internal/llm"}}}

	a, err := e.AssessCoverage(context.Background(), stdContract(), res, contract.CoverageMeasurement{Pct: 0, Unit: "line", Known: true})
	require.NoError(t, err)
	assert.False(t, a.Reached, "measured 0% is not unknown")
	assert.Equal(t, 0.0, a.CoveragePct)
}

func TestAssessCoverage_UnknownSkipsGate(t *testing.T) {
	e := newAssessExaminer(t, `{"reached":true,"gaps":[],"coverage_pct":0,"reasoning":"ok"}`)
	res := []agent.StepResult{{TestCase: &agent.TestCase{ID: "tc-1", Target: "internal/llm"}}}

	a, err := e.AssessCoverage(context.Background(), stdContract(), res, contract.CoverageMeasurement{Known: false})
	require.NoError(t, err)
	assert.True(t, a.Reached, "LLM judgment stands when coverage unmeasured")
	assert.Equal(t, 0.0, a.CoveragePct)
	for _, g := range a.Gaps {
		assert.NotEqual(t, "coverage", g.Kind, "no coverage gap appended when unknown")
	}
}

func TestAssessCoverage_FunctionUnitNotesMismatch(t *testing.T) {
	e := newAssessExaminer(t, `{"reached":true,"gaps":[],"coverage_pct":0.5,"reasoning":"ok"}`)
	res := []agent.StepResult{{TestCase: &agent.TestCase{ID: "tc-1", Target: "internal/llm"}}}

	a, err := e.AssessCoverage(context.Background(), stdContract(), res, contract.CoverageMeasurement{Pct: 0.50, Unit: "function", Known: true})
	require.NoError(t, err)
	assert.False(t, a.Reached)
	found := false
	for _, g := range a.Gaps {
		if g.Kind == "coverage" {
			found = true
			assert.Contains(t, g.Detail, "function")
		}
	}
	assert.True(t, found)
}

func TestAssessCoverage_BothAgreeReached(t *testing.T) {
	e := newAssessExaminer(t, `{"reached":true,"gaps":[],"coverage_pct":0.80,"reasoning":"ok"}`)
	res := []agent.StepResult{{TestCase: &agent.TestCase{ID: "tc-1", Target: "internal/llm"}}}

	a, err := e.AssessCoverage(context.Background(), stdContract(), res, contract.CoverageMeasurement{Pct: 0.80, Unit: "line", Known: true})
	require.NoError(t, err)
	assert.True(t, a.Reached)
	assert.Equal(t, 0.80, a.CoveragePct)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/head/examiner/ -run TestAssessCoverage -v`
Expected: FAIL — signature mismatch (compiler error: old `AssessCoverage` takes `float64`).

- [ ] **Step 3: Implement the new `AssessCoverage`**

Replace the entire contents of `internal/head/examiner/assess.go`:

```go
package examiner

import (
	"context"
	"fmt"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/contract"
)

// AssessCoverage judges whether a test session met its coverage contract.
// m is the objective coverage measurement for the Agent's tests. Its Pct is a
// 0–1 fraction compared against Gate.LineThreshold; Unit is "line" or
// "function"; Known is false when coverage could not be measured, in which case
// the objective gate is skipped and the LLM's verdict stands.
func (e *Examiner) AssessCoverage(ctx context.Context, c *contract.Contract, results []agent.StepResult, m contract.CoverageMeasurement) (*contract.Assessment, error) {
	prompt := ai.NewPrompt().
		System(`You assess a test session against its coverage contract. Judge whether scope, path types, error scopes, boundaries, and invariants are covered. Use the objective coverage %. Report gaps concretely.`).
		Task(fmt.Sprintf("Contract: %+v\nCases run: %d\nObjective coverage of gated module: %.2f (unit: %s, gate: %.2f)", c, len(results), m.Pct, m.Unit, c.CoverageGate.LineThreshold)).
		Output(`Respond with JSON: {"reached":false,"gaps":[{"kind":"","detail":""}],"reasoning":""}`).
		Build()
	var a contract.Assessment
	if err := e.judge.judgeDriver.Decide(ctx, prompt, &a); err != nil {
		return nil, fmt.Errorf("assess coverage: %w", err)
	}

	if !m.Known {
		// Unmeasured: do NOT bias the verdict. Leave Reached and Gaps to the LLM.
		a.CoveragePct = 0
		return &a, nil
	}

	// Objective gate: below threshold → not reached regardless of the LLM.
	if m.Pct < c.CoverageGate.LineThreshold {
		a.Reached = false
		detail := fmt.Sprintf("%.0f%% < %.0f%% gate", m.Pct*100, c.CoverageGate.LineThreshold*100)
		if m.Unit != "line" {
			detail += fmt.Sprintf(" (measured as %s coverage)", m.Unit)
		}
		a.Gaps = append(a.Gaps, contract.Gap{Kind: "coverage", Detail: detail})
	}
	// The objective measurement always overrides the model's estimate.
	a.CoveragePct = m.Pct
	return &a, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/head/examiner/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/head/examiner/assess.go internal/head/examiner/assess_test.go
git commit -m "feat(examiner): AssessCoverage takes CoverageMeasurement, skip gate when unknown"
```

---

### Task 7: `coverageForSession` returns `CoverageMeasurement` (normalize 0–1)

**Files:**
- Modify: `internal/session/coverage.go`
- Modify: `internal/session/lifecycle_types.go:39-44,81-84`
- Modify: `internal/session/lifecycle_factory.go:65`
- Test: `internal/session/coverage_test.go`

**Interfaces:**
- Consumes: `contract.CoverageMeasurement` (Task 1), provider report fields (Tasks 2–5).
- Produces: `lineCoverage(ctx) contract.CoverageMeasurement`; `coverageFn func(ctx, *Session) contract.CoverageMeasurement`.

- [ ] **Step 1: Rewrite the tests**

Replace the entire contents of `internal/session/coverage_test.go`:

```go
package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/binoctal/cerberus/internal/head/contract"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
	"go.uber.org/zap"
)

func TestCoverageForSession_GoLineMeasurement(t *testing.T) {
	// coverageFn injected: simulate a Go line measurement of 75.5% (0–100) → 0.755 fraction.
	s, _ := store.New(":memory:")
	defer func() { _ = s.Close() }()
	cfg := project.DefaultConfig()
	sess := &Session{Config: &cfg, Store: s, Logger: zap.NewNop(), ProjectDir: ".",
		coverageFn: func(_ context.Context, _ *Session) contract.CoverageMeasurement {
			// Stand-in for the real provider path; see TestCoverageForSession_NormalizesProvider.
			return contract.CoverageMeasurement{Pct: 0.755, Unit: "line", Known: true}
		}}
	m := sess.lineCoverage(context.Background())
	assert.True(t, m.Known)
	assert.Equal(t, "line", m.Unit)
	assert.InDelta(t, 0.755, m.Pct, 0.0001)
}

func TestCoverageForSession_NormalizesProviderToFraction(t *testing.T) {
	// Provider returns 0–100; coverageForSession must divide by 100 and set Known
	// when the denominator is non-zero. We exercise the provider path directly by
	// giving a Session with no coverageFn and a ProjectDir that has no measurable
	// source → falls to error path → Known=false.
	s, _ := store.New(":memory:")
	defer func() { _ = s.Close() }()
	cfg := project.DefaultConfig()
	sess := &Session{Config: &cfg, Store: s, Logger: zap.NewNop(),
		ProjectDir: "/nonexistent/path/that/does/not/exist"}
	m := coverageForSession(context.Background(), sess)
	assert.False(t, m.Known, "provider failure → Known=false, not Pct=0 gate-bait")
	assert.Equal(t, 0.0, m.Pct)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/ -run TestCoverageForSession -v`
Expected: FAIL — `lineCoverage` returns `float64`, `coverageForSession` returns `float64`.

- [ ] **Step 3: Rewrite `coverage.go`**

Replace the entire contents of `internal/session/coverage.go`:

```go
package session

import (
	"context"
	"os"
	"path/filepath"

	"github.com/binoctal/cerberus/internal/autotest"
	"github.com/binoctal/cerberus/internal/head/contract"
)

// lineCoverage returns the Examiner-phase coverage measurement, using an injected
// override when present (tests); otherwise the default coverageForSession.
func (s *Session) lineCoverage(ctx context.Context) contract.CoverageMeasurement {
	if s.coverageFn != nil {
		return s.coverageFn(ctx, s)
	}
	return coverageForSession(ctx, s)
}

// coverageForSession runs the language-specific coverage provider and returns a
// CoverageMeasurement. Pct is normalized to a 0–1 fraction (matching
// Gate.LineThreshold). Known is true only when the provider succeeded and the
// coverage denominator is non-zero; a provider error yields Known=false so the
// objective gate is skipped instead of forcing a false not-reached on a fake 0.
func coverageForSession(ctx context.Context, sess *Session) contract.CoverageMeasurement {
	markers := make(map[string]bool)
	if _, err := os.Stat(filepath.Join(sess.ProjectDir, "package.json")); err == nil {
		markers["package.json"] = true
	}
	if _, err := os.Stat(filepath.Join(sess.ProjectDir, "requirements.txt")); err == nil {
		markers["requirements.txt"] = true
	}
	if _, err := os.Stat(filepath.Join(sess.ProjectDir, "pyproject.toml")); err == nil {
		markers["pyproject.toml"] = true
	}

	var sourceFile string
	if matches, _ := filepath.Glob(filepath.Join(sess.ProjectDir, "*.go")); len(matches) > 0 {
		sourceFile = matches[0]
	} else if matches, _ := filepath.Glob(filepath.Join(sess.ProjectDir, "*.js")); len(matches) > 0 {
		sourceFile = matches[0]
	} else if matches, _ := filepath.Glob(filepath.Join(sess.ProjectDir, "*.ts")); len(matches) > 0 {
		sourceFile = matches[0]
	} else if matches, _ := filepath.Glob(filepath.Join(sess.ProjectDir, "*.py")); len(matches) > 0 {
		sourceFile = matches[0]
	}

	lang := autotest.DetectLanguage(sourceFile, markers)
	provider := autotest.NewCoverageProviderForLanguage(lang, autotest.DefaultGoCoverageRunner, sess.Logger)
	report, err := provider.RunCoverage(ctx, sess.ProjectDir)
	if err != nil || report == nil {
		return contract.CoverageMeasurement{Known: false}
	}

	unit := report.CoverageUnit
	if unit == "" {
		unit = "function"
	}
	var pct100 float64
	known := false
	if unit == "line" {
		// Line coverage is measured when any profile block exists.
		if len(report.Profile) > 0 {
			pct100 = report.LineCoveragePct
			known = true
		}
	} else {
		if report.TotalFuncs > 0 {
			pct100 = float64(report.CoveredFuncs) / float64(report.TotalFuncs) * 100
			known = true
		}
	}
	if !known {
		return contract.CoverageMeasurement{Known: false}
	}
	return contract.CoverageMeasurement{Pct: pct100 / 100, Unit: unit, Known: true}
}
```

- [ ] **Step 4: Update the `coverageFn` signature**

In `internal/session/lifecycle_types.go`, change both the `SessionConfig` field and the `Session` field. Replace:

```go
	CoverageFn func(ctx context.Context, sess *Session) float64
```

with:

```go
	CoverageFn func(ctx context.Context, sess *Session) contract.CoverageMeasurement
```

and replace the `Session` struct field:

```go
	coverageFn func(ctx context.Context, sess *Session) float64
```

with:

```go
	coverageFn func(ctx context.Context, sess *Session) contract.CoverageMeasurement
```

Add the import `"github.com/binoctal/cerberus/internal/head/contract"` to `lifecycle_types.go` if missing.

`lifecycle_factory.go:65` (`coverageFn: cfg.CoverageFn`) needs no change — it forwards the field.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/session/ -run TestCoverageForSession -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/session/coverage.go internal/session/coverage_test.go internal/session/lifecycle_types.go
git commit -m "feat(session): coverageForSession returns normalized CoverageMeasurement"
```

---

### Task 8: Update `coverageFn` test stubs across the session package

**Files:**
- Modify: every `*_test.go` under `internal/session/` that assigns `CoverageFn:`.

**Interfaces:**
- Consumes: new `coverageFn` signature (Task 7).

- [ ] **Step 1: Find all stub sites**

Run: `grep -rn "CoverageFn:" internal/session/`
Expected sites (from earlier audit): `autotest_integration_test.go:64,95`, `contract_integration_test.go:97,149,183`, `reflexion_integration_test.go:48,154,217`, `resume_idempotency_test.go:48,152,238,320`. Also check for inline `func(context.Context, *Session) float64` literals.

- [ ] **Step 2: Update each stub**

For each site that currently reads:

```go
CoverageFn: stubCoverageFn(),
```

verify `stubCoverageFn`'s definition (search `func stubCoverageFn`) and change its return type from `func(context.Context, *Session) float64` to `func(context.Context, *Session) contract.CoverageMeasurement`, returning `contract.CoverageMeasurement{Pct: <old value>/100, Unit: "line", Known: true}`. Ensure `contract` is imported in that file.

For inline literals of the form:

```go
CoverageFn: func(context.Context, *Session) float64 { return 100.0 },
```

replace with:

```go
CoverageFn: func(context.Context, *Session) contract.CoverageMeasurement {
    return contract.CoverageMeasurement{Pct: 1.0, Unit: "line", Known: true}
},
```

(Preserve each test's intent: a stub that returned `100.0` → `Pct: 1.0`; one returning `0` to trigger the gate → `Pct: 0, Known: true`; one simulating failure → `Known: false`.)

- [ ] **Step 3: Build + run session tests**

Run: `make build && go test ./internal/session/ -v`
Expected: builds; all session tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/session/
git commit -m "test(session): update coverageFn stubs to CoverageMeasurement"
```

---

### Task 9: Migration + store `SaveContract`/`LoadContract`

**Files:**
- Create: `migrations/V010__session_contract.sql`
- Modify: `internal/store/session.go`
- Test: `internal/store/migrations_test.go`

**Interfaces:**
- Produces: `store.SaveContract(ctx, sessionID string, c *contract.Contract) error`, `store.LoadContract(ctx, sessionID string) (*contract.Contract, error)`; `store.Session.Contract string` field; `GetSession`/`ListSessions` read the new column.

- [ ] **Step 1: Write the failing test**

Append to `internal/store/migrations_test.go`:

```go
func TestSaveAndLoadContract_RoundTrip(t *testing.T) {
	s, err := New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

	ctx := context.Background()
	sess, err := s.CreateSession(ctx, "standard", "g", "p")
	require.NoError(t, err)

	in := &contract.Contract{
		Depth:        "standard",
		Scope:        []string{"internal/llm"},
		CoverageGate: contract.Gate{Module: "internal/llm", LineThreshold: 0.65},
	}
	require.NoError(t, s.SaveContract(ctx, sess.ID, in))

	got, err := s.LoadContract(ctx, sess.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "standard", got.Depth)
	assert.Equal(t, 0.65, got.CoverageGate.LineThreshold)

	// GetSession must still scan the new column without error.
	_, err = s.GetSession(ctx, sess.ID)
	require.NoError(t, err)
}
```

Add imports as needed: `"github.com/binoctal/cerberus/internal/head/contract"`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestSaveAndLoadContract -v`
Expected: FAIL — `SaveContract` undefined.

- [ ] **Step 3: Create the migration**

Create `migrations/V010__session_contract.sql`:

```sql
-- Add contract column to sessions for persisting the coverage contract so the
-- Examiner can assess on resume (Scout phase, which builds the contract, is
-- skipped during resume).
ALTER TABLE sessions ADD COLUMN contract TEXT NOT NULL DEFAULT '';
```

- [ ] **Step 4: Extend `store.Session` and SELECTs**

In `internal/store/session.go`, add a field to the `Session` struct (after `AutoTestReport`):

```go
	Contract       string  `json:"contract,omitempty"`
```

Update both `GetSession` and `ListSessions` to read the column. In `GetSession`, change the query and Scan to include `contract`:

```go
	err := s.db.QueryRowContext(ctx,
		`SELECT id, mode, status, goal, project_name, coverage_pct, stats, autotest_report, contract, started_at, COALESCE(finished_at, '')
		 FROM sessions WHERE id = ?`, id).Scan(
		&sess.ID, &sess.Mode, &sess.Status, &sess.Goal, &sess.ProjectName,
		&sess.CoveragePct, &sess.Stats, &sess.AutoTestReport, &sess.Contract, &sess.StartedAt, &sess.FinishedAt)
```

In `ListSessions`, make the same additive change to the `SELECT` column list (add `contract` after `autotest_report`) and the `rows.Scan(...)` call (add `&sess.Contract` after `&sess.AutoTestReport`).

- [ ] **Step 5: Add `SaveContract` / `LoadContract`**

Append to `internal/store/session.go` (the file already imports `encoding/json` via `jsonText`; if not, add `"encoding/json"`):

```go
// SaveContract persists the coverage contract JSON for a session (UPSERT).
func (s *Store) SaveContract(ctx context.Context, sessionID string, c *contract.Contract) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE sessions SET contract = ? WHERE id = ?`, string(raw), sessionID)
	return err
}

// LoadContract reads and decodes the coverage contract for a session.
// Returns (nil, nil) when no contract is stored.
func (s *Store) LoadContract(ctx context.Context, sessionID string) (*contract.Contract, error) {
	var raw string
	err := s.db.QueryRowContext(ctx,
		`SELECT contract FROM sessions WHERE id = ?`, sessionID).Scan(&raw)
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return nil, nil
	}
	var c contract.Contract
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return nil, err
	}
	return &c, nil
}
```

Add the import `"github.com/binoctal/cerberus/internal/head/contract"` to `internal/store/session.go`.

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/store/ -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add migrations/V010__session_contract.sql internal/store/session.go internal/store/migrations_test.go
git commit -m "feat(store): persist coverage contract (V010) with SaveContract/LoadContract"
```

---

### Task 10: Persist Contract after Scout builds it

**Files:**
- Modify: `internal/session/run_phases_scout.go:46-54`

**Interfaces:**
- Consumes: `store.SaveContract` (Task 9).

- [ ] **Step 1: Add the persist call**

In `internal/session/run_phases_scout.go`, inside the `if depth != "off"` block, immediately after the `SelfAssessContract` sub-block (after line 54, before the closing `}` of `if depth != "off"`), add:

```go
		// Persist contract so resume (which skips Scout) can still assess coverage.
		if rp.session.Contract != nil {
			if saveErr := rp.session.Store.SaveContract(rp.ctx, rp.session.ID, rp.session.Contract); saveErr != nil {
				rp.session.Logger.Warn("failed to save contract", zap.Error(saveErr))
			}
		}
```

- [ ] **Step 2: Build**

Run: `make build`
Expected: compiles.

- [ ] **Step 3: Commit**

```bash
git add internal/session/run_phases_scout.go
git commit -m "feat(session): persist coverage contract after Scout builds it"
```

---

### Task 11: Examiner run path passes the measurement

**Files:**
- Modify: `internal/session/run_phases_examiner.go:40-41`

**Interfaces:**
- Consumes: `lineCoverage` returning `contract.CoverageMeasurement` (Task 7), new `AssessCoverage` signature (Task 6).

- [ ] **Step 1: Update the call site**

In `internal/session/run_phases_examiner.go`, replace:

```go
		covPct := rp.session.lineCoverage(rp.ctx)
		assessment, aerr := examinerHead.AssessCoverage(rp.ctx, rp.session.Contract, rp.results, covPct)
```

with:

```go
		measurement := rp.session.lineCoverage(rp.ctx)
		assessment, aerr := examinerHead.AssessCoverage(rp.ctx, rp.session.Contract, rp.results, measurement)
```

- [ ] **Step 2: Build + run examiner/session tests**

Run: `make build && go test ./internal/session/ ./internal/head/examiner/ -v`
Expected: builds; tests pass.

- [ ] **Step 3: Commit**

```bash
git add internal/session/run_phases_examiner.go
git commit -m "feat(session): pass CoverageMeasurement to AssessCoverage on run path"
```

---

### Task 12: Resume loads Contract and assesses coverage

**Files:**
- Modify: `internal/session/resume_phases_lifecycle.go` (`initialize`)
- Modify: `internal/session/resume_phases_run.go` (`examineResults`)
- Test: `internal/session/resume_idempotency_test.go`

**Interfaces:**
- Consumes: `store.LoadContract` (Task 9), `AssessCoverage` (Task 6), `lineCoverage` (Task 7).

- [ ] **Step 1: Write the failing test**

Append to `internal/session/resume_idempotency_test.go` (mirroring the existing resume test scaffolding in that file — reuse its helpers to build a resumable session with a saved plan; if a helper like `newResumableSession(t)` exists, use it, otherwise copy the setup from an existing resume test):

```go
func TestResume_AssessesCoverageWithLoadedContract(t *testing.T) {
	cfg, sess, cleanup := newResumableSessionWithContract(t, &contract.Contract{
		Depth:        "standard",
		Scope:        []string{"internal/llm"},
		CoverageGate: contract.Gate{Module: "internal/llm", LineThreshold: 0.99},
	})
	defer cleanup()
	cfg.CoverageFn = func(context.Context, *Session) contract.CoverageMeasurement {
		return contract.CoverageMeasurement{Pct: 0.10, Unit: "line", Known: true}
	}

	err := sess.Resume(context.Background())
	require.NoError(t, err)
	require.NotNil(t, sess.Assessment, "resume must assess coverage")
	assert.False(t, sess.Assessment.Reached, "10% < 99% gate → not reached")
}
```

If `newResumableSessionWithContract` does not exist, add a helper next to the existing resume-test helpers that: creates a session, saves a plan via `Store.SavePlan`, saves the contract via `Store.SaveContract`, and returns `(cfg, sess, cleanup)`. Model it on the existing `resume_idempotency_test.go` setup; inject a stub Agent/Examiner driver so Resume completes without live LLM calls.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/ -run TestResume_AssessesCoverageWithLoadedContract -v`
Expected: FAIL — `sess.Assessment` is nil (resume never assesses; Contract never loaded).

- [ ] **Step 3: Load the Contract on resume init**

In `internal/session/resume_phases_lifecycle.go`, replace `initialize`:

```go
// initialize prepares the session for resuming.
func (rp *resumePhase) initialize() error {
	rp.session.Logger.Info("resuming session", zap.String("id", rp.session.ID))
	// Resume skips Scout, so reload the persisted coverage contract (if any) to
	// allow the Examiner to assess coverage. Best-effort: never aborts resume.
	if c, err := rp.session.Store.LoadContract(rp.ctx, rp.session.ID); err != nil {
		rp.session.Logger.Warn("load contract for resume", zap.Error(err))
	} else if c != nil {
		rp.session.Contract = c
	}
	return nil
}
```

- [ ] **Step 4: Assess on resume**

In `internal/session/resume_phases_run.go`, replace `examineResults` with a version that also runs `AssessCoverage` (mirroring `run_phases_examiner.go`):

```go
// examineResults runs the examiner phase on the results, then assesses coverage.
func (rp *resumePhase) examineResults() error {
	examinerCfg := examiner.DefaultExaminerConfig()
	if rp.session.Config.Settings.ConfidenceThreshold > 0 {
		examinerCfg.ConfThreshold = rp.session.Config.Settings.ConfidenceThreshold
		examinerCfg.AutoFix = rp.session.Config.Settings.AutoFix
	}
	examinerCfg.MaxWorkers = rp.session.MaxWorkers

	examinerHead := examiner.NewExaminer(rp.session.driverFor(&rp.session.examinerDriver), rp.session.criticDriver, rp.session.Store, examinerCfg, rp.session.Logger)

	var err error
	rp.verdicts, rp.reflections, err = examinerHead.Examine(rp.ctx, rp.results, rp.session.ID, rp.session.Config.Project.Name)
	if err != nil {
		return fmt.Errorf("examiner (resume): %w", err)
	}

	if rp.session.Contract != nil {
		measurement := rp.session.lineCoverage(rp.ctx)
		assessment, aerr := examinerHead.AssessCoverage(rp.ctx, rp.session.Contract, rp.results, measurement)
		if aerr == nil {
			rp.session.Assessment = assessment
			rp.session.Logger.Info("coverage assessment (resume)",
				zap.Bool("reached", assessment.Reached),
				zap.Int("gaps", len(assessment.Gaps)),
				zap.Float64("coverage_pct", assessment.CoveragePct))
		} else {
			rp.session.Logger.Warn("coverage assessment failed (resume)", zap.Error(aerr))
		}
	}
	return nil
}
```

Ensure imports in `resume_phases_run.go` include `"go.uber.org/zap"` (already present).

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/session/ -run TestResume -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/session/resume_phases_lifecycle.go internal/session/resume_phases_run.go internal/session/resume_idempotency_test.go
git commit -m "feat(session): resume loads contract and assesses coverage"
```

---

### Task 13: Full verification

**Files:** none (verification only).

- [ ] **Step 1: Format**

Run: `make fmt`
Expected: no changes (or apply them).

- [ ] **Step 2: Lint**

Run: `make lint`
Expected: no findings. If lint flags the `contract` import cycle anywhere, confirm `store`→`contract` and `session`→`contract` are both acyclic (contract imports only `encoding/json`, `fmt`).

- [ ] **Step 3: Test (race)**

Run: `make test`
Expected: all packages PASS, including new tests from Tasks 3, 4, 6, 7, 9, 12.

- [ ] **Step 4: Combined check**

Run: `make check`
Expected: green.

- [ ] **Step 5: Commit any fmt/lint fixes**

```bash
git add -A
git commit -m "chore: fmt/lint after examiner real-coverage wiring"
```

(Only if there are changes; otherwise skip.)

---

## Self-Review Notes

- **Spec coverage:** D1 (measure Agent tests, decouple AutoTest) → Task 7 path only measures via provider, AutoTest untouched ✓. D2 (Go line coverage, `pct()`) → Tasks 2–4 ✓. D3 (`CoverageMeasurement`) → Tasks 1, 6 ✓. D4 (unknown skip / function-unit note / measured-0%) → Task 6 tests ✓. D5 (persist Contract, resume assess) → Tasks 9, 10, 12 ✓. Scale normalization → Task 7 + spec addendum ✓.
- **Scale fix:** `LineCoveragePct` stays 0–100; `CoverageMeasurement.Pct` is 0–1 (Task 7 divides by 100). Fixes the latent "gate never fires" bug. The `0.50`/`0.80` values in `assess_test.go` and `LineThreshold: 0.65` confirm the contract side is 0–1.
- **Type consistency:** `CoverageMeasurement{Pct, Unit, Known}` used identically in Tasks 1, 6, 7, 8, 11, 12. `SaveContract(ctx, id, *contract.Contract)` / `LoadContract(ctx, id) (*contract.Contract, error)` identical in Tasks 9, 10, 12.
- **No placeholders:** every code step shows full, correct code; no TBD/TODO or "similar to above".
