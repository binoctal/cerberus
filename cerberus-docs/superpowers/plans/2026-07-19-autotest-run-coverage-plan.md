# AutoTest RunCoverage Injectable-Runner Refactor — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor Node / Python / Mocha coverage providers to use an injected runner (matching Go), add hermetic unit tests for every `RunCoverage` branch, wire up the dead `DefaultNodeCoverageRunner` / `DefaultPythonCoverageRunner`, add `DefaultMochaCoverageRunner`, and remove Python's untested SQLite fallback.

**Architecture:** Each provider gains a `run coverageRunner` field (`func(ctx, projectDir) ([]byte, error)`). `RunCoverage` narrows to nil-check → `p.run` → parse. All exec logic moves into the per-language `DefaultXxxCoverageRunner`. `provider_factory.go` drops its `interface{}` parameter for a concrete function type and selects the per-language Default when the caller passes nil. `GoCoverageProvider` is the reference shape — this plan makes the other three match it.

**Tech Stack:** Go 1.25, module `github.com/binoctal/cerberus`, SQLite via `modernc.org/sqlite` (no CGo), `testify`, `go.uber.org/zap`.

**Spec:** `cerberus-docs/superpowers/specs/2026-07-19-autotest-run-coverage-design.md`

## Global Constraints

- Go 1.25, module `github.com/binoctal/cerberus`. Pure-Go SQLite, no CGo.
- Commit author `binoctal <binoctal@gmail.com>`, **no Co-Authored-By**. Commit messages and code comments in English.
- All docs under `cerberus-docs/` (never `docs/`).
- `coverageRunner` is an unexported type (`internal/autotest/coverage_go.go:18`): `func(ctx context.Context, projectDir string) ([]byte, error)`. Provider constructors are package-internal and may take it directly; `NewCoverageProviderForLanguage` is called from `internal/session` (external) so its parameter uses the unnamed function type, not `coverageRunner`.
- `DetectLanguage` returns only `"go" | "node" | "python"` — never `"mocha"`. The factory therefore has no mocha case. Mocha is refactored for symmetry and coverage; `DefaultMochaCoverageRunner` is not routed by the factory.
- Per-task gate: `PATH="$(go env GOPATH)/bin:$PATH" make check` (fmt + lint + test) must pass.
- `GoCoverageProvider` is the reference implementation — do not modify it. Match its shape: constructor takes the runner, `RunCoverage` errors on `p.run == nil`, no internal default.

## File Structure

**Modified:**
- `internal/autotest/coverage_node_types.go` — add `run coverageRunner` field.
- `internal/autotest/coverage_node_factory.go` — `NewNodeCoverageProvider` gains `run` param.
- `internal/autotest/coverage_node_run.go` — `RunCoverage` rewritten to call `p.run` + `parseJestCoverage`.
- `internal/autotest/coverage_python_types.go` — add `run coverageRunner` field.
- `internal/autotest/coverage_python_factory.go` — `NewPythonCoverageProvider` gains `run` param.
- `internal/autotest/coverage_python_run.go` — `RunCoverage` rewritten; `parseCoverageData` deleted.
- `internal/autotest/coverage_python_parse.go` — `parseSQLiteCoverage` deleted (and now-unused imports).
- `internal/autotest/coverage_mocha_types.go` — add `run coverageRunner` field.
- `internal/autotest/coverage_mocha_factory.go` — `NewMochaCoverageProvider` gains `run` param; add `DefaultMochaCoverageRunner`.
- `internal/autotest/coverage_mocha_run.go` — `RunCoverage` rewritten to call `p.run` + `parseIstanbulCoverage`.
- `internal/autotest/provider_factory.go` — `runner` parameter typed; per-language Default selection; drop `interface{}`.
- `internal/autotest/coverage_providers_parse_test.go` — update 6 constructor calls for new signatures.
- `internal/session/run_phases_autotest.go` — pass `nil` runner to factory.
- `internal/session/coverage.go` — pass `nil` runner to factory.

**Deleted:**
- `internal/autotest/coverage_python_run_helpers.go` — entire file (all five helpers + `pythonCmdContext` become dead under the new model).

**Created:**
- `internal/autotest/coverage_node_run_test.go` — hermetic `RunCoverage` tests.
- `internal/autotest/coverage_python_run_test.go` — hermetic `RunCoverage` tests.
- `internal/autotest/coverage_mocha_run_test.go` — hermetic `RunCoverage` tests.
- `internal/autotest/config_test.go` — tests for the five 0%-coverage config constructors.

---

### Task 1: Node provider — injectable runner + hermetic tests

**Files:**
- Modify: `internal/autotest/coverage_node_types.go`
- Modify: `internal/autotest/coverage_node_factory.go`
- Modify: `internal/autotest/coverage_node_run.go`
- Modify: `internal/autotest/provider_factory.go` (node case only)
- Modify: `internal/autotest/coverage_providers_parse_test.go:24,37`
- Create: `internal/autotest/coverage_node_run_test.go`

**Interfaces:**
- Consumes: `coverageRunner` (coverage_go.go:18), `DefaultNodeCoverageRunner` (coverage_node_factory.go:21), `parseJestCoverage` (coverage_node_parse.go:9).
- Produces: `NewNodeCoverageProvider(cfg *CoverageConfig, run coverageRunner, logger *zap.Logger) *NodeCoverageProvider`; `NodeCoverageProvider.RunCoverage` is now injectable.

- [ ] **Step 1: Add `run` field to the struct**

Replace the struct in `internal/autotest/coverage_node_types.go`:

```go
// NodeCoverageProvider implements CoverageProvider for Node.js Jest projects
type NodeCoverageProvider struct {
	config *CoverageConfig
	run    coverageRunner
	logger *zap.Logger
}
```

- [ ] **Step 2: Update the constructor**

Replace `NewNodeCoverageProvider` in `internal/autotest/coverage_node_factory.go`:

```go
// NewNodeCoverageProvider creates a new Node coverage provider. Pass nil run
// to use the RunCoverage nil-runner guard (tests); the factory wires the real
// default. Matches GoCoverageProvider's shape.
func NewNodeCoverageProvider(cfg *CoverageConfig, run coverageRunner, logger *zap.Logger) *NodeCoverageProvider {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &NodeCoverageProvider{config: cfg, run: run, logger: logger}
}
```

- [ ] **Step 3: Rewrite `RunCoverage`**

Replace the entire `RunCoverage` method body in `internal/autotest/coverage_node_run.go` with:

```go
package autotest

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// RunCoverage invokes the injected runner and parses the returned Jest JSON.
// The runner owns exec, timeout, and reading the coverage file.
func (p *NodeCoverageProvider) RunCoverage(ctx context.Context, projectDir string) (*CoverageReport, error) {
	if p.config == nil {
		return nil, fmt.Errorf("node coverage: config not set")
	}
	if p.run == nil {
		return nil, fmt.Errorf("node coverage: runner not configured")
	}
	data, err := p.run(ctx, projectDir)
	if err != nil {
		return nil, fmt.Errorf("node coverage: run failed: %w", err)
	}
	report, err := p.parseJestCoverage(data)
	if err != nil {
		return nil, fmt.Errorf("node coverage: parse: %w", err)
	}
	report.Pass = true
	report.CoverageUnit = "function"
	p.logger.Info("node coverage complete",
		zap.Int("total_funcs", report.TotalFuncs),
		zap.Int("covered_funcs", report.CoveredFuncs))
	return report, nil
}
```

Note: the old file's imports (`os`, `os/exec`, `path/filepath`, `strings`) are no longer used — the new import block above is the complete set.

- [ ] **Step 4: Wire the factory's node case**

In `internal/autotest/provider_factory.go`, replace the `case "node":` line:

```go
	case "node":
		return NewNodeCoverageProvider(DefaultNodeCoverageConfig(), DefaultNodeCoverageRunner, logger)
```

(The `runner interface{}` parameter stays for now — Task 4 retypes it. The node case ignores it and uses `DefaultNodeCoverageRunner`, matching how Go already works.)

- [ ] **Step 5: Fix the existing parse-test call sites**

In `internal/autotest/coverage_providers_parse_test.go`, replace both occurrences (lines ~24 and ~37):

```go
p := NewNodeCoverageProvider(nil)
```

with:

```go
p := NewNodeCoverageProvider(nil, nil, nil)
```

- [ ] **Step 6: Write the failing hermetic tests**

Create `internal/autotest/coverage_node_run_test.go`:

```go
package autotest

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const jestValidJSON = `{
  "/f.js": {
    "statementMap": {
      "0": {"start": {"line": 1, "column": 0}, "end": {"line": 1, "column": 5}},
      "1": {"start": {"line": 2, "column": 0}, "end": {"line": 2, "column": 5}}
    },
    "s": {"0": 1, "1": 0}
  }
}`

func TestNodeRunCoverage_NilConfig(t *testing.T) {
	p := NewNodeCoverageProvider(nil, func(context.Context, string) ([]byte, error) { return nil, nil }, nil)
	_, err := p.RunCoverage(context.Background(), ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config")
}

func TestNodeRunCoverage_NilRunner(t *testing.T) {
	p := NewNodeCoverageProvider(DefaultNodeCoverageConfig(), nil, nil)
	_, err := p.RunCoverage(context.Background(), ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runner")
}

func TestNodeRunCoverage_RunnerError(t *testing.T) {
	p := NewNodeCoverageProvider(DefaultNodeCoverageConfig(),
		func(context.Context, string) ([]byte, error) { return nil, errors.New("boom") }, nil)
	_, err := p.RunCoverage(context.Background(), ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

func TestNodeRunCoverage_ValidJSON(t *testing.T) {
	p := NewNodeCoverageProvider(DefaultNodeCoverageConfig(),
		func(context.Context, string) ([]byte, error) { return []byte(jestValidJSON), nil }, nil)
	rep, err := p.RunCoverage(context.Background(), ".")
	require.NoError(t, err)
	assert.True(t, rep.Pass)
	assert.Equal(t, "function", rep.CoverageUnit)
	assert.Equal(t, 2, rep.TotalFuncs)
	assert.Equal(t, 1, rep.CoveredFuncs)
}

func TestNodeRunCoverage_Garbage(t *testing.T) {
	p := NewNodeCoverageProvider(DefaultNodeCoverageConfig(),
		func(context.Context, string) ([]byte, error) { return []byte("not json"), nil }, nil)
	_, err := p.RunCoverage(context.Background(), ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse")
}
```

- [ ] **Step 7: Run tests — verify pass**

Run: `go test ./internal/autotest/ -run TestNodeRunCoverage -v`
Expected: PASS (5 tests).

Run: `PATH="$(go env GOPATH)/bin:$PATH" make check`
Expected: PASS (full suite green; parse tests still pass with updated constructor calls).

- [ ] **Step 8: Commit**

```bash
git add internal/autotest/coverage_node_types.go internal/autotest/coverage_node_factory.go \
  internal/autotest/coverage_node_run.go internal/autotest/coverage_node_run_test.go \
  internal/autotest/provider_factory.go internal/autotest/coverage_providers_parse_test.go
git -c user.name="binoctal" -c user.email="binoctal@gmail.com" \
  commit -m "refactor(autotest): Node RunCoverage uses injected runner + hermetic tests"
```

---

### Task 2: Python provider — injectable runner, remove SQLite fallback, delete dead helpers

**Files:**
- Modify: `internal/autotest/coverage_python_types.go`
- Modify: `internal/autotest/coverage_python_factory.go`
- Modify: `internal/autotest/coverage_python_run.go`
- Modify: `internal/autotest/coverage_python_parse.go`
- Modify: `internal/autotest/provider_factory.go` (python case only)
- Modify: `internal/autotest/coverage_providers_parse_test.go:53,63,73`
- Delete: `internal/autotest/coverage_python_run_helpers.go`
- Create: `internal/autotest/coverage_python_run_test.go`

**Interfaces:**
- Consumes: `DefaultPythonCoverageRunner` (coverage_python_factory.go:21), `parseJSONCoverage` (coverage_python_parse.go:15).
- Produces: `NewPythonCoverageProvider(cfg, run, logger)`; `parseCoverageData` and `parseSQLiteCoverage` are removed; `coverage_python_run_helpers.go` is deleted.

- [ ] **Step 1: Add `run` field to the struct**

Replace the struct in `internal/autotest/coverage_python_types.go`:

```go
// PythonCoverageProvider implements CoverageProvider for Python pytest+coverage.py projects
type PythonCoverageProvider struct {
	config *CoverageConfig
	run    coverageRunner
	logger *zap.Logger
}
```

- [ ] **Step 2: Update the constructor**

Replace `NewPythonCoverageProvider` in `internal/autotest/coverage_python_factory.go`:

```go
// NewPythonCoverageProvider creates a new Python coverage provider. Pass nil run
// for the nil-runner guard (tests); the factory wires the real default.
func NewPythonCoverageProvider(cfg *CoverageConfig, run coverageRunner, logger *zap.Logger) *PythonCoverageProvider {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &PythonCoverageProvider{config: cfg, run: run, logger: logger}
}
```

- [ ] **Step 3: Rewrite `RunCoverage` and delete `parseCoverageData`**

Replace the entire contents of `internal/autotest/coverage_python_run.go` with:

```go
package autotest

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// RunCoverage invokes the injected runner and parses the returned coverage.py JSON.
// The runner owns exec, timeout, and reading the coverage file. SQLite fallback
// was removed: the runner guarantees JSON output.
func (p *PythonCoverageProvider) RunCoverage(ctx context.Context, projectDir string) (*CoverageReport, error) {
	if p.config == nil {
		return nil, fmt.Errorf("python coverage: config not set")
	}
	if p.run == nil {
		return nil, fmt.Errorf("python coverage: runner not configured")
	}
	data, err := p.run(ctx, projectDir)
	if err != nil {
		return nil, fmt.Errorf("python coverage: run failed: %w", err)
	}
	report, err := p.parseJSONCoverage(data)
	if err != nil {
		return nil, fmt.Errorf("python coverage: parse: %w", err)
	}
	report.Pass = true
	report.CoverageUnit = "function"
	p.logger.Info("python coverage complete",
		zap.Int("total_funcs", report.TotalFuncs),
		zap.Int("covered_funcs", report.CoveredFuncs))
	return report, nil
}
```

(This removes `parseCoverageData`, which was the JSON→SQLite fallback.)

- [ ] **Step 4: Delete `parseSQLiteCoverage` and fix imports**

In `internal/autotest/coverage_python_parse.go`:
- Delete the entire `parseSQLiteCoverage` method (the function starting `func (p *PythonCoverageProvider) parseSQLiteCoverage(projectDir string)` through its closing brace — currently lines ~74-155).
- The remaining file keeps only `parseJSONCoverage`. Update the import block to drop now-unused imports — the final imports are:

```go
import (
	"encoding/json"
	"fmt"
)
```

(`database/sql`, `context`, `os`, `path/filepath`, and `_ "modernc.org/sqlite"` are all removed — only `parseJSONCoverage` remains, which uses `encoding/json` and `fmt`.)

- [ ] **Step 5: Delete the dead helpers file**

```bash
rm internal/autotest/coverage_python_run_helpers.go
```

This removes `pythonCmdContext`, `determinePythonCommand`, `buildPythonTestCommand`, `applyTimeout`, `executeTestCommand`, `generateCoverageReport`. None are referenced after Step 3 (the old `RunCoverage` was their only caller).

- [ ] **Step 6: Verify nothing else references the deleted symbols**

Run: `grep -rn "parseSQLiteCoverage\|parseCoverageData\|pythonCmdContext\|determinePythonCommand\|buildPythonTestCommand\|applyTimeout\|executeTestCommand\|generateCoverageReport" --include="*.go" internal/`
Expected: no matches (empty output).

- [ ] **Step 7: Wire the factory's python case**

In `internal/autotest/provider_factory.go`, replace the `case "python":` line:

```go
	case "python":
		return NewPythonCoverageProvider(DefaultPythonCoverageConfig(), DefaultPythonCoverageRunner, logger)
```

- [ ] **Step 8: Fix the existing parse-test call sites**

In `internal/autotest/coverage_providers_parse_test.go`, replace all three occurrences (lines ~53, ~63, ~73):

```go
p := NewPythonCoverageProvider(nil)
```

with:

```go
p := NewPythonCoverageProvider(nil, nil, nil)
```

- [ ] **Step 9: Write the failing hermetic tests**

Create `internal/autotest/coverage_python_run_test.go`:

```go
package autotest

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const pythonValidJSON = `{
  "files": {
    "/f.py": {
      "summary": {"num_statements": 2, "covered_lines": 1, "percent_covered": 50.0, "missing_lines": 1},
      "executed_lines": [1],
      "missing_lines": [2]
    }
  },
  "meta": {"branch_coverage": false, "timestamp": "2026-07-19T00:00:00"}
}`

func TestPythonRunCoverage_NilConfig(t *testing.T) {
	p := NewPythonCoverageProvider(nil, func(context.Context, string) ([]byte, error) { return nil, nil }, nil)
	_, err := p.RunCoverage(context.Background(), ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config")
}

func TestPythonRunCoverage_NilRunner(t *testing.T) {
	p := NewPythonCoverageProvider(DefaultPythonCoverageConfig(), nil, nil)
	_, err := p.RunCoverage(context.Background(), ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runner")
}

func TestPythonRunCoverage_RunnerError(t *testing.T) {
	p := NewPythonCoverageProvider(DefaultPythonCoverageConfig(),
		func(context.Context, string) ([]byte, error) { return nil, errors.New("boom") }, nil)
	_, err := p.RunCoverage(context.Background(), ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

func TestPythonRunCoverage_ValidJSON(t *testing.T) {
	p := NewPythonCoverageProvider(DefaultPythonCoverageConfig(),
		func(context.Context, string) ([]byte, error) { return []byte(pythonValidJSON), nil }, nil)
	rep, err := p.RunCoverage(context.Background(), ".")
	require.NoError(t, err)
	assert.True(t, rep.Pass)
	assert.Equal(t, "function", rep.CoverageUnit)
	assert.Equal(t, 2, rep.TotalFuncs)
	assert.Equal(t, 1, rep.CoveredFuncs)
}

func TestPythonRunCoverage_Garbage(t *testing.T) {
	p := NewPythonCoverageProvider(DefaultPythonCoverageConfig(),
		func(context.Context, string) ([]byte, error) { return []byte("not json"), nil }, nil)
	_, err := p.RunCoverage(context.Background(), ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse")
}
```

- [ ] **Step 10: Run tests — verify pass**

Run: `go test ./internal/autotest/ -run TestPythonRunCoverage -v`
Expected: PASS (5 tests).

Run: `go build ./...`
Expected: compiles (confirms deleted symbols have no remaining references).

Run: `PATH="$(go env GOPATH)/bin:$PATH" make check`
Expected: PASS.

- [ ] **Step 11: Commit**

```bash
git add -A internal/autotest/
git -c user.name="binoctal" -c user.email="binoctal@gmail.com" \
  commit -m "refactor(autotest): Python RunCoverage uses injected runner; drop SQLite fallback + dead helpers"
```

(`git add -A internal/autotest/` stages the file deletion alongside the edits.)

---

### Task 3: Mocha provider — injectable runner + new Default runner + hermetic tests

**Files:**
- Modify: `internal/autotest/coverage_mocha_types.go`
- Modify: `internal/autotest/coverage_mocha_factory.go`
- Modify: `internal/autotest/coverage_mocha_run.go`
- Modify: `internal/autotest/coverage_providers_parse_test.go:44`
- Create: `internal/autotest/coverage_mocha_run_test.go`

**Interfaces:**
- Consumes: `parseIstanbulCoverage` (coverage_mocha_helpers.go:13).
- Produces: `NewMochaCoverageProvider(cfg, run, logger)`, `DefaultMochaCoverageRunner`.

**Note:** `DetectLanguage` never returns `"mocha"`, so the factory has no mocha case and `DefaultMochaCoverageRunner` is not routed in production. This task still adds it for symmetry with Node/Python and to make `RunCoverage` testable.

- [ ] **Step 1: Add `run` field to the struct**

Replace the struct in `internal/autotest/coverage_mocha_types.go` (keep the existing `SetLogger` method):

```go
// MochaCoverageProvider implements CoverageProvider for Mocha + nyc projects
type MochaCoverageProvider struct {
	config *CoverageConfig
	run    coverageRunner
	logger *zap.Logger
}
```

- [ ] **Step 2: Update the constructor and add `DefaultMochaCoverageRunner`**

Replace the entire contents of `internal/autotest/coverage_mocha_factory.go` with:

```go
package autotest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"

	"go.uber.org/zap"
)

// NewMochaCoverageProvider creates a new Mocha coverage provider. Pass nil run
// for the nil-runner guard (tests); callers wire the real default.
func NewMochaCoverageProvider(cfg *CoverageConfig, run coverageRunner, logger *zap.Logger) *MochaCoverageProvider {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &MochaCoverageProvider{config: cfg, run: run, logger: logger}
}

// DefaultMochaCoverageRunner runs nyc/mocha with coverage into a tmpdir and
// returns the Istanbul JSON bytes. Mirrors DefaultNodeCoverageRunner.
func DefaultMochaCoverageRunner(ctx context.Context, projectDir string) ([]byte, error) {
	tmp, err := os.MkdirTemp("", "cerberus-mocha-cover-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	out := filepath.Join(tmp, "coverage-final.json")
	cmd := exec.CommandContext(ctx, "npm", "test", "--", "--coverage", "--coverage-reporter=json", "--outputCoverage="+out)
	cmd.Dir = projectDir

	if runErr := cmd.Run(); runErr != nil {
		_ = runErr // Coverage report might still exist
	}

	return os.ReadFile(out)
}
```

- [ ] **Step 3: Rewrite `RunCoverage`**

Replace the entire contents of `internal/autotest/coverage_mocha_run.go` with:

```go
package autotest

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// RunCoverage invokes the injected runner and parses the returned Istanbul JSON.
// The runner owns exec, timeout, and reading the coverage file.
func (p *MochaCoverageProvider) RunCoverage(ctx context.Context, projectDir string) (*CoverageReport, error) {
	if p.config == nil {
		return nil, fmt.Errorf("mocha coverage: config not set")
	}
	if p.run == nil {
		return nil, fmt.Errorf("mocha coverage: runner not configured")
	}
	data, err := p.run(ctx, projectDir)
	if err != nil {
		return nil, fmt.Errorf("mocha coverage: run failed: %w", err)
	}
	report, err := p.parseIstanbulCoverage(data)
	if err != nil {
		return nil, fmt.Errorf("mocha coverage: parse: %w", err)
	}
	report.Pass = true
	report.CoverageUnit = "function"
	p.logger.Info("mocha coverage complete",
		zap.Int("total_funcs", report.TotalFuncs),
		zap.Int("covered_funcs", report.CoveredFuncs))
	return report, nil
}
```

- [ ] **Step 4: Fix the existing parse-test call site**

In `internal/autotest/coverage_providers_parse_test.go`, replace (line ~44):

```go
p := NewMochaCoverageProvider(nil)
```

with:

```go
p := NewMochaCoverageProvider(nil, nil, nil)
```

- [ ] **Step 5: Write the failing hermetic tests**

Create `internal/autotest/coverage_mocha_run_test.go`. Istanbul JSON is the same shape as Jest JSON (`parseIstanbulCoverage` reuses `JestCoverageJSON`), so the valid fixture is identical:

```go
package autotest

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMochaRunCoverage_NilConfig(t *testing.T) {
	p := NewMochaCoverageProvider(nil, func(context.Context, string) ([]byte, error) { return nil, nil }, nil)
	_, err := p.RunCoverage(context.Background(), ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config")
}

func TestMochaRunCoverage_NilRunner(t *testing.T) {
	p := NewMochaCoverageProvider(DefaultMochaCoverageConfig(), nil, nil)
	_, err := p.RunCoverage(context.Background(), ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runner")
}

func TestMochaRunCoverage_RunnerError(t *testing.T) {
	p := NewMochaCoverageProvider(DefaultMochaCoverageConfig(),
		func(context.Context, string) ([]byte, error) { return nil, errors.New("boom") }, nil)
	_, err := p.RunCoverage(context.Background(), ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

func TestMochaRunCoverage_ValidJSON(t *testing.T) {
	p := NewMochaCoverageProvider(DefaultMochaCoverageConfig(),
		func(context.Context, string) ([]byte, error) { return []byte(jestValidJSON), nil }, nil)
	rep, err := p.RunCoverage(context.Background(), ".")
	require.NoError(t, err)
	assert.True(t, rep.Pass)
	assert.Equal(t, "function", rep.CoverageUnit)
	assert.Equal(t, 2, rep.TotalFuncs)
	assert.Equal(t, 1, rep.CoveredFuncs)
}

func TestMochaRunCoverage_Garbage(t *testing.T) {
	p := NewMochaCoverageProvider(DefaultMochaCoverageConfig(),
		func(context.Context, string) ([]byte, error) { return []byte("not json"), nil }, nil)
	_, err := p.RunCoverage(context.Background(), ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse")
}
```

(`jestValidJSON` is defined in `coverage_node_run_test.go` from Task 1 — same package, reused here.)

- [ ] **Step 6: Run tests — verify pass**

Run: `go test ./internal/autotest/ -run TestMochaRunCoverage -v`
Expected: PASS (5 tests).

Run: `PATH="$(go env GOPATH)/bin:$PATH" make check`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/autotest/coverage_mocha_types.go internal/autotest/coverage_mocha_factory.go \
  internal/autotest/coverage_mocha_run.go internal/autotest/coverage_mocha_run_test.go \
  internal/autotest/coverage_providers_parse_test.go
git -c user.name="binoctal" -c user.email="binoctal@gmail.com" \
  commit -m "refactor(autotest): Mocha RunCoverage uses injected runner + add DefaultMochaCoverageRunner"
```

---

### Task 4: Strongly-type the factory and simplify session call sites

**Files:**
- Modify: `internal/autotest/provider_factory.go`
- Modify: `internal/session/run_phases_autotest.go:53`
- Modify: `internal/session/coverage.go:75`

**Interfaces:**
- Produces: `NewCoverageProviderForLanguage(lang string, runner func(context.Context, string) ([]byte, error), logger *zap.Logger) CoverageProvider` — concrete function type, no `interface{}`; selects per-language Default when `runner == nil`.

- [ ] **Step 1: Retype the factory**

Replace the entire `NewCoverageProviderForLanguage` function in `internal/autotest/provider_factory.go` with:

```go
// NewCoverageProviderForLanguage creates the correct coverage provider for a
// detected language. Reused by AutoTest (run_phases_autotest) and the Examiner
// coverage step (coverage.go). Pass nil runner to use each language's Default.
func NewCoverageProviderForLanguage(lang string, runner func(context.Context, string) ([]byte, error), logger *zap.Logger) CoverageProvider {
	switch lang {
	case "node":
		if runner == nil {
			runner = DefaultNodeCoverageRunner
		}
		return NewNodeCoverageProvider(DefaultNodeCoverageConfig(), runner, logger)
	case "python":
		if runner == nil {
			runner = DefaultPythonCoverageRunner
		}
		return NewPythonCoverageProvider(DefaultPythonCoverageConfig(), runner, logger)
	default: // "go" or fallback
		if runner == nil {
			runner = DefaultGoCoverageRunner
		}
		return NewGoCoverageProvider(runner, logger)
	}
}
```

Drop the now-unused `"context"` import if the file no longer references it (it previously did only via the type assertion). The final imports are just `"go.uber.org/zap"`.

- [ ] **Step 2: Simplify the session call site in run_phases_autotest.go**

In `internal/session/run_phases_autotest.go`, replace line ~53:

```go
	cov = autotest.NewCoverageProviderForLanguage(lang, autotest.DefaultGoCoverageRunner, rp.session.Logger)
```

with:

```go
	cov = autotest.NewCoverageProviderForLanguage(lang, nil, rp.session.Logger)
```

- [ ] **Step 3: Simplify the session call site in coverage.go**

In `internal/session/coverage.go`, replace line ~75:

```go
	provider := autotest.NewCoverageProviderForLanguage(lang, autotest.DefaultGoCoverageRunner, sess.Logger)
```

with:

```go
	provider := autotest.NewCoverageProviderForLanguage(lang, nil, sess.Logger)
```

- [ ] **Step 4: Build + run full suite**

Run: `go build ./...`
Expected: compiles (confirms the signature change is consistent across both session call sites).

Run: `PATH="$(go env GOPATH)/bin:$PATH" make check`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/autotest/provider_factory.go internal/session/run_phases_autotest.go internal/session/coverage.go
git -c user.name="binoctal" -c user.email="binoctal@gmail.com" \
  commit -m "refactor(autotest): strongly-type factory runner param; session passes nil"
```

---

### Task 5: Config constructor tests + final coverage confirmation

**Files:**
- Create: `internal/autotest/config_test.go`

**Interfaces:**
- Produces: tests for `DefaultNodeCoverageConfig`, `DefaultMochaCoverageConfig`, `DefaultPythonCoverageConfig`, `NodeCoverageConfig`, `PythonCoverageConfig` (all currently 0%).

- [ ] **Step 1: Write the tests**

Create `internal/autotest/config_test.go`:

```go
package autotest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultNodeCoverageConfig(t *testing.T) {
	cfg := DefaultNodeCoverageConfig()
	assert.Equal(t, []string{"npm", "test"}, cfg.TestCommand)
	assert.Equal(t, "coverage/coverage-final.json", cfg.OutputPath)
	assert.Equal(t, ProjectTypeNode, cfg.ProjectType)
	assert.True(t, cfg.Timeout > 0)
	assert.Contains(t, cfg.Env, "NODE_ENV=test")
}

func TestDefaultMochaCoverageConfig(t *testing.T) {
	cfg := DefaultMochaCoverageConfig()
	assert.Equal(t, []string{"npm", "test"}, cfg.TestCommand)
	assert.Equal(t, ProjectTypeMocha, cfg.ProjectType)
	assert.True(t, cfg.Timeout > 0)
}

func TestDefaultPythonCoverageConfig(t *testing.T) {
	cfg := DefaultPythonCoverageConfig()
	assert.Contains(t, cfg.TestCommand[0], "pytest")
	assert.Equal(t, "coverage.json", cfg.OutputPath)
	assert.Equal(t, ".coverage", cfg.DatabasePath)
	assert.Equal(t, ProjectTypePython, cfg.ProjectType)
}

func TestNodeCoverageConfig_Custom(t *testing.T) {
	cfg := NodeCoverageConfig([]string{"jest"}, "out.json", 2*time.Minute)
	assert.Equal(t, []string{"jest"}, cfg.TestCommand)
	assert.Equal(t, "out.json", cfg.OutputPath)
	assert.Equal(t, 2*time.Minute, cfg.Timeout)
	assert.Equal(t, ProjectTypeNode, cfg.ProjectType)
}

func TestPythonCoverageConfig_Custom(t *testing.T) {
	cfg := PythonCoverageConfig("python3", "cov.json", 3*time.Minute)
	assert.Equal(t, "cov.json", cfg.OutputPath)
	assert.Equal(t, ".coverage", cfg.DatabasePath)
	assert.Equal(t, 3*time.Minute, cfg.Timeout)
	assert.Equal(t, ProjectTypePython, cfg.ProjectType)
}
```

- [ ] **Step 2: Run tests — verify pass**

Run: `go test ./internal/autotest/ -run "TestDefault|TestNodeCoverageConfig_Custom|TestPythonCoverageConfig_Custom" -v`
Expected: PASS (5 tests).

- [ ] **Step 3: Confirm coverage gain**

Run:
```bash
go test -coverprofile=/tmp/autotest_final.out ./internal/autotest/ >/dev/null 2>&1
go tool cover -func=/tmp/autotest_final.out | tail -1
go tool cover -func=/tmp/autotest_final.out | grep -E "RunCoverage|DefaultNodeCoverageRunner|DefaultPythonCoverageRunner|DefaultMochaCoverageRunner|DefaultNodeCoverageConfig|DefaultMochaCoverageConfig|DefaultPythonCoverageConfig"
```
Expected: total ≥ 70%; `RunCoverage` for all three providers > 0%; the Default runners and config constructors > 0%. (`DefaultNodeCoverageRunner` / `DefaultMochaCoverageRunner` themselves stay 0% in this run since jest/mocha are absent — that's expected and recorded in the spec; their wiring is covered by the hermetic tests.)

- [ ] **Step 4: Final full gate**

Run: `PATH="$(go env GOPATH)/bin:$PATH" make check`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/autotest/config_test.go
git -c user.name="binoctal" -c user.email="binoctal@gmail.com" \
  commit -m "test(autotest): cover config constructors for Node/Mocha/Python"
```

---

## Self-Review

**1. Spec coverage:**
- Provider `run` field + constructor + `RunCoverage` rewrite (Node/Python/Mocha): Tasks 1–3. ✓
- Hermetic unit tests for every `RunCoverage` branch (nil config, nil runner, runner error, valid JSON, garbage): Tasks 1–3 Steps 6/9/5. ✓
- Wire up dead `DefaultNodeCoverageRunner` / `DefaultPythonCoverageRunner`: Task 1 Step 4, Task 2 Step 7 (factory cases). ✓
- Add `DefaultMochaCoverageRunner`: Task 3 Step 2. ✓
- Remove Python SQLite fallback (`parseCoverageData` + `parseSQLiteCoverage`): Task 2 Steps 3–4. ✓
- Delete dead Python helpers: Task 2 Step 5. ✓
- Factory strongly-typed, per-language Default selection, drop `interface{}`: Task 4 Step 1. ✓
- Session call sites pass nil: Task 4 Steps 2–3. ✓
- Config constructors covered: Task 5. ✓
- Behavior-equivalence guarantee: Default runners use the same commands as before (Node `npm test -- --coverage --coverageReporters=json`, Python `coverage run -m pytest`, Mocha mirrors Node). ✓
- Go provider untouched: no task modifies `coverage_go.go`. ✓

**2. Placeholder scan:** No TBD/TODO; every code step shows complete code; file paths and line numbers are exact; commit commands include the author override.

**3. Type consistency:** `coverageRunner` used uniformly for the `run` field; constructor signature `NewXxxCoverageProvider(cfg *CoverageConfig, run coverageRunner, logger *zap.Logger)` identical across Node/Python/Mocha and consistent with the factory calls; `NewCoverageProviderForLanguage`'s unnamed `func(context.Context, string) ([]byte, error)` is assignment-compatible with `coverageRunner` (named alias of the same underlying type); `jestValidJSON` defined once in Task 1 and reused in Task 3 (same package). `parseJestCoverage` / `parseJSONCoverage` / `parseIstanbulCoverage` names match the existing source.

**Refinements vs. the spec (spec to be updated to match):**
- **D2 (Python helpers):** plan deletes the *entire* `coverage_python_run_helpers.go` (including `determinePythonCommand`), not just four helpers — `DefaultPythonCoverageRunner` uses the `coverage` CLI and doesn't need python-interpreter selection.
- **Mocha routing:** `DetectLanguage` never returns `"mocha"`, so the factory has no mocha case; `DefaultMochaCoverageRunner` is not production-routed. It exists for symmetry and testability.
- **Timeout branch:** folded into the generic "runner returns error → wrapped error" path (matches Go; the runner owns timeout via `ctx`). No separate timeout-specialized branch in `RunCoverage`.
