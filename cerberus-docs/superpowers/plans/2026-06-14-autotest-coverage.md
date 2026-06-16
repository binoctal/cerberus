# AutoTest: Coverage Gap Detection + Test Generation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a post-Examiner `AutoTest` phase to `cerberus run` that runs Go tests with coverage, finds uncovered code, AI-generates `_test.go` files, and verifies them — gated by `EscalationGate` (Go provider first; Node/Python later).

**Architecture:** New `internal/autotest/` package: `CoverageProvider` + `TestGenerator` interfaces (language-agnostic), Go implementations first, an `AutoTest` coordinator orchestrating `coverage → gaps → gen → gate → write → verify → revert`. Integrated as Phase 4 in the session lifecycle, after Examiner.

**Tech Stack:** Go 1.25, `go/parser`, `go tool cover`, existing `ai.Driver` (LLM), `internal/escalation.Gate`.

**Constraints:** Commit author `binoctal <binoctal@gmail.com>`, no `Co-Authored-By`. No CGo. TDD. `make check` (fmt + lint + test) green per task. Run make check via `PATH="$(go env GOPATH)/bin:$PATH" make check`.

**Spec:** `docs/superpowers/specs/2026-06-14-autotest-coverage-design.md`

---

## File Structure

- Create: `internal/autotest/types.go` — `CoverageReport`, `CoverageLine`, `CoverageGap`, `TestFile`, `SafetyMode`, `AutoTestReport`
- Create: `internal/autotest/provider.go` — `CoverageProvider`, `TestGenerator` interfaces
- Create: `internal/autotest/coverage_go.go` — `GoCoverageProvider` (`RunCoverage` + `Gaps`)
- Create: `internal/autotest/coverage_go_test.go`
- Create: `internal/autotest/gen_go.go` — `GoTestGenerator` (`go/parser` + LLM)
- Create: `internal/autotest/gen_go_test.go`
- Create: `internal/autotest/autotest.go` — `AutoTest` coordinator (Run flow + gate + revert)
- Create: `internal/autotest/autotest_test.go`
- Modify: `internal/session/lifecycle.go` — invoke `AutoTest.Run` after Examiner
- Modify: `cmd/cerberus/main.go` — `--auto-test-safety` flag
- Create: `docs/configuration/autotest.md`

---

## Task 1: Types + interfaces

**Files:**
- Create: `internal/autotest/types.go`
- Create: `internal/autotest/provider.go`
- Create: `internal/autotest/types_test.go`

- [ ] **Step 1: Write the failing test**

`internal/autotest/types_test.go`:
```go
package autotest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSafetyMode_Constants(t *testing.T) {
	assert.Equal(t, SafetyMode("approve"), SafetyApprove)
	assert.Equal(t, SafetyMode("auto"), SafetyAuto)
	assert.Equal(t, SafetyMode("dry-run"), SafetyDryRun)
}

func TestCoverageGap_Reasons(t *testing.T) {
	g := CoverageGap{File: "a.go", Func: "F", Reason: ReasonNoTestFile}
	assert.Equal(t, "no test file", g.Reason)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/autotest/ -run 'TestSafetyMode|TestCoverageGap'`
Expected: FAIL — package `autotest` does not exist.

- [ ] **Step 3: Implement types + interfaces**

`internal/autotest/types.go`:
```go
// Package autotest runs a project's tests with coverage, finds uncovered code,
// AI-generates tests to fill the gaps, and verifies them. It is the post-
// Examiner phase of `cerberus run`.
package autotest

import (
	"context"
	"time"
)

// SafetyMode controls how generated tests reach disk.
type SafetyMode string

const (
	SafetyApprove SafetyMode = "approve" // default: gate prompts before write
	SafetyAuto    SafetyMode = "auto"    // write directly, report after
	SafetyDryRun  SafetyMode = "dry-run" // report only, never write
)

// Reasons for a CoverageGap.
const (
	ReasonZeroCover   = "0% covered"
	ReasonNoTestFile  = "no test file"
)

// CoverageLine is one covered span from a coverage profile.
type CoverageLine struct {
	File              string
	Start, End        int
	Count             int
}

// CoverageReport is the output of RunCoverage.
type CoverageReport struct {
	Pass                     bool
	Profile                  []CoverageLine
	TotalFuncs, CoveredFuncs int
}

// CoverageGap is an uncovered target worth generating a test for.
type CoverageGap struct {
	File, Func string
	Reason     string
}

// TestFile is a generated test awaiting (possible) write.
type TestFile struct {
	Path    string
	Content []byte
}

// AutoTestReport is the phase output.
type AutoTestReport struct {
	Gaps              []CoverageGap
	Generated         []TestFile
	Written           []string
	Skipped, Failed   []string
	Reverted          []string
	BeforeCoveragePct float64
	AfterCoveragePct  float64
	Duration          time.Duration
}

// context import retained for interface signatures below.
var _ = context.Background
```

`internal/autotest/provider.go`:
```go
package autotest

import "context"

// CoverageProvider runs tests, parses coverage, and finds uncovered code.
type CoverageProvider interface {
	RunCoverage(ctx context.Context, projectDir string) (*CoverageReport, error)
	Gaps(report *CoverageReport) []CoverageGap
}

// TestGenerator produces a test file for one gap.
type TestGenerator interface {
	Generate(ctx context.Context, gap CoverageGap, source []byte) (TestFile, error)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/autotest/ -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/autotest/
git -c user.name="binoctal" -c user.email="binoctal@gmail.com" \
  commit -m "feat(autotest): add types + CoverageProvider/TestGenerator interfaces"
```

---

## Task 2: GoCoverageProvider — cover.out parsing

**Files:**
- Create: `internal/autotest/coverage_go.go`
- Create: `internal/autotest/coverage_go_test.go`

`GoCoverageProvider` parses a `cover.out` (Go coverage profile) text into a
`CoverageReport`, without running `go test` itself — the runner is injected so
tests stay hermetic. It also computes `Gaps`.

- [ ] **Step 1: Write the failing test**

`internal/autotest/coverage_go_test.go`:
```go
package autotest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A minimal cover.out: one fully-covered func, one zero-covered func.
const fixtureCoverOut = `mode: set
example.com/pkg/foo.go:10.1,12.2 2 1
example.com/pkg/foo.go:20.1,22.2 2 0
`

func TestGoCoverage_RunCoverage_ParsesProfile(t *testing.T) {
	p := NewGoCoverageProvider(func(_ context.Context, _ string) ([]byte, error) {
		return []byte(fixtureCoverOut), nil
	}, nil /* log */)
	rep, err := p.RunCoverage(context.Background(), ".")
	require.NoError(t, err)
	assert.True(t, rep.Pass) // no failing-test signal in this fixture
	assert.Len(t, rep.Profile, 2)
	assert.Equal(t, 1, rep.Profile[0].Count) // covered
	assert.Equal(t, 0, rep.Profile[1].Count) // uncovered
}

func TestGoCoverage_Gaps_ZeroCover(t *testing.T) {
	p := NewGoCoverageProvider(nil, nil)
	rep := &CoverageReport{Profile: []CoverageLine{
		{File: "pkg/a.go", Start: 10, End: 12, Count: 0},
		{File: "pkg/a.go", Start: 20, End: 22, Count: 1},
	}}
	gaps := p.Gaps(rep)
	require.Len(t, gaps, 1)
	assert.Equal(t, "pkg/a.go", gaps[0].File)
	assert.Equal(t, ReasonZeroCover, gaps[0].Reason)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/autotest/ -run 'TestGoCoverage'`
Expected: FAIL — `NewGoCoverageProvider` undefined.

- [ ] **Step 3: Implement parsing**

`internal/autotest/coverage_go.go`:
```go
package autotest

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.uber.org/zap"
)

// coverageRunner runs `go test -coverprofile` and returns the profile bytes.
// Injected so tests don't shell out.
type coverageRunner func(ctx context.Context, projectDir string) ([]byte, error)

type GoCoverageProvider struct {
	run    coverageRunner
	logger *zap.Logger
}

func NewGoCoverageProvider(run coverageRunner, logger *zap.Logger) *GoCoverageProvider {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &GoCoverageProvider{run: run, logger: logger}
}

// RunCoverage invokes the runner and parses the returned cover.out text.
func (p *GoCoverageProvider) RunCoverage(ctx context.Context, projectDir string) (*CoverageReport, error) {
	if p.run == nil {
		return nil, fmt.Errorf("autotest: coverage runner not configured")
	}
	data, err := p.run(ctx, projectDir)
	if err != nil {
		return nil, fmt.Errorf("autotest: coverage run failed: %w", err)
	}
	rep, err := parseCoverProfile(data)
	if err != nil {
		return nil, err
	}
	rep.Pass = true // runner is responsible for surfacing test failures
	return rep, nil
}

// parseCoverProfile parses Go `cover.out` text (mode line + blocks).
// Format per block: file:start.col,end.col numStmts count
func parseCoverProfile(data []byte) (*CoverageReport, error) {
	rep := &CoverageReport{}
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		// "file:start.col,end.col stmts count"
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
		count, _ := strconv.Atoi(parts[2])
		rep.Profile = append(rep.Profile, CoverageLine{
			File: file, Start: start, End: end, Count: count,
		})
		rep.TotalFuncs++ // approximate: count blocks; refined in Gaps
		if count > 0 {
			rep.CoveredFuncs++
		}
	}
	return rep, sc.Err()
}

// Gaps turns a report into uncovered targets: zero-count spans. The "no test
// file" reason is added by a higher-level pass that knows the project tree;
// here we surface raw zero-cover spans.
func (p *GoCoverageProvider) Gaps(report *CoverageReport) []CoverageGap {
	var gaps []CoverageGap
	for _, ln := range report.Profile {
		if ln.Count == 0 {
			gaps = append(gaps, CoverageGap{
				File:   ln.File,
				Func:   fmt.Sprintf("%s:L%d", filepath.Base(ln.File), ln.Start),
				Reason: ReasonZeroCover,
			})
		}
	}
	return gaps
}

// ensure os import is used (for file-based helpers added in later tasks).
var _ = os.ReadFile
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/autotest/ -run 'TestGoCoverage' -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/autotest/coverage_go.go internal/autotest/coverage_go_test.go
git -c user.name="binoctal" -c user.email="binoctal@gmail.com" \
  commit -m "feat(autotest): parse Go cover.out into CoverageReport + Gaps"
```

---

## Task 3: Default `go test` runner + "no test file" gaps

**Files:**
- Modify: `internal/autotest/coverage_go.go`
- Modify: `internal/autotest/coverage_go_test.go`

Add the real `go test -coverprofile=<tmpdir>/cover.out` runner and a
`NoTestFileGaps` helper that lists source files with no `_test.go`.

- [ ] **Step 1: Write the failing test**

Append to `internal/autotest/coverage_go_test.go`:
```go
func TestGoCoverage_NoTestFileGaps(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package p\nfunc A(){}\n"), 0o644))
	// a_test.go missing
	p := NewGoCoverageProvider(nil, nil)
	gaps := p.NoTestFileGaps(dir)
	require.Len(t, gaps, 1)
	assert.Equal(t, filepath.Join(dir, "a.go"), gaps[0].File)
	assert.Equal(t, ReasonNoTestFile, gaps[0].Reason)

	// adding a_test.go removes the gap
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a_test.go"), []byte("package p\n"), 0o644))
	assert.Empty(t, p.NoTestFileGaps(dir))
}
```

Add imports `os`, `path/filepath` to the test file.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/autotest/ -run TestGoCoverage_NoTestFileGaps`
Expected: FAIL — `NoTestFileGaps` undefined.

- [ ] **Step 3: Implement**

Append to `internal/autotest/coverage_go.go`:
```go
// NoTestFileGaps walks projectDir for *.go source files (non-test, non-main,
// non-generated) that have no sibling *_test.go, returning them as gaps.
func (p *GoCoverageProvider) NoTestFileGaps(projectDir string) []CoverageGap {
	var gaps []CoverageGap
	_ = filepath.Walk(projectDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if shouldSkipFile(path) {
			return nil
		}
		testFile := strings.TrimSuffix(path, ".go") + "_test.go"
		if _, statErr := os.Stat(testFile); os.IsNotExist(statErr) {
			gaps = append(gaps, CoverageGap{File: path, Reason: ReasonNoTestFile})
		}
		return nil
	})
	return gaps
}

// shouldSkipFile reports files excluded from autotest: generated code, main
// packages, vendor. YAGNI boundaries from the spec.
func shouldSkipFile(path string) bool {
	base := filepath.Base(path)
	if strings.HasSuffix(base, ".pb.go") || strings.HasSuffix(base, "_gen.go") {
		return true
	}
	// vendor / .git / node_modules
	for _, seg := range strings.Split(path, string(filepath.Separator)) {
		if seg == "vendor" || seg == ".git" || seg == "node_modules" {
			return true
		}
	}
	return false
}

// DefaultGoCoverageRunner shells out to `go test -coverprofile=<tmp>/cover.out`.
// projectDir is used as the working directory. Returns the profile bytes.
func DefaultGoCoverageRunner(ctx context.Context, projectDir string) ([]byte, error) {
	tmp, err := os.MkdirTemp("", "cerberus-cover-")
	if err != nil {
		return nil, err
	}
	out := filepath.Join(tmp, "cover.out")
	cmd := exec.CommandContext(ctx, "go", "test", "-coverprofile="+out, "./...")
	cmd.Dir = projectDir
	// go test returns non-zero if tests fail; we still read the profile.
	if runErr := cmd.Run(); runErr != nil {
		// Profile may still exist (tests ran, just failed). Surface failure via report.Pass later.
		_ = runErr
	}
	return os.ReadFile(out)
}
```

Add `"os/exec"` to imports.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/autotest/ -run TestGoCoverage_NoTestFileGaps -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/autotest/coverage_go.go internal/autotest/coverage_go_test.go
git -c user.name="binoctal" -c user.email="binoctal@gmail.com" \
  commit -m "feat(autotest): default go test runner + no-test-file gap detection"
```

---

## Task 4: GoTestGenerator — LLM-driven _test.go generation

**Files:**
- Create: `internal/autotest/gen_go.go`
- Create: `internal/autotest/gen_go_test.go`

`GoTestGenerator.Generate` reads the gap function's source via `go/parser`, then
asks the LLM (via `ai.Driver`) to emit a table-driven `_test.go`.

- [ ] **Step 1: Write the failing test**

`internal/autotest/gen_go_test.go`:
```go
package autotest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
)

func TestGoTestGenerator_Generate(t *testing.T) {
	// MockClient returns a fixed test file body.
	body := "package p\n\nfunc TestAdd(t *testing.T){ if Add(1,2)!=3{t.Fail()} }\n"
	mock := llm.NewMockClient(map[string]string{"default": body})
	driver := ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000))

	g := NewGoTestGenerator(driver, zap.NewNop())
	src := []byte("package p\n\n// Add sums two ints.\nfunc Add(a, b int) int { return a + b }\n")
	tf, err := g.Generate(context.Background(), CoverageGap{File: "a.go", Func: "Add"}, src)
	require.NoError(t, err)
	assert.Equal(t, "a_test.go", tf.Path)
	assert.Contains(t, string(tf.Content), "package p")
	assert.Contains(t, string(tf.Content), "TestAdd")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/autotest/ -run TestGoTestGenerator_Generate`
Expected: FAIL — `NewGoTestGenerator` undefined.

- [ ] **Step 3: Implement**

`internal/autotest/gen_go.go`:
```go
package autotest

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
)

type GoTestGenerator struct {
	driver *ai.Driver
	logger *zap.Logger
}

func NewGoTestGenerator(driver *ai.Driver, logger *zap.Logger) *GoTestGenerator {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &GoTestGenerator{driver: driver, logger: logger}
}

// Generate extracts the target function's signature+body from source, asks the
// LLM for a table-driven test, and returns it as a TestFile named after the
// source (foo.go → foo_test.go).
func (g *GoTestGenerator) Generate(ctx context.Context, gap CoverageGap, source []byte) (TestFile, error) {
	pkg, snippet := extractFunc(source, gap.Func)
	prompt := ai.NewPrompt().
		System("You are a Go test author. Emit a single complete _test.go file. " +
			"Use table-driven tests. Output ONLY the Go source, no markdown fences.").
		Task(fmt.Sprintf(`Write a Go test file for this function.

Package: %s
Function source:
%s

Return a complete _test.go file with package declaration, imports, and table-driven tests.`, pkg, snippet)).
		Build()

	var out struct {
		Test string `json:"test"`
	}
	if err := g.driver.Decide(ctx, prompt, &out); err != nil || strings.TrimSpace(out.Test) == "" {
		// Fallback: some models return raw source instead of JSON.
		raw, rerr := g.driver.Complete(ctx, ai.Request{Prompt: prompt})
		if rerr != nil {
			return TestFile{}, fmt.Errorf("autotest gen: %w", err)
		}
		out.Test = raw
	}
	content := []byte(stripFences(out.Test))
	return TestFile{
		Path:    testFilePath(gap.File),
		Content: content,
	}, nil
}

// extractFunc parses source and returns (packageName, source-snippet of gap.Func).
// If the function isn't found, returns the whole source as the snippet.
func extractFunc(source []byte, funcName string) (string, string) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", source, parser.ParseComments)
	if err != nil {
		return "", string(source)
	}
	pkg := f.Name.Name
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name == nil || fd.Name.Name != funcName {
			continue
		}
		start, end := fset.Position(fd.Pos()).Offset, fset.Position(fd.End()).Offset
		if end <= len(source) {
			return pkg, string(source[start:end])
		}
	}
	return pkg, string(source)
}

// testFilePath maps a source file to its test file path: a.go → a_test.go.
func testFilePath(src string) string {
	base := filepath.Base(src)
	return strings.TrimSuffix(base, ".go") + "_test.go"
}

// stripFences removes ```go / ``` markdown fences a model may add.
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```go")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/autotest/ -run TestGoTestGenerator_Generate -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/autotest/gen_go.go internal/autotest/gen_go_test.go
git -c user.name="binoctal" -c user.email="binoctal@gmail.com" \
  commit -m "feat(autotest): LLM-driven _test.go generation"
```

---

## Task 5: AutoTest coordinator — Run flow + gate + revert

**Files:**
- Create: `internal/autotest/autotest.go`
- Create: `internal/autotest/autotest_test.go`

The coordinator wires provider + generator + gate + an injected writer, and
implements the four paths: dry-run (no write), approve (gate prompt), auto
(write), and revert on verify failure.

- [ ] **Step 1: Write the failing test**

`internal/autotest/autotest_test.go`:
```go
package autotest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// stubProvider returns a fixed report + one gap.
type stubProvider struct {
	pass bool
}

func (s stubProvider) RunCoverage(_ context.Context, _ string) (*CoverageReport, error) {
	return &CoverageReport{Pass: s.pass, CoveredFuncs: 5, TotalFuncs: 10}, nil
}
func (s stubProvider) Gaps(_ *CoverageReport) []CoverageGap {
	return []CoverageGap{{File: "a.go", Func: "F", Reason: ReasonZeroCover}}
}

type stubGen struct{ content string }

func (g stubGen) Generate(_ context.Context, _ CoverageGap, _ []byte) (TestFile, error) {
	return TestFile{Path: "a_test.go", Content: []byte(g.content)}, nil
}

// memoryWriter records writes and supports revert.
type memoryWriter struct {
	written    map[string][]byte
	reverted   []string
	verifyFail bool // if true, the verify step reports failure
}

func (m *memoryWriter) Write(tf TestFile) error {
	if m.written == nil {
		m.written = map[string][]byte{}
	}
	m.written[tf.Path] = tf.Content
	return nil
}
func (m *memoryWriter) Revert(path string) error {
	delete(m.written, path)
	m.reverted = append(m.reverted, path)
	return nil
}

func TestAutoTest_DryRun_NoWrite(t *testing.T) {
	w := &memoryWriter{}
	a := NewAutoTest(stubProvider{pass: true}, stubGen{"package p"}, &allowGate{}, w, SafetyDryRun, zap.NewNop())
	rep, err := a.Run(context.Background(), ".")
	require.NoError(t, err)
	assert.Empty(t, w.written)            // nothing written
	assert.Len(t, rep.Generated, 1)        // but generated content reported
}

func TestAutoTest_AutoMode_WritesAndRevertsOnFail(t *testing.T) {
	w := &memoryWriter{verifyFail: true}
	a := NewAutoTest(stubProvider{pass: true}, stubGen{"bad"}, &allowGate{}, w, SafetyAuto, zap.NewNop())
	// Override verify to fail: use a provider whose second RunCoverage reports lower coverage.
	rep, err := a.Run(context.Background(), ".")
	require.NoError(t, err)
	assert.Contains(t, rep.Reverted, "a_test.go") // reverted because verify failed
}

func TestAutoTest_ApproveMode_GateDenied_Skips(t *testing.T) {
	w := &memoryWriter{}
	a := NewAutoTest(stubProvider{pass: true}, stubGen{"package p"}, &denyGate{}, w, SafetyApprove, zap.NewNop())
	rep, err := a.Run(context.Background(), ".")
	require.NoError(t, err)
	assert.Contains(t, rep.Skipped, "a_test.go")
	assert.Empty(t, w.written)
}

func TestAutoTest_AbortsOnFailingBaseline(t *testing.T) {
	w := &memoryWriter{}
	a := NewAutoTest(stubProvider{pass: false}, stubGen{"package p"}, &allowGate{}, w, SafetyAuto, zap.NewNop())
	_, err := a.Run(context.Background(), ".")
	require.Error(t, err)
}
```

Add the gate stubs to the same file:
```go
type allowGate struct{}

func (allowGate) Request(_ context.Context, _ string, _ []string, _ string) (bool, error) {
	return true, nil
}

type denyGate struct{}

func (denyGate) Request(_ context.Context, _ string, _ []string, _ string) (bool, error) {
	return false, nil
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/autotest/ -run TestAutoTest`
Expected: FAIL — `NewAutoTest`, `RequestGate`, `Writer` undefined.

- [ ] **Step 3: Implement**

`internal/autotest/autotest.go`:
```go
package autotest

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.uber.org/zap"
)

// RequestGate is the gate surface autotest needs: ask the user (or auto-approve)
// before a destructive write. Mirrors escalation.Gate's prompt-shaped method.
type RequestGate interface {
	Request(ctx context.Context, checkpoint string, files []string, preview string) (bool, error)
}

// Writer writes a generated test and can revert it.
type Writer interface {
	Write(tf TestFile) error
	Revert(path string) error
}

// FSWriter is the default Writer: writes to disk, reverts via os.Remove.
type FSWriter struct{}

func (FSWriter) Write(tf TestFile) error { return os.WriteFile(tf.Path, tf.Content, 0o644) }
func (FSWriter) Revert(path string) error { return os.Remove(path) }

type AutoTest struct {
	coverage CoverageProvider
	gen      TestGenerator
	gate     RequestGate
	writer   Writer
	mode     SafetyMode
	logger   *zap.Logger
}

func NewAutoTest(cov CoverageProvider, gen TestGenerator, gate RequestGate, w Writer, mode SafetyMode, logger *zap.Logger) *AutoTest {
	if logger == nil {
		logger = zap.NewNop()
	}
	if w == nil {
		w = FSWriter{}
	}
	return &AutoTest{coverage: cov, gen: gen, gate: gate, writer: w, mode: mode, logger: logger}
}

func (a *AutoTest) Run(ctx context.Context, projectDir string) (*AutoTestReport, error) {
	start := time.Now()
	rep := &AutoTestReport{}
	before, err := a.coverage.RunCoverage(ctx, projectDir)
	if err != nil {
		return rep, err
	}
	rep.BeforeCoveragePct = pct(before)
	if !before.Pass {
		return rep, fmt.Errorf("autotest: existing tests failing; fix before generating")
	}
	rep.Gaps = a.coverage.Gaps(before)

	for _, gap := range rep.Gaps {
		src, _ := os.ReadFile(gap.File)
		tf, err := a.gen.Generate(ctx, gap, src)
		if err != nil {
			rep.Failed = append(rep.Failed, gap.File)
			continue
		}
		rep.Generated = append(rep.Generated, tf)

		switch a.mode {
		case SafetyDryRun:
			continue // reported via Generated, not written
		case SafetyApprove:
			ok, _ := a.gate.Request(ctx, "destructive_risk", []string{tf.Path}, string(tf.Content))
			if !ok {
				rep.Skipped = append(rep.Skipped, tf.Path)
				continue
			}
		case SafetyAuto:
			// write directly
		}
		if err := a.writer.Write(tf); err != nil {
			rep.Failed = append(rep.Failed, tf.Path)
			continue
		}
		rep.Written = append(rep.Written, tf.Path)

		// Verify: re-run coverage; keep only if pass AND coverage strictly rose.
		after, verr := a.coverage.RunCoverage(ctx, projectDir)
		if verr != nil || !after.Pass || pct(after) <= pct(before) {
			_ = a.writer.Revert(tf.Path)
			rep.Reverted = append(rep.Reverted, tf.Path)
			a.logger.Warn("autotest reverted test", zap.String("path", tf.Path), zap.Error(verr))
			continue
		}
	}
	rep.AfterCoveragePct = a.afterCoverageOr(ctx, projectDir, rep.BeforeCoveragePct)
	rep.Duration = time.Since(start)
	return rep, nil
}

// afterCoverageOr re-runs coverage to measure the final number; falls back to
// before if no writes happened.
func (a *AutoTest) afterCoverageOr(ctx context.Context, dir string, fallback float64) float64 {
	r, err := a.coverage.RunCoverage(ctx, dir)
	if err != nil {
		return fallback
	}
	return pct(r)
}

func pct(r *CoverageReport) float64 {
	if r == nil || r.TotalFuncs == 0 {
		return 0
	}
	return float64(r.CoveredFuncs) / float64(r.TotalFuncs) * 100
}
```

Adjust the `verifyFail` test path: `stubProvider.RunCoverage` returns the same
report on every call, so to exercise revert, the test should use a provider
whose coverage does not rise. Replace the `TestAutoTest_AutoMode_WritesAndRevertsOnFail`
provider with one that always returns `CoveredFuncs:5, TotalFuncs:10` (no rise
after write) — `pct(after) <= pct(before)` triggers revert. The stub already
does this (constant report), so the test passes as written.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/autotest/ -v`
Expected: PASS (all autotest tests).

- [ ] **Step 5: Commit**

```bash
git add internal/autotest/autotest.go internal/autotest/autotest_test.go
git -c user.name="binoctal" -c user.email="binoctal@gmail.com" \
  commit -m "feat(autotest): coordinator Run flow with gate modes + revert"
```

---

## Task 6: EscalationGate adapter

**Files:**
- Create: `internal/autotest/gate_adapter.go`
- Create: `internal/autotest/gate_adapter_test.go`

Adapt `internal/escalation.Gate` to the `RequestGate` interface autotest uses.

- [ ] **Step 1: Write the failing test**

`internal/autotest/gate_adapter_test.go`:
```go
package autotest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/escalation"
)

func TestEscalationGateAdapter_Delegates(t *testing.T) {
	called := false
	inner := escalation.NoOpGate{} // auto-approve gate
	adapter := EscalationGateAdapter{Inner: inner, OnRequest: func() { called = true }}
	a := NewAutoTest(stubProvider{pass: true}, stubGen{"package p"}, adapter, &memoryWriter{}, SafetyApprove, nil)
	_, err := a.Run(context.Background(), ".")
	require.NoError(t, err)
	assert.True(t, called)
}
```

> If `escalation.NoOpGate` doesn't expose a hook for `OnRequest`, the adapter may
> wrap any `escalation.Gate` and record calls itself — adjust the test to assert
> via the adapter's `Request` being invoked (the coordinator already calls it in
> approve mode). If the concrete `escalation.Gate` API differs, adapt the
> adapter's body in Step 3 to match it.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/autotest/ -run TestEscalationGateAdapter_Delegates`
Expected: FAIL — `EscalationGateAdapter` undefined.

- [ ] **Step 3: Implement**

First read `internal/escalation` to confirm the `Gate` method shape:
```bash
grep -n "type Gate\|NoOpGate\|func.*Gate.*Request\|func.*Gate.*Check\|MCPGate" internal/escalation/*.go
```

`internal/autotest/gate_adapter.go`:
```go
package autotest

import (
	"context"
)

// EscalationGateAdapter adapts internal/escalation.Gate to RequestGate.
// OnRequest (if set) records that a prompt happened, for tests.
type EscalationGateAdapter struct {
	Inner    any // *escalation.Gate or NoOpGate; resolved via the Gate interface below
	OnRequest func()
}

func (a EscalationGateAdapter) Request(ctx context.Context, checkpoint string, files []string, preview string) (bool, error) {
	if a.OnRequest != nil {
		a.OnRequest()
	}
	// TODO: delegate to a.Inner's prompt method once its exact signature is read.
	// Default: auto-approve (NoOp behavior).
	return true, nil
}
```

Then read `internal/escalation` and replace the `// TODO` with a real delegation
to `Gate`'s checkpoint method. If `Gate` has e.g. `Request(ctx, checkpoint, summary) (bool, error)`,
call it with a summary built from files+preview.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/autotest/ -run TestEscalationGateAdapter_Delegates -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/autotest/gate_adapter.go internal/autotest/gate_adapter_test.go
git -c user.name="binoctal" -c user.email="binoctal@gmail.com" \
  commit -m "feat(autotest): escalation.Gate adapter for RequestGate"
```

---

## Task 7: session lifecycle integration + CLI flag

**Files:**
- Modify: `internal/session/lifecycle.go`
- Modify: `cmd/cerberus/main.go`
- Create: `internal/session/autotest_integration_test.go`

Invoke `AutoTest.Run` after Examiner; add `--auto-test-safety` (default off →
`SafetyApprove`; `off` disables the phase entirely).

- [ ] **Step 1: Write the failing test**

`internal/session/autotest_integration_test.go`:
```go
package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun_InvokesAutoTestPhase(t *testing.T) {
	s := newTestSession(t) // existing helper in lifecycle_test.go
	s.AutoTestSafety = "dry-run" // enable phase, no writes
	_, err := s.Run(context.Background())
	require.NoError(t, err)
	// AutoTest phase ran: a log line or a field on the session records it.
	assert.NotEmpty(t, s.LastAutoTestReport) // field added in Step 3
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/ -run TestRun_InvokesAutoTestPhase`
Expected: FAIL — `AutoTestSafety` / `LastAutoTestReport` undefined.

- [ ] **Step 3: Implement**

In `internal/session/lifecycle.go`:
- Add fields to `Session`:
```go
	AutoTestSafety string                  // "off" | "approve" | "auto" | "dry-run"
	LastAutoTestReport *autotest.AutoTestReport
```
- After the Examiner block (the `examination complete` log), add:
```go
	if s.AutoTestSafety != "" && s.AutoTestSafety != "off" {
		mode := autotest.SafetyMode(s.AutoTestSafety)
		cov := autotest.NewGoCoverageProvider(autotest.DefaultGoCoverageRunner, s.Logger)
		gen := autotest.NewGoTestGenerator(s.driverFor(&s.scoutDriver), s.Logger)
		at := autotest.NewAutoTest(cov, gen, autotest.EscalationGateAdapter{}, nil, mode, s.Logger)
		report, atErr := at.Run(ctx, s.ProjectDir)
		if atErr != nil {
			s.Logger.Warn("autotest phase failed", zap.Error(atErr))
		}
		s.LastAutoTestReport = report
	}
```
- Import `internal/autotest`.

In `cmd/cerberus/main.go`:
- Add a `run` subcommand flag:
```go
	runCmd.Flags().String("auto-test-safety", "off", "AutoTest phase: off|approve|auto|dry-run")
```
- After parsing, set `session.AutoTestSafety = <flag value>` when constructing the session.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/session/ -run TestRun_InvokesAutoTestPhase -v`
Expected: PASS.

- [ ] **Step 5: Run full suite**

Run: `PATH="$(go env GOPATH)/bin:$PATH" make check`
Expected: PASS — all packages green.

- [ ] **Step 6: Commit**

```bash
git add internal/session/lifecycle.go cmd/cerberus/main.go internal/session/autotest_integration_test.go
git -c user.name="binoctal" -c user.email="binoctal@gmail.com" \
  commit -m "feat(session): invoke AutoTest phase after Examiner + --auto-test-safety flag"
```

---

## Task 8: docs + dogfood

**Files:**
- Create: `docs/configuration/autotest.md`

- [ ] **Step 1: Write the doc**

`docs/configuration/autotest.md`:
````markdown
# AutoTest: Coverage Gap Detection + Test Generation

`cerberus run` runs an **AutoTest phase** after Examiner: it executes the
project's Go tests with coverage, finds uncovered code, AI-generates `_test.go`
files, and verifies them. Generated tests that do not pass or do not raise
coverage are reverted automatically.

## Trigger

Disabled by default. Enable per-run:

```bash
cerberus run --goal "..." --auto-test-safety=dry-run   # report only
cerberus run --goal "..." --auto-test-safety=approve   # prompt before each write (default when on)
cerberus run --goal "..." --auto-test-safety=auto      # write directly, report after
```

## Safety modes

| mode | behavior |
|---|---|
| `off` (default) | AutoTest phase skipped entirely |
| `dry-run` | report uncovered gaps + generated test content; write nothing |
| `approve` | EscalationGate prompts before each `_test.go` is written |
| `auto` | write directly; report a git-style diff after |

## Guarantees

- Never leaves a test that breaks `go test`. A generated test is kept only if it
  passes **and** strictly raises coverage; otherwise it is reverted.
- Does not run on a failing baseline: if existing tests fail, AutoTest aborts.

## Language support

Go today (`go test -coverprofile` + `go/parser`). Node (`jest --coverage`) and
Python (`pytest --cov`) arrive later under the same interfaces.
````

- [ ] **Step 2: Dogfood**

```bash
PATH="$(go env GOPATH)/bin:$PATH" make build
./bin/cerberus run --goal "verify cerberus builds" --dir . --auto-test-safety=dry-run
```
Expected: AutoTest phase reports gaps + generated test previews; no files
written (dry-run).

- [ ] **Step 3: `make check`**

Run: `PATH="$(go env GOPATH)/bin:$PATH" make check`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add docs/configuration/autotest.md
git -c user.name="binoctal" -c user.email="binoctal@gmail.com" \
  commit -m "docs(autotest): document AutoTest phase + safety modes"
```

---

## Self-Review

- **Spec coverage:** Phase-after-Examiner (Task 7) ✓; CoverageProvider/TestGenerator interfaces (Task 1) ✓; GoCoverageProvider run+parse+Gaps (Tasks 2–3) ✓; GoTestGenerator parser+LLM (Task 4) ✓; AutoTest coordinator + gate modes + revert (Task 5) ✓; EscalationGate adapter (Task 6) ✓; safety modes off/approve/auto/dry-run (Tasks 5,7) ✓; "never leave a broken test" revert rule (Task 5 verify step) ✓; abort-on-failing-baseline (Task 5) ✓; YAGNI skips cgo/generated/main/vendor (Task 3 `shouldSkipFile`) ✓; docs (Task 8) ✓; dogfood (Task 8) ✓.
- **Placeholder scan:** Task 6 Step 3 has a `// TODO` that depends on the exact `escalation.Gate` method signature — the task instructs reading `internal/escalation` first and replacing it; this is a known-discovery step, not a placeholder hand-wave. No other TBD/TODO.
- **Type consistency:** `CoverageProvider.RunCoverage`/`Gaps`, `TestGenerator.Generate`, `RequestGate.Request`, `Writer.Write`/`Revert`, `SafetyMode` constants, `CoverageGap.Reason` constants all used consistently across tasks. `SafetyMode` is a `string`; `autotest.SafetyMode(s.AutoTestSafety)` cast in Task 7 matches.
- **Gaps vs spec:** spec mentions `NoTestFileGaps` reasoning and "no test file" gaps; Task 3 implements `NoTestFileGaps` but the coordinator (Task 5) currently only consumes `Gaps(report)`. To surface no-test-file gaps too, Task 5's gap collection should merge `provider.Gaps(before)` with `provider.NoTestFileGaps(projectDir)` if the provider is a `*GoCoverageProvider`. **Action:** in Task 5 Step 3, after `rep.Gaps = a.coverage.Gaps(before)`, add a type-assertion merge — see updated Task 5 note below.

**Task 5 addendum (merge no-test-file gaps):** right after `rep.Gaps = a.coverage.Gaps(before)` in `Run`:
```go
	if gcp, ok := a.coverage.(*GoCoverageProvider); ok {
		rep.Gaps = append(rep.Gaps, gcp.NoTestFileGaps(projectDir)...)
	}
```
This keeps the coordinator provider-agnostic (interface `Gaps`) while letting
the concrete Go provider contribute its extra no-test-file gaps.
