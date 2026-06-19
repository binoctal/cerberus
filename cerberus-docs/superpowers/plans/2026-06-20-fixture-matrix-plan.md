# Fixture Matrix Implementation Plan (Plan 2)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Validate cerberus can test 4 external fixture projects (Go/Node/Python/SaaS) with mock LLM + real executor/autotest, verifying universality (not just self-test) and exercising the never-run Node/Python autotest code paths.

**Architecture:** 4 minimal fixture projects under `test/fixtures/`, each with a Go integration test that runs `cerberus run` with a mock LLM (preset responses at LLM call points) but real executors (go test / npm test / pytest / HTTP) and real autotest (coverage parsing + generator). A shared mock helper centralizes LLM responses. AutoTest language routing is extended if the main flow only handles Go today.

**Tech Stack:** Go 1.25, cerberus internal packages, `llm.NewMockClient`, Node/jest, Python/pytest (CI skips if toolchain absent).

## Global Constraints

- Module: `github.com/binoctal/cerberus`, Go 1.25, no CGo.
- Commit author: `binoctal <binoctal@gmail.com>`, no Co-Authored-By.
- Comments and commit messages in English.
- Follow existing patterns: `llm.NewMockClient`, `session.NewSession`, `testStoreWithMigrations` (in `internal/session/lifecycle_test.go`).
- `make check` (fmt + lint + test) must pass after each task.
- Docs only in `cerberus-docs/`, never `docs/`.
- Node/Python fixture tests must **skip gracefully** if the toolchain (`npm`/`python3`) is absent (use `exec.LookPath` or `t.Skip`), so CI without Node/Python doesn't fail.

## File Structure

- Create `test/fixtures/mock_helper_test.go` — shared mock LLM response builder.
- Create `test/fixtures/go-lib/go.mod` + `math.go` — Go fixture project.
- Create `test/fixtures/go_lib_fixture_test.go` — Go fixture integration test.
- Create `test/fixtures/saas-api/server.go` — SaaS fixture (httptest server).
- Create `test/fixtures/saas_api_fixture_test.go` — SaaS integration test.
- Modify `internal/autotest/` — language routing (if AutoTest only handles Go).
- Create `test/fixtures/node-app/` — Node fixture (lib.js, package.json, jest config).
- Create `test/fixtures/node_app_fixture_test.go` — Node integration test.
- Create `test/fixtures/python-pkg/` — Python fixture (math.py, requirements.txt).
- Create `test/fixtures/python_pkg_fixture_test.go` — Python integration test.

---

### Task 1: Shared mock helper + Go fixture

**Files:**
- Create: `test/fixtures/mock_helper_test.go`
- Create: `test/fixtures/go-lib/go.mod`, `test/fixtures/go-lib/math.go`
- Create: `test/fixtures/go_lib_fixture_test.go`

**Interfaces:**
- Produces: `fixtures.MockResponses(targetFile string) map[string]string` — mock LLM responses parameterized by the fixture's target file (used by all fixture tests).
- Produces: `test/fixtures/go-lib/` — a Go module with an uncovered `Add` function.

- [ ] **Step 1: Create Go fixture project**

`test/fixtures/go-lib/go.mod`:
```
module github.com/binoctal/cerberus/test/fixtures/go-lib

go 1.25
```

`test/fixtures/go-lib/math.go`:
```go
// Package math provides simple math functions for fixture testing.
package math

// Add returns the sum of two integers. Intentionally has no test file
// (no math_test.go) so AutoTest detects it as an uncovered gap.
func Add(a, b int) int {
	return a + b
}

// Sub returns the difference. Also uncovered.
func Sub(a, b int) int {
	return a - b
}
```

- [ ] **Step 2: Write the shared mock helper**

`test/fixtures/mock_helper_test.go`:
```go
package fixtures

// MockResponses returns mock LLM responses parameterized by the fixture's
// target file. The same response set works for Scout.Plan, BuildCoverageContract,
// Agent.Steer, Examiner.Judge, and AutoTest generator — all get a permissive
// JSON that lets the real executor/coverage/generator code run.
func MockResponses(targetFile string) map[string]string {
	planAndContract := `{
		"cases": [{"id":"tc-1","name":"test","target":"` + targetFile + `","action":"file_read","expectation":"reads ok","priority":0.9}],
		"depth": "standard",
		"scope": ["` + targetFile + `"],
		"path_types": ["happy"],
		"error_scope": ["4xx"],
		"boundaries": ["empty"],
		"priorities": {},
		"coverage_gate": {"module":"` + targetFile + `","line_threshold":0.5}
	}`
	return map[string]string{
		"default": planAndContract,
	}
}
```

- [ ] **Step 3: Write the Go fixture integration test (failing)**

`test/fixtures/go_lib_fixture_test.go`:
```go
package fixtures

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
)

func TestGoLibFixture(t *testing.T) {
	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	require.NoError(t, store.RunMigrations(context.Background(), s.DB(), "../../migrations"))

	cfg := project.DefaultConfig()
	cfg.Settings.Mode = "local"
	mockClient := llm.NewMockClient(MockResponses("math.go"))
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(50000, 5000))

	sess, err := session.NewSession(context.Background(), session.SessionConfig{
		Mode:       session.ModeRun,
		Goal:       "test go-lib fixture",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     zap.NewNop(),
		ProjectDir: "test/fixtures/go-lib",
	})
	require.NoError(t, err)

	err = sess.Run(context.Background())
	require.NoError(t, err, "session should complete")

	assert.NotNil(t, sess.Contract, "coverage contract should be produced")
	assert.NotEmpty(t, sess.ID, "session ID assigned")
}
```

- [ ] **Step 4: Run test — verify it passes (or fix path issues)**

Run: `go test ./test/fixtures/ -run TestGoLibFixture -v`
Expected: PASS (session completes, contract produced). If path issues, fix `ProjectDir` / `migrations` relative paths.

- [ ] **Step 5: Commit**

```bash
git add test/fixtures/
git commit -m "test(fixtures): add Go fixture + shared mock helper"
```

---

### Task 2: SaaS fixture

**Files:**
- Create: `test/fixtures/saas-api/server.go`
- Create: `test/fixtures/saas_api_fixture_test.go`

**Interfaces:**
- Produces: a Go httptest server fixture + integration test verifying HTTP executor + SaaS mode.

- [ ] **Step 1: Create SaaS fixture (httptest server in test)**

`test/fixtures/saas_api_fixture_test.go`:
```go
package fixtures

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
)

func TestSaaSAPIFixture(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/users":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"id":"1"}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	require.NoError(t, store.RunMigrations(context.Background(), s.DB(), "../../migrations"))

	cfg := project.DefaultConfig()
	cfg.Settings.Mode = "" // SaaS mode (services has URL)
	cfg.Services = []project.Service{{Name: "api", URL: srv.URL, Health: "/health"}}
	mockClient := llm.NewMockClient(MockResponses("/health"))
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(50000, 5000))
	_ = driver // session uses mockClient directly

	sess, err := session.NewSession(context.Background(), session.SessionConfig{
		Mode:       session.ModeRun,
		Goal:       "test SaaS fixture",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     zap.NewNop(),
		ProjectDir: ".",
	})
	require.NoError(t, err)

	err = sess.Run(context.Background())
	require.NoError(t, err, "SaaS session should complete")

	assert.NotNil(t, sess.Contract, "contract produced in SaaS mode")
}
```

- [ ] **Step 2: Run test**

Run: `go test ./test/fixtures/ -run TestSaaSAPIFixture -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add test/fixtures/
git commit -m "test(fixtures): add SaaS fixture with httptest server"
```

---

### Task 3: AutoTest language routing

**Files:**
- Modify: `internal/session/run_phases_autotest.go` (currently hardcodes `NewGoCoverageProvider`)
- Modify: `internal/autotest/autotest_factory.go` or similar (add provider selection by language)
- Test: `internal/autotest/routing_test.go` (new)

**Interfaces:**
- Produces: language detection + provider routing so AutoTest handles Go/Node/Python (not just Go).

**IMPORTANT:** This task requires **investigating first** how AutoTest currently selects its coverage provider. Read `internal/session/run_phases_autotest.go` (look for `NewGoCoverageProvider`) and `internal/autotest/autotest_factory.go`. If the main flow hardcodes Go, add a detector + router.

- [ ] **Step 1: Investigate current routing**

Read `internal/session/run_phases_autotest.go:18` — it likely calls `autotest.NewGoCoverageProvider(...)`. This means Node/Python fixtures can't use AutoTest. Document what you find.

- [ ] **Step 2: Write the failing test for language detection**

`internal/autotest/routing_test.go`:
```go
package autotest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectLanguage(t *testing.T) {
	assert.Equal(t, "go", detectLanguage("test/fixtures/go-lib/math.go", nil))
	assert.Equal(t, "node", detectLanguage("test/fixtures/node-app/lib.js", map[string]bool{"package.json": true}))
	assert.Equal(t, "python", detectLanguage("test/fixtures/python-pkg/math.py", map[string]bool{"requirements.txt": true}))
}

func TestSelectProvider(t *testing.T) {
	// detectLanguage → provider name (string, not concrete type — factory resolves later)
	assert.Equal(t, "go", providerForLanguage("go"))
	assert.Equal(t, "node", providerForLanguage("node"))
	assert.Equal(t, "python", providerForLanguage("python"))
}
```

- [ ] **Step 3: Run test — expect FAIL**

Run: `go test ./internal/autotest/ -run TestDetectLanguage`
Expected: FAIL (undefined).

- [ ] **Step 4: Implement language detection + provider routing**

Add to `internal/autotest/` (new file `language.go` or in factory):
```go
package autotest

import (
	"path/filepath"
	"strings"
)

// detectLanguage infers the project language from source file extensions
// and marker files (package.json for Node, requirements.txt for Python).
func detectLanguage(sourceFile string, markers map[string]bool) string {
	ext := filepath.Ext(sourceFile)
	switch ext {
	case ".go":
		return "go"
	case ".js", ".ts", ".jsx", ".tsx":
		return "node"
	case ".py":
		return "python"
	}
	if markers["package.json"] {
		return "node"
	}
	if markers["requirements.txt"] || markers["pyproject.toml"] {
		return "python"
	}
	return "go" // default
}

// providerForLanguage returns the coverage provider name for a language.
func providerForLanguage(lang string) string {
	switch lang {
	case "node":
		return "node"
	case "python":
		return "python"
	default:
		return "go"
	}
}
```

Then modify `internal/session/run_phases_autotest.go` to use the detector when creating the coverage provider. The exact change depends on what you found in Step 1 — if it's `autotest.NewGoCoverageProvider(...)`, wrap it:
```go
// In executeAutoTestPhase, replace hardcoded Go provider with:
lang := autotest.DetectProjectLanguage(rp.session.ProjectDir)
switch lang {
case "node":
    cov = autotest.NewNodeCoverageProvider(...)  // or however Node coverage is constructed
case "python":
    cov = autotest.NewPythonCoverageProvider(...)
default:
    cov = autotest.NewGoCoverageProvider(...)
}
```
(Read the existing Node/Python coverage factory functions to get exact signatures — they exist in `internal/autotest/coverage_node_factory.go` and `coverage_python_factory.go`.)

- [ ] **Step 5: Run tests**

Run: `go test ./internal/autotest/ -run "TestDetectLanguage|TestSelectProvider" -v`
Expected: PASS.

Run: `go build ./...` — must compile.

- [ ] **Step 6: Commit**

```bash
git add internal/autotest/ internal/session/run_phases_autotest.go
git commit -m "feat(autotest): add language detection + provider routing"
```

---

### Task 4: Node fixture

**Files:**
- Create: `test/fixtures/node-app/package.json`, `lib.js`, `jest.config.js`
- Create: `test/fixtures/node_app_fixture_test.go`

**Interfaces:**
- Consumes: Task 3 language routing.
- Produces: Node fixture integration test verifying `coverage_node_parse` + `gen_node_extract` (never-run code).

**IMPORTANT:** This test must **skip if `npm` is not available** (`exec.LookPath("npm")`). CI without Node skips gracefully.

- [ ] **Step 1: Create Node fixture project**

`test/fixtures/node-app/package.json`:
```json
{
  "name": "cerberus-fixture-node",
  "version": "1.0.0",
  "scripts": { "test": "jest --coverage --coverageReporters=json" },
  "devDependencies": { "jest": "^29.0.0" }
}
```

`test/fixtures/node-app/lib.js`:
```javascript
function add(a, b) { return a + b; }
function sub(a, b) { return a - b; }
module.exports = { add, sub };
```

`test/fixtures/node-app/jest.config.js`:
```javascript
module.exports = {
  collectCoverage: true,
  coverageReporters: ["json"],
  coverageDirectory: "coverage"
};
```

- [ ] **Step 2: Write integration test (skip if no npm)**

`test/fixtures/node_app_fixture_test.go`:
```go
package fixtures

import (
	"context"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
)

func TestNodeAppFixture(t *testing.T) {
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm not available, skipping Node fixture test")
	}

	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	require.NoError(t, store.RunMigrations(context.Background(), s.DB(), "../../migrations"))

	cfg := project.DefaultConfig()
	cfg.Settings.Mode = "local"
	mockClient := llm.NewMockClient(MockResponses("lib.js"))
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(50000, 5000))
	_ = driver

	sess, err := session.NewSession(context.Background(), session.SessionConfig{
		Mode:       session.ModeRun,
		Goal:       "test node fixture",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     zap.NewNop(),
		ProjectDir: "test/fixtures/node-app",
	})
	require.NoError(t, err)

	err = sess.Run(context.Background())
	// Node fixture tests whether cerberus can process a Node project.
	// If coverage_node_parse or gen_node has bugs, this will fail —
	// fix them TDD-style (write test for the bug, fix, re-run).
	require.NoError(t, err, "Node session should complete")

	assert.NotNil(t, sess.Contract)
}
```

- [ ] **Step 3: Run test**

Run: `go test ./test/fixtures/ -run TestNodeAppFixture -v`
Expected: PASS (if npm available), or SKIP (if not).

**If it FAILS** (not skip, not pass): `coverage_node_parse` / `gen_node` likely has a bug. Debug the failure, write a focused test for the bug, fix it, re-run. This is the core value of Plan 2.

- [ ] **Step 4: Commit**

```bash
git add test/fixtures/
git commit -m "test(fixtures): add Node fixture exercising coverage_node_parse + gen_node"
```

---

### Task 5: Python fixture

**Files:**
- Create: `test/fixtures/python-pkg/math.py`, `requirements.txt`
- Create: `test/fixtures/python_pkg_fixture_test.go`

**Interfaces:**
- Consumes: Task 3 language routing.
- Produces: Python fixture integration test verifying `coverage_python_parse` + `gen_python_extract`.

**Skip if `python3` not available.**

- [ ] **Step 1: Create Python fixture**

`test/fixtures/python-pkg/math.py`:
```python
def add(a, b):
    return a + b

def sub(a, b):
    return a - b
```

`test/fixtures/python-pkg/requirements.txt`:
```
pytest>=7.0
pytest-cov>=4.0
```

- [ ] **Step 2: Write integration test (skip if no python3)**

`test/fixtures/python_pkg_fixture_test.go`:
```go
package fixtures

import (
	"context"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
)

func TestPythonPkgFixture(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available, skipping Python fixture test")
	}

	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	require.NoError(t, store.RunMigrations(context.Background(), s.DB(), "../../migrations"))

	cfg := project.DefaultConfig()
	cfg.Settings.Mode = "local"
	mockClient := llm.NewMockClient(MockResponses("math.py"))

	sess, err := session.NewSession(context.Background(), session.SessionConfig{
		Mode:       session.ModeRun,
		Goal:       "test python fixture",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     zap.NewNop(),
		ProjectDir: "test/fixtures/python-pkg",
	})
	require.NoError(t, err)

	err = sess.Run(context.Background())
	require.NoError(t, err, "Python session should complete")

	assert.NotNil(t, sess.Contract)
}
```

- [ ] **Step 3: Run test**

Run: `go test ./test/fixtures/ -run TestPythonPkgFixture -v`
Expected: PASS (if python3) or SKIP. If FAIL → `coverage_python_parse` / `gen_python` bug → fix TDD.

- [ ] **Step 4: Commit**

```bash
git add test/fixtures/
git commit -m "test(fixtures): add Python fixture exercising coverage_python_parse + gen_python"
```

---

## Self-Review

- **Spec coverage**: fixture 4 语言(T1 Go, T2 SaaS, T4 Node, T5 Python)✓;mock helper(T1)✓;AutoTest 语言路由(T3)✓;bug 处理(T4/T5 "if fail, debug + fix")✓;CI skip if toolchain absent(T4/T5 t.Skip)✓。
- **Placeholders**: none; every step has code. Task 3 has an investigation step (read current code) + concrete detector/router code.
- **Type consistency**: `MockResponses(targetFile string)` used in T1/T2/T4/T5 consistently. `detectLanguage`/`providerForLanguage` defined T3.
- **One caveat**: Task 3's provider routing modification depends on the existing Node/Python factory signatures (coverage_node_factory.go, coverage_python_factory.go) — the implementer must read those to get exact constructor args. Flagged in Step 4.
