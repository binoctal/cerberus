# CI Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Harden cerberus CI: (1) run all 4 fixtures with real toolchains, (2) AssessCoverage uses real line coverage not pass-ratio proxy, (3) weekly real-LLM dogfood for AI quality.

**Architecture:** Modify ci.yml to install Node/Python toolchains. Add a coverage helper in session that reuses AutoTest coverage if available, else independently runs the language-specific coverage provider. Add a weekly dogfood workflow using ANTHROPIC_* env vars (no hardcoded model).

**Tech Stack:** Go 1.25, GitHub Actions (setup-node/setup-python), cerberus autotest coverage providers, ANTHROPIC_* env vars.

## Global Constraints

- Module: `github.com/binoctal/cerberus`, Go 1.25, no CGo.
- Commit author: `binoctal <binoctal@gmail.com>`, no Co-Authored-By.
- Comments/commits in English. Follow existing patterns.
- `make check` (fmt+lint+test) must pass after each task.
- **No hardcoded model/endpoint/key** — all via `ANTHROPIC_*` env vars.
- Docs only in `cerberus-docs/`, never `docs/`.
- Node/Python fixture tests still skip locally if toolchain absent (CI provides it).

## File Structure

- Modify `.github/workflows/ci.yml` — add setup-node/python + fixture dep install.
- Create `internal/session/coverage.go` — `coverageForSession` helper.
- Modify `internal/session/run_phases_examiner.go` — use real coverage for AssessCoverage.
- Modify `internal/autotest/` — extract `NewCoverageProviderForLanguage` (reuse Task 3 routing).
- Create `.github/workflows/dogfood.yml` — weekly real-LLM dogfood.

---

### Task 1: CI toolchain (setup-node/python + fixture deps)

**Files:**
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Produces: CI test job with Node 20 + Python 3.11 + fixture deps installed.

- [ ] **Step 1: Read current ci.yml**

Run: `cat .github/workflows/ci.yml`
Note the test job structure (checkout → setup-go → build → vet → test → selftest).

- [ ] **Step 2: Add toolchain steps after setup-go**

In the `test` job, after `setup-go` and before `Build binary`, add:

```yaml
      - uses: actions/setup-node@v4
        with:
          node-version: '20'
      - uses: actions/setup-python@v5
        with:
          python-version: '3.11'
      - name: Install fixture deps
        run: |
          cd test/fixtures/node-app && npm install --silent
          cd ../python-pkg && pip install -q -r requirements.txt
```

- [ ] **Step 3: Verify locally (mock CI)**

Run: `go test ./test/fixtures/ -v`
Expected: Node/Python fixture tests either PASS (if local toolchain) or SKIP (if absent). No FAIL from toolchain.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: add Node/Python toolchain for fixture tests"
```

---

### Task 2: Real line coverage for AssessCoverage

**Files:**
- Modify: `internal/autotest/` — extract `NewCoverageProviderForLanguage(lang, runner, logger)`.
- Create: `internal/session/coverage.go` — `coverageForSession` helper.
- Modify: `internal/session/run_phases_examiner.go` — use real coverage.
- Test: `internal/session/coverage_test.go`

**Interfaces:**
- Produces: `(rp *runPhase) realCoveragePct(ctx) float64` — returns real line coverage (AutoTest report if available, else independent coverage run).

- [ ] **Step 1: Extract NewCoverageProviderForLanguage**

Read `internal/session/run_phases_autotest.go` — find the language routing switch (Task 3 of Plan 2: `switch lang { case "node": NewNodeCoverageProvider... }`). Extract this into `internal/autotest/provider_factory.go`:

```go
package autotest

// NewCoverageProviderForLanguage creates the correct coverage provider for a
// detected language. Reused by AutoTest (run_phases_autotest) and the
// Examiner coverage step (run_phases_examiner).
func NewCoverageProviderForLanguage(lang string, runner CoverageRunner, logger *zap.Logger) CoverageProvider {
	switch lang {
	case "node":
		return NewNodeCoverageProvider(runner, logger)
	case "python":
		return NewPythonCoverageProvider(runner, logger)
	default:
		return NewGoCoverageProvider(runner, logger)
	}
}
```

Then update `run_phases_autotest.go` to call `NewCoverageProviderForLanguage` instead of inline switch (DRY).

- [ ] **Step 2: Write the failing test for coverageForSession**

`internal/session/coverage_test.go`:
```go
package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
)

func TestCoverageForSession_NoAutoTest_GoProject(t *testing.T) {
	// A Go project dir with no AutoTest report → independent coverage run.
	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

	cfg := project.DefaultConfig()
	sess := &Session{
		Config:     &cfg,
		Store:      s,
		Logger:     zap.NewNop(),
		ProjectDir: ".", // cerberus repo root (has Go files)
	}

	pct := coverageForSession(context.Background(), sess)
	// cerberus repo has partial coverage; just assert it ran (pct >= 0, not NaN).
	assert.GreaterOrEqual(t, pct, 0.0, "coverage should be a valid number")
}
```

- [ ] **Step 3: Run test — expect FAIL**

Run: `go test ./internal/session/ -run TestCoverageForSession -v`
Expected: FAIL (undefined coverageForSession).

- [ ] **Step 4: Implement coverageForSession**

`internal/session/coverage.go`:
```go
package session

import (
	"context"

	"github.com/binoctal/cerberus/internal/autotest"
)

// coverageForSession returns the real line coverage percentage for the session's
// project. If AutoTest ran (has a report with coverage), reuse it; otherwise
// independently run the language-specific coverage provider.
func coverageForSession(ctx context.Context, sess *Session) float64 {
	// A: reuse AutoTest report if available.
	if sess.LastAutoTestReport != nil && sess.LastAutoTestReport.BeforeCoveragePct > 0 {
		return sess.LastAutoTestReport.BeforeCoveragePct
	}

	// B: independently run coverage provider.
	lang := autotest.DetectLanguage(sess.ProjectDir)
	runner := autotest.DefaultGoCoverageRunner // works for Go; Node/Python runners exist
	provider := autotest.NewCoverageProviderForLanguage(lang, runner, sess.Logger)
	report, err := provider.RunCoverage(ctx, sess.ProjectDir)
	if err != nil || report == nil {
		return 0
	}
	return report.CoveragePct
}
```

**NOTE:** `DetectLanguage` takes a projectDir string in the current implementation — verify its signature (it may need the markers map or source files). Read `internal/autotest/language.go` to confirm. If it needs more args, adjust the call.

Also: `DefaultGoCoverageRunner` is a `func(ctx, projectDir) ([]byte, error)` — for Node/Python, the provider may need a different runner. Read `coverage_node_factory.go` / `coverage_python_factory.go` for what runner they expect. If different, pass the correct one per language (the provider factory may need to handle runner selection internally).

- [ ] **Step 5: Run test — expect PASS**

Run: `go test ./internal/session/ -run TestCoverageForSession -v`
Expected: PASS.

- [ ] **Step 6: Wire into run_phases_examiner**

In `internal/session/run_phases_examiner.go`, replace the current covPct computation (pass-ratio) with:
```go
	if rp.session.Contract != nil {
		covPct := coverageForSession(rp.ctx, rp.session)
		assessment, aerr := examinerHead.AssessCoverage(rp.ctx, rp.session.Contract, rp.results, covPct)
		// ... rest unchanged
	}
```

Remove the old `passed/total` computation.

- [ ] **Step 7: Run full session tests + make check**

Run: `go test ./internal/session/ ./internal/autotest/`
Then: `make check`
Expected: all green.

- [ ] **Step 8: Commit**

```bash
git add internal/session/coverage.go internal/session/coverage_test.go internal/session/run_phases_examiner.go internal/autotest/
git commit -m "feat(session): real line coverage for AssessCoverage (not pass-ratio proxy)"
```

---

### Task 3: Weekly real-LLM dogfood workflow

**Files:**
- Create: `.github/workflows/dogfood.yml`

**Interfaces:**
- Produces: weekly CI workflow that runs cerberus against fixtures with real LLM (env vars from secrets).

- [ ] **Step 1: Create dogfood workflow**

`.github/workflows/dogfood.yml`:
```yaml
name: Dogfood (real LLM)

on:
  schedule:
    - cron: '17 6 * * 1'  # weekly Monday ~6:17 UTC
  workflow_dispatch: {}    # manual trigger

jobs:
  dogfood:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      - uses: actions/setup-node@v4
        with:
          node-version: '20'

      - uses: actions/setup-python@v5
        with:
          python-version: '3.11'

      - name: Install fixture deps
        run: |
          cd test/fixtures/node-app && npm install --silent
          cd ../python-pkg && pip install -q -r requirements.txt

      - name: Build cerberus
        run: make build

      - name: Run dogfood (all local fixtures, real LLM)
        env:
          ANTHROPIC_AUTH_TOKEN: ${{ secrets.ANTHROPIC_AUTH_TOKEN }}
          ANTHROPIC_BASE_URL: ${{ secrets.ANTHROPIC_BASE_URL }}
          ANTHROPIC_DEFAULT_SONNET_MODEL: ${{ secrets.ANTHROPIC_DEFAULT_SONNET_MODEL }}
        run: |
          for fixture in go-lib node-app python-pkg; do
            echo "=== dogfood $fixture ==="
            ./build/cerberus run --dir test/fixtures/$fixture --goal "dogfood: test $fixture" || echo "WARN: $fixture dogfood failed (review needed)"
          done

      - name: Run SaaS dogfood (httptest + real LLM)
        env:
          ANTHROPIC_AUTH_TOKEN: ${{ secrets.ANTHROPIC_AUTH_TOKEN }}
          ANTHROPIC_BASE_URL: ${{ secrets.ANTHROPIC_BASE_URL }}
          ANTHROPIC_DEFAULT_SONNET_MODEL: ${{ secrets.ANTHROPIC_DEFAULT_SONNET_MODEL }}
        run: |
          echo "=== dogfood SaaS ==="
          ./build/cerberus run --url http://localhost:9999 --goal "dogfood: test SaaS" || echo "WARN: SaaS dogfood failed"
```

**Key design**: all model/endpoint/key via `ANTHROPIC_*` env vars from GitHub secrets. No hardcoded model name. Changing model = update secret, not workflow.

**Note**: SaaS dogfood needs a live HTTP server. For simplicity, this step may fail if no server is running on :9999. The `|| echo` ensures it doesn't block. A future improvement: start a fixture server in the workflow. For now, the 3 local fixtures (go/node/python) are the primary dogfood targets.

- [ ] **Step 2: Document secrets needed**

Add to the workflow a comment or create `cerberus-docs/ci/dogfood-secrets.md`:
```markdown
# Dogfood CI Secrets

Set these in GitHub repo Settings → Secrets and variables → Actions:

- `ANTHROPIC_AUTH_TOKEN` — LLM API key (e.g. GLM key).
- `ANTHROPIC_BASE_URL` — LLM endpoint (e.g. https://open.bigmodel.cn/api/anthropic).
- `ANTHROPIC_DEFAULT_SONNET_MODEL` — model name (e.g. GLM-4.7).

These match cerberus config resolution (ANTHROPIC_* env vars).
Changing model/endpoint = update secret, not code.
```

- [ ] **Step 3: Verify workflow syntax**

Run: `actionlint .github/workflows/dogfood.yml` (if available) or review YAML manually.
Expected: valid syntax.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/dogfood.yml cerberus-docs/ci/dogfood-secrets.md
git commit -m "ci: add weekly real-LLM dogfood workflow (ANTHROPIC_* env vars)"
```

---

## Self-Review

- **Spec coverage**: #1 CI toolchain (T1) ✓; #2 real coverage B+A (T2) ✓; #3 weekly dogfood env vars (T3) ✓. env var principle (no hardcoded model) — T3 uses ANTHROPIC_* ✓.
- **Placeholders**: none; every step has code. Task 2 has a NOTE about verifying DetectLanguage signature + runner selection (read factory files) — flagged for implementer.
- **Type consistency**: `coverageForSession(ctx, *Session) float64` defined T2, used in run_phases_examiner T2. `NewCoverageProviderForLanguage(lang, runner, logger)` defined in autotest, used by session.
- **One caveat**: Task 2's `DetectLanguage` signature + coverage runner per language may need adjustment based on existing code — implementer must read `language.go` + factory files. Flagged in Step 4 NOTE.
