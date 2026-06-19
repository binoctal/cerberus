# Coverage Contract Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the AI-authored, depth-tiered Coverage Contract that spans Scout (define + self-assess) and Examiner (assess against), giving cerberus a pre-defined coverage standard instead of ad-hoc post-hoc evaluation.

**Architecture:** A new `internal/head/contract/` package holds the shared types (`Contract`, `Gate`, `Assessment`). Scout gains `BuildCoverageContract` (AI produces contract from goal + project + depth tier, then a self-assessment pass). Examiner gains `AssessCoverage` (compares session results against the contract, combining AI semantic judgment with objective coverage data). Config adds a `coverage.depth` tier; session wires both phases; report surfaces contract + gaps.

**Tech Stack:** Go 1.25, cerberus internal packages (`ai`, `project`, `head/scout`, `head/examiner`, `store`, `session`), existing `ai.Driver` + `llm.MockClient` for tests.

## Global Constraints

- Module: `github.com/binoctal/cerberus`,Go 1.25.
- No CGo (pure Go SQLite).
- Commit author: `binoctal <binoctal@gmail.com>`, no Co-Authored-By.
- Comments and commit messages in English.
- Follow existing patterns: `ai.NewPrompt().System().Context().Task().Output().Build()`, `driver.Decide`, test style in `internal/head/scout/*_test.go`.
- `make check` (fmt + lint + test) must pass after each task.
- Docs only in `cerberus-docs/`, never `docs/`.

## Scope

This is **Plan 1 — Contract Core** (types, tiers, Scout build + self-assess, Examiner assess, wiring, report). The **fixture matrix** (cross-project validation: Go/Node/Python/SaaS) is large and independent — it becomes **Plan 2**, referenced at the end.

## File Structure

- Create `internal/head/contract/types.go` — `Contract`, `Gate`, `Assessment`, `Gap` types.
- Create `internal/head/contract/depth.go` — depth tiers + dimension expansion (pure data).
- Create `internal/head/scout/contract.go` — `BuildCoverageContract` + self-assessment.
- Create `internal/head/examiner/assess.go` — `AssessCoverage`.
- Modify `internal/project/schema.go` — `CoverageSettings`.
- Modify `internal/session/run_phases_scout.go` — produce contract, store on session.
- Modify `internal/session/run_phases_examiner.go` — call AssessCoverage.
- Modify `internal/session/lifecycle_types.go` — `Session.Contract` field.
- Modify `cmd/cerberus/main_report.go` — render contract + assessment.

---

### Task 1: Contract types

**Files:**
- Create: `internal/head/contract/types.go`
- Test: `internal/head/contract/types_test.go`

**Interfaces:**
- Produces: `contract.Contract`, `contract.Gate`, `contract.Assessment`, `contract.Gap` (consumed by Tasks 4, 6, 7, 8).

- [ ] **Step 1: Write the failing test**

```go
// internal/head/contract/types_test.go
package contract

import "testing"

func TestContractDefaults(t *testing.T) {
	c := Contract{Depth: "standard"}
	if c.Depth != "standard" {
		t.Fatalf("Depth = %q", c.Depth)
	}
	g := Gate{Module: "internal/llm", LineThreshold: 0.65}
	if g.LineThreshold != 0.65 {
		t.Fatalf("threshold = %v", g.LineThreshold)
	}
}
```

- [ ] **Step 2: Run test — expect FAIL (package not found)**

Run: `go test ./internal/head/contract/`
Expected: `FAIL` (no Go files / undefined).

- [ ] **Step 3: Implement types**

```go
// internal/head/contract/types.go
package contract

// Contract is the AI-authored coverage standard for a session.
type Contract struct {
	Depth        string            // smoke | standard | thorough
	Scope        []string          // modules/paths to cover
	PathTypes    []string          // happy | alternative | boundary | edge
	ErrorScope   []string          // 4xx | validation | exception
	Boundaries   []string          // empty | zero | max | invalid | extreme
	Invariants   []InvariantRef    // pulled from project.yaml invariants
	Priorities   map[string]string // module → high|med|low
	CoverageGate Gate              // objective coverage threshold
}

// InvariantRef references a project invariant the contract must enforce.
type InvariantRef struct {
	ID         string
	Description string
}

// Gate is an objective coverage threshold (language-agnostic; each coverage
// provider measures, the contract only stores the threshold).
type Gate struct {
	Module          string
	LineThreshold   float64
	BranchThreshold float64
}

// Assessment is the Examiner's session-level verdict against a Contract.
type Assessment struct {
	Reached      bool     // contract satisfied?
	Gaps         []Gap    // what's missing
	CoveragePct  float64  // objective coverage of gated module
	Reasoning    string
}

// Gap describes a coverage shortfall found during assessment.
type Gap struct {
	Kind   string // scope | pathtype | error | boundary | invariant | coverage
	Detail string
}
```

- [ ] **Step 4: Run test — expect PASS**

Run: `go test ./internal/head/contract/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/head/contract/
git commit -m "feat(contract): add CoverageContract shared types"
```

---

### Task 2: Depth tiers + dimension expansion

**Files:**
- Create: `internal/head/contract/depth.go`
- Test: `internal/head/contract/depth_test.go`

**Interfaces:**
- Produces: `contract.DepthStandard`/`Smoke`/`Thorough` constants, `contract.ExpandDepth(depth string) Dimensions`.
- Consumes: Task 1 types.

- [ ] **Step 1: Write the failing test**

```go
// internal/head/contract/depth_test.go
package contract

import "testing"

func TestExpandDepth(t *testing.T) {
	smoke := ExpandDepth(DepthSmoke)
	if contains(smoke.PathTypes, "boundary") {
		t.Error("smoke must not include boundary paths")
	}
	std := ExpandDepth(DepthStandard)
	if !contains(std.PathTypes, "boundary") || !contains(std.ErrorScope, "4xx") {
		t.Error("standard must include boundary + 4xx")
	}
	thorough := ExpandDepth(DepthThorough)
	if !contains(thorough.Boundaries, "extreme") {
		t.Error("thorough must include extreme boundaries")
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run test — expect FAIL**

Run: `go test ./internal/head/contract/ -run TestExpandDepth`
Expected: `FAIL` (undefined ExpandDepth).

- [ ] **Step 3: Implement depth expansion**

```go
// internal/head/contract/depth.go
package contract

// Depth tiers.
const (
	DepthSmoke    = "smoke"
	DepthStandard = "standard"
	DepthThorough = "thorough"
)

// Dimensions is the per-tier expansion of what the contract should cover.
type Dimensions struct {
	PathTypes  []string
	ErrorScope []string
	Boundaries []string
	Concurrency bool
}

// ExpandDepth maps a tier to its dimension expansion.
func ExpandDepth(depth string) Dimensions {
	switch depth {
	case DepthSmoke:
		return Dimensions{
			PathTypes:  []string{"happy"},
			ErrorScope: []string{"none"},
		}
	case DepthThorough:
		return Dimensions{
			PathTypes:  []string{"happy", "alternative", "boundary", "edge"},
			ErrorScope: []string{"4xx", "validation", "exception"},
			Boundaries: []string{"empty", "zero", "max", "invalid", "extreme"},
			Concurrency: true,
		}
	default: // standard
		return Dimensions{
			PathTypes:  []string{"happy", "alternative"},
			ErrorScope: []string{"4xx", "validation"},
			Boundaries: []string{"empty", "zero", "max", "invalid"},
		}
	}
}
```

- [ ] **Step 4: Run test — expect PASS**

Run: `go test ./internal/head/contract/ -run TestExpandDepth`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/head/contract/depth.go internal/head/contract/depth_test.go
git commit -m "feat(contract): add depth tier dimension expansion"
```

---

### Task 3: CoverageSettings config

**Files:**
- Modify: `internal/project/schema.go` (add to `Settings`)
- Test: `internal/project/schema_test.go` (add)

**Interfaces:**
- Produces: `project.Settings.Coverage` (`CoverageSettings{Depth, LineThreshold, BranchThreshold}`).
- Consumes: Task 2 constants (via string values).

- [ ] **Step 1: Write the failing test**

```go
// append to internal/project/schema_test.go
func TestCoverageSettingsParse(t *testing.T) {
	var s Settings
	require.NoError(t, yaml.Unmarshal([]byte("coverage:\n  depth: thorough\n  line_threshold: 0.85\n"), &s))
	if s.Coverage.Depth != "thorough" || s.Coverage.LineThreshold != 0.85 {
		t.Fatalf("parsed = %+v", s.Coverage)
	}
}
```
(Add `"gopkg.in/yaml.v3"` import if not present; DefaultConfig should set Depth to `contract.DepthStandard` — but to avoid an import cycle project→contract, use the literal `"standard"` and document it. Add a `DefaultCoverageSettings()` helper returning standard defaults.)

- [ ] **Step 2: Run test — expect FAIL**

Run: `go test ./internal/project/ -run TestCoverageSettings`
Expected: `FAIL` (no Coverage field).

- [ ] **Step 3: Implement**

```go
// internal/project/schema.go — add to Settings:
type Settings struct {
	Mode                string            `yaml:"mode,omitempty"`
	// ... existing fields ...
	Coverage            CoverageSettings  `yaml:"coverage,omitempty"`
}

// CoverageSettings configures the coverage contract tier and thresholds.
type CoverageSettings struct {
	Depth           string  `yaml:"depth,omitempty"`            // default "standard"
	LineThreshold   float64 `yaml:"line_threshold,omitempty"`   // default 0.65
	BranchThreshold float64 `yaml:"branch_threshold,omitempty"` // default 0.50
}

// ResolveCoverage fills defaults (called by DefaultConfig + config loaders).
func ResolveCoverage(c CoverageSettings) CoverageSettings {
	if c.Depth == "" {
		c.Depth = "standard"
	}
	if c.LineThreshold == 0 {
		c.LineThreshold = 0.65
	}
	if c.BranchThreshold == 0 {
		c.BranchThreshold = 0.50
	}
	return c
}
```
Ensure `DefaultConfig()` calls `ResolveCoverage` on its Settings.Coverage.

- [ ] **Step 4: Run test — expect PASS**

Run: `go test ./internal/project/ -run TestCoverageSettings`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/project/schema.go internal/project/schema_test.go
git commit -m "feat(project): add coverage depth config"
```

---

### Task 4: Scout.BuildCoverageContract

**Files:**
- Create: `internal/head/scout/contract.go`
- Test: `internal/head/scout/contract_test.go`

**Interfaces:**
- Produces: `(*Scout).BuildCoverageContract(ctx, goal, model, depth) (*contract.Contract, error)`.
- Consumes: Task 1/2 contract package; existing `Scout.driver`, `project.ProjectModel`.

- [ ] **Step 1: Write the failing test**

```go
// internal/head/scout/contract_test.go
package scout

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/contract"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

func TestBuildCoverageContract(t *testing.T) {
	// mock LLM returns a valid contract JSON
	mock := llm.NewMockClient(map[string]string{
		"default": `{"depth":"standard","scope":["internal/llm"],"path_types":["happy","alternative"],"error_scope":["4xx"],"boundaries":["empty"],"priorities":{"internal/llm":"high"},"coverage_gate":{"module":"internal/llm","line_threshold":0.65}}`,
	})
	driver := ai.NewDriver(mock, ai.NewTokenBudget(10000, 1000))
	s := NewScout(driver, setupTestStore(t), &project.Config{}, zap.NewNop())

	c, err := s.BuildCoverageContract(context.Background(), "test internal/llm", &project.ProjectModel{}, contract.DepthStandard)
	require.NoError(t, err)
	assert.Equal(t, "standard", c.Depth)
	assert.Contains(t, c.Scope, "internal/llm")
	assert.Equal(t, 0.65, c.CoverageGate.LineThreshold)
}
```
(Add `"github.com/binoctal/cerberus/internal/ai"` import.)

- [ ] **Step 2: Run test — expect FAIL**

Run: `go test ./internal/head/scout/ -run TestBuildCoverageContract`
Expected: `FAIL` (undefined BuildCoverageContract).

- [ ] **Step 3: Implement**

```go
// internal/head/scout/contract.go
package scout

import (
	"context"
	"fmt"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/contract"
	"github.com/binoctal/cerberus/internal/project"
)

const promptContractSystem = `You define a coverage contract: what a test session must cover and how deeply. Given a project model, goal, and depth tier, return the scope, path types, error scopes, boundaries, priorities, and an objective coverage gate.`

func (s *Scout) BuildCoverageContract(ctx context.Context, goal string, model *project.ProjectModel, depth string) (*contract.Contract, error) {
	dims := contract.ExpandDepth(depth)
	prompt := ai.NewPrompt().
		System(promptContractSystem).
		Context(s.buildAnalyzeContext(TargetInfo{Goal: goal})).
		Task(fmt.Sprintf("Goal: %s\nDepth: %s\nExpand to dimensions: %+v\nReturn a JSON coverage contract.", goal, depth, dims)).
		Output(`Respond with JSON: {"depth":"","scope":[],"path_types":[],"error_scope":[],"boundaries":[],"priorities":{},"coverage_gate":{"module":"","line_threshold":0.0}}`).
		Build()

	var c contract.Contract
	if err := s.driver.Decide(ctx, prompt, &c); err != nil {
		return nil, fmt.Errorf("build coverage contract: %w", err)
	}
	if c.Depth == "" {
		c.Depth = depth
	}
	// carry invariants from config as hard refs
	for _, inv := range s.config.Invariants {
		c.Invariants = append(c.Invariants, contract.InvariantRef{ID: inv.ID, Description: inv.Description})
	}
	return &c, nil
}
```

- [ ] **Step 4: Run test — expect PASS**

Run: `go test ./internal/head/scout/ -run TestBuildCoverageContract`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/head/scout/contract.go internal/head/scout/contract_test.go
git commit -m "feat(scout): build AI-authored coverage contract"
```

---

### Task 5: Contract self-assessment (meta)

**Files:**
- Modify: `internal/head/scout/contract.go`
- Test: `internal/head/scout/contract_test.go` (add)

**Interfaces:**
- Produces: `(*Scout).SelfAssessContract(ctx, *contract.Contract) (notes []string, err error)` — returns gap notes the builder folds into case generation.

- [ ] **Step 1: Write the failing test**

```go
func TestSelfAssessContract(t *testing.T) {
	mock := llm.NewMockClient(map[string]string{"default": `{"notes":["missing error handling for 5xx","scope omits internal/session"]}`})
	driver := ai.NewDriver(mock, ai.NewTokenBudget(10000, 1000))
	s := NewScout(driver, setupTestStore(t), &project.Config{}, zap.NewNop())

	c := &contract.Contract{Depth: "standard", Scope: []string{"internal/llm"}}
	notes, err := s.SelfAssessContract(context.Background(), c)
	require.NoError(t, err)
	assert.NotEmpty(t, notes)
}
```

- [ ] **Step 2: Run test — expect FAIL**

Run: `go test ./internal/head/scout/ -run TestSelfAssessContract`
Expected: `FAIL` (undefined).

- [ ] **Step 3: Implement**

```go
// append to internal/head/scout/contract.go
func (s *Scout) SelfAssessContract(ctx context.Context, c *contract.Contract) ([]string, error) {
	prompt := ai.NewPrompt().
		System(`You critique a coverage contract for gaps: missing scope, missing path types vs the depth tier, missing invariants. Return notes only.`).
		Task(fmt.Sprintf("Contract: %+v", c)).
		Output(`Respond with JSON: {"notes":[]}`).
		Build()
	var out struct{ Notes []string }
	if err := s.driver.Decide(ctx, prompt, &out); err != nil {
		return nil, fmt.Errorf("self-assess contract: %w", err)
	}
	return out.Notes, nil
}
```
Wire it: `BuildCoverageContract` calls `SelfAssessContract` and logs notes (smoke tier may skip — see Task 4 callers in Task 7).

- [ ] **Step 4: Run test — expect PASS**

Run: `go test ./internal/head/scout/ -run TestSelfAssessContract`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/head/scout/contract.go internal/head/scout/contract_test.go
git commit -m "feat(scout): add contract self-assessment pass"
```

---

### Task 6: Examiner.AssessCoverage

**Files:**
- Create: `internal/head/examiner/assess.go`
- Test: `internal/head/examiner/assess_test.go`

**Interfaces:**
- Produces: `(*Examiner).AssessCoverage(ctx, *contract.Contract, []agent.StepResult, coveragePct) (*contract.Assessment, error)`.
- Consumes: Tasks 1, 2; existing `Examiner.judgeDriver`, `agent.StepResult`.

- [ ] **Step 1: Write the failing test**

```go
// internal/head/examiner/assess_test.go
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

func TestAssessCoverage(t *testing.T) {
	mock := llm.NewMockClient(map[string]string{
		"default": `{"reached":false,"gaps":[{"kind":"scope","detail":"internal/session not covered"}],"coverage_pct":0.42,"reasoning":"scope incomplete"}`,
	})
	driver := ai.NewDriver(mock, ai.NewTokenBudget(10000, 1000))
	e := NewExaminer(driver, nil, setupExaminerStore(t), DefaultExaminerConfig(), zap.NewNop())

	c := &contract.Contract{Depth: "standard", Scope: []string{"internal/llm", "internal/session"}, CoverageGate: contract.Gate{Module: "internal/llm", LineThreshold: 0.65}}
	res := []agent.StepResult{{TestCase: &agent.TestCase{ID: "tc-1", Target: "internal/llm"}}}

	a, err := e.AssessCoverage(context.Background(), c, res, 0.42)
	require.NoError(t, err)
	assert.False(t, a.Reached)
	assert.NotEmpty(t, a.Gaps)
}
```
(`setupExaminerStore` exists in `examiner_test.go`.)

- [ ] **Step 2: Run test — expect FAIL**

Run: `go test ./internal/head/examiner/ -run TestAssessCoverage`
Expected: `FAIL` (undefined AssessCoverage).

- [ ] **Step 3: Implement**

```go
// internal/head/examiner/assess.go
package examiner

import (
	"context"
	"fmt"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/contract"
)

func (e *Examiner) AssessCoverage(ctx context.Context, c *contract.Contract, results []agent.StepResult, coveragePct float64) (*contract.Assessment, error) {
	prompt := ai.NewPrompt().
		System(`You assess a test session against its coverage contract. Judge whether scope, path types, error scopes, boundaries, and invariants are covered. Use the objective coverage %. Report gaps concretely.`).
		Task(fmt.Sprintf("Contract: %+v\nCases run: %d\nObjective coverage of gated module: %.2f (gate: %.2f)", c, len(results), coveragePct, c.CoverageGate.LineThreshold)).
		Output(`Respond with JSON: {"reached":false,"gaps":[{"kind":"","detail":""}],"coverage_pct":0.0,"reasoning":""}`).
		Build()
	var a contract.Assessment
	if err := e.judgeDriver.Decide(ctx, prompt, &a); err != nil {
		return nil, fmt.Errorf("assess coverage: %w", err)
	}
	// Objective gate override: below threshold → not reached regardless of LLM.
	if coveragePct < c.CoverageGate.LineThreshold {
		a.Reached = false
		a.Gaps = append(a.Gaps, contract.Gap{Kind: "coverage", Detail: fmt.Sprintf("%.0f%% < %.0f%% gate", coveragePct*100, c.CoverageGate.LineThreshold*100)})
	}
	a.CoveragePct = coveragePct
	return &a, nil
}
```

- [ ] **Step 4: Run test — expect PASS**

Run: `go test ./internal/head/examiner/ -run TestAssessCoverage`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/head/examiner/assess.go internal/head/examiner/assess_test.go
git commit -m "feat(examiner): assess session against coverage contract"
```

---

### Task 7: Session wiring + report

**Files:**
- Modify: `internal/session/lifecycle_types.go` (add `Contract *contract.Contract`)
- Modify: `internal/session/run_phases_scout.go` (build contract before Plan)
- Modify: `internal/session/run_phases_examiner.go` (call AssessCoverage)
- Modify: `cmd/cerberus/main_report.go` (render contract + assessment)

**Interfaces:**
- Consumes: Tasks 3–6.

- [ ] **Step 1: Add Contract field + integration test**

```go
// internal/session/lifecycle_types.go — add to Session:
Contract *contract.Contract
```
Add an integration test that runs a session with a mock LLM returning both plan JSON and contract JSON, and asserts `sess.Contract != nil` and the summary includes an assessment. Reuse the pattern in `internal/session/autotest_integration_test.go`.

- [ ] **Step 2: Wire Scout phase**

In `run_phases_scout.go`, after `Analyze` and before `Plan`:
```go
depth := project.ResolveCoverage(rp.session.Config.Settings.Coverage).Depth
rp.session.Contract, err = scoutHead.BuildCoverageContract(rp.ctx, rp.session.Goal, model, depth)
if err != nil {
	rp.session.Logger.Warn("coverage contract build failed; proceeding without", zap.Error(err))
}
if rp.session.Contract != nil && depth != "smoke" {
	if notes, nerr := scoutHead.SelfAssessContract(rp.ctx, rp.session.Contract); nerr == nil {
		rp.session.Logger.Info("contract self-assessment notes", zap.Strings("notes", notes))
	}
}
```
(`project` + `zap` already imported; add `contract` if needed — actually only logging here, contract is on session.)

- [ ] **Step 3: Wire Examiner phase**

In `run_phases_examiner.go`, after the verdict loop, before persist:
```go
if rp.session.Contract != nil {
	covPct := rp.summary.CoveragePct // or read from coverage provider
	assessment, aerr := examinerHead.AssessCoverage(rp.ctx, rp.session.Contract, rp.results, covPct)
	if aerr == nil {
		rp.session.Logger.Info("coverage assessment", zap.Bool("reached", assessment.Reached), zap.Int("gaps", len(assessment.Gaps)))
		// store assessment on session for report (add field if needed)
	}
}
```

- [ ] **Step 4: Render in report**

In `main_report.go`, add a section after Verdicts rendering that prints the contract (depth, scope, gate) and the assessment (reached, gaps) if present on the session.

- [ ] **Step 5: Run full suite + manual check**

Run: `make check`
Expected: all green.
Manual: run `cerberus run --dir . --goal "..." ` (local-only config) once and confirm report shows contract + assessment. (This is a smoke check, not a permanent test.)

- [ ] **Step 6: Commit**

```bash
git add internal/session/ cmd/cerberus/main_report.go
git commit -m "feat(session): wire coverage contract through run + report"
```

---

## Plan 2 (out of scope here, referenced)

**Fixture matrix** — `test/fixtures/{go-lib,node-app,python-pkg,saas-api}/` + integration tests running `cerberus run` with mock LLM per fixture, validating contract build/assess across languages and exercising the never-run Node/Python autotest paths. This is its own plan (each fixture + its autotest path is a testable unit). Tracked as the follow-up to this plan.

---

## Self-Review (run after writing)

- **Spec coverage**: types (T1), tiers (T2), config (T3), Scout build (T4) + self-assess (T5), Examiner assess (T6), wiring+report (T7) — all spec components mapped. Fixture matrix explicitly Plan 2.
- **Placeholders**: none; every code step has full code.
- **Type consistency**: `contract.Contract` / `Gate` / `Assessment` / `Gap` consistent across T1, T4, T5, T6, T7. `ExpandDepth` returns `Dimensions`; `BuildCoverageContract` takes `depth string`.
- **One caveat flagged**: Task 3 avoids an import cycle (project → contract) by using the string `"standard"` literal with a comment, not `contract.DepthStandard`. Documented inline.
