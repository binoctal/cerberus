# LLM Call Logging & Configurable Log Level Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `CERBERUS_LOG_LEVEL` actually control cerberus's logger and expose what the LLM returns, so a `cerberus run` abort like `assembly produced zero cases` is diagnosable instead of a black box.

**Architecture:** One new package (`internal/logging`) builds a level-aware zap logger that tees JSON to stderr AND a daily file (`cerberus-YYYY-MM-DD.log`). All ~12 `cmd/` entrypoints stop calling `zap.NewProduction()` directly and call `logging.NewLogger(cfg.LogLevel, cfg.Paths.LogsDir)`. Scout's `runAIPlanning` adds three `s.logger.Debug` calls (tool calls received → cases assembled → zero-case reason). No `Driver` change.

**Tech Stack:** Go 1.25 · `go.uber.org/zap` + `zaptest/observer` (already in the zap module — no new dependency) · `internal/runtime` paths.

**Spec:** `cerberus-docs/superpowers/specs/2026-07-27-llm-call-logging-design.md`

## Global Constraints

- Go 1.25, module `github.com/binoctal/cerberus`, **pure Go (no CGo)**, **no new external dependency** (`zaptest/observer` is a subpackage of the existing `go.uber.org/zap`).
- **Behavior-preserving at default level**: `CERBERUS_LOG_LEVEL` unset → info level → console output identical to today (same JSON encoder, same `caller` field — see Task 1's `zap.AddCaller()` note), plus a daily file at info volume.
- Commit author `binoctal <binoctal@gmail.com>`, **no Co-Authored-By**, commit messages in English, conventional-commit style (`feat(scope): ...`).
- `make check` (fmt + lint + test -race) EXIT 0.
- **All docs in `cerberus-docs/`** (never `docs/`).
- Secrets: only **planning-phase** tool calls are logged (role/type names). Execution-phase logging (which would include injected tokens) is out of scope.

---

## File Structure

- **new** `internal/logging/logging.go` — `NewLogger(level, logsDir)` + `parseLevel` + unexported `dailyFile`/`newFileCore`. One responsibility: build a configured tee logger.
- **new** `internal/logging/logging_test.go` — level parsing, daily-file write, level filtering, file-failure fallback.
- **modify** `internal/head/scout/direct_planning.go:60-83` — three `s.logger.Debug` calls in `runAIPlanning`. `strings` and `zap` are already imported there.
- **modify** `internal/head/scout/direct_planning_test.go` — two observer-backed tests (add `zapcore` + `zaptest/observer` imports).
- **modify** `cmd/cerberus/*.go` (12 sites) — `zap.NewProduction()` → `logging.NewLogger(cfg.LogLevel, cfg.Paths.LogsDir)`; add the `internal/logging` import.

---

## Task 1: `internal/logging` package — configurable level + daily-file tee

**Files:**
- Create: `internal/logging/logging.go`
- Test: `internal/logging/logging_test.go`

**Interfaces:**
- Produces: `logging.NewLogger(level string, logsDir string) *zap.Logger` — used by Task 3.

- [ ] **Step 1: Write the failing test file**

Create `internal/logging/logging_test.go`:

```go
package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap/zapcore"
)

func TestParseLevel(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want zapcore.Level
	}{
		{"debug", "debug", zapcore.DebugLevel},
		{"info", "info", zapcore.InfoLevel},
		{"warn", "warn", zapcore.WarnLevel},
		{"warning-alias", "warning", zapcore.WarnLevel},
		{"error", "error", zapcore.ErrorLevel},
		{"empty", "", zapcore.InfoLevel},
		{"unknown", "verbose", zapcore.InfoLevel},
		{"uppercase", "DEBUG", zapcore.DebugLevel},
		{"padded", "  info  ", zapcore.InfoLevel},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseLevel(c.in); got != c.want {
				t.Errorf("parseLevel(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestDailyFile(t *testing.T) {
	got := dailyFile("/tmp/logs", time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC))
	want := filepath.Join("/tmp/logs", "cerberus-2026-07-27.log")
	if got != want {
		t.Errorf("dailyFile = %q, want %q", got, want)
	}
}

func TestNewLogger_WritesDailyFile(t *testing.T) {
	dir := t.TempDir()
	logger := NewLogger("debug", dir)
	logger.Debug("hello-from-test")
	_ = logger.Sync()

	name := "cerberus-" + time.Now().Format("2006-01-02") + ".log"
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("expected daily file %s: %v", name, err)
	}
	if !strings.Contains(string(data), "hello-from-test") {
		t.Errorf("daily file missing debug line; got:\n%s", data)
	}
}

func TestNewLogger_LevelFilters(t *testing.T) {
	dir := t.TempDir()
	logger := NewLogger("info", dir)
	logger.Debug("should-be-filtered")
	logger.Info("should-pass")
	_ = logger.Sync()

	name := "cerberus-" + time.Now().Format("2006-01-02") + ".log"
	data, _ := os.ReadFile(filepath.Join(dir, name))
	if strings.Contains(string(data), "should-be-filtered") {
		t.Error("debug line leaked into info-level file")
	}
	if !strings.Contains(string(data), "should-pass") {
		t.Error("info line missing from file")
	}
}

func TestNewLogger_FileOpenFails_Graceful(t *testing.T) {
	// logsDir points at an existing FILE, so MkdirAll fails -> stderr fallback.
	filePath := filepath.Join(t.TempDir(), "i-am-a-file")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	logger := NewLogger("debug", filePath) // must not panic
	logger.Debug("still-works")
	_ = logger.Sync()
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/logging/ -run 'TestParseLevel|TestDailyFile|TestNewLogger' -v`
Expected: FAIL / build error — package has no symbols (`NewLogger`, `parseLevel`, `dailyFile` undefined).

- [ ] **Step 3: Write the implementation**

Create `internal/logging/logging.go`:

```go
// Package logging builds cerberus's configured zap logger.
//
// It honors CERBERUS_LOG_LEVEL (resolved by the cmd/ entrypoints from
// config.Load) and tees JSON output to stderr AND a daily file under the
// runtime logs directory, so LLM-pipeline behavior is observable post-mortem
// without re-running.
package logging

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// NewLogger builds a zap logger that tees JSON output to stderr and a daily
// file under logsDir, at the parsed level. The daily file is named
// cerberus-YYYY-MM-DD.log (date fixed at process start) and opened append-only.
//
// A file-open failure degrades gracefully: the returned logger writes to
// stderr only and never aborts the run. Either way, one info line names where
// logs are written so the sink is discoverable.
//
// zap.AddCaller is applied to preserve the "caller" field that the previous
// zap.NewProduction() loggers emitted. Sampling is intentionally NOT applied
// so debug-level visibility is never dropped.
func NewLogger(level string, logsDir string) *zap.Logger {
	lvl := parseLevel(level)
	enc := zap.NewProductionEncoderConfig()
	stderrCore := zapcore.NewCore(
		zapcore.NewJSONEncoder(enc),
		zapcore.Lock(os.Stderr),
		lvl,
	)
	fileCore, sink := newFileCore(logsDir, lvl, enc)
	if fileCore == nil {
		logger := zap.New(stderrCore, zap.AddCaller())
		logger.Info("logging to stderr only (file sink unavailable)", zap.String("dir", logsDir))
		return logger
	}
	logger := zap.New(zapcore.NewTee(stderrCore, fileCore), zap.AddCaller())
	logger.Info("logging to file", zap.String("path", sink))
	return logger
}

// dailyFile names the daily log file for the given instant.
func dailyFile(logsDir string, now time.Time) string {
	return filepath.Join(logsDir, "cerberus-"+now.Format("2006-01-02")+".log")
}

// newFileCore opens the daily file under logsDir and returns a JSON core at
// lvl writing to it (append). Returns (nil, "") if the directory/file cannot
// be created or opened, so the caller can degrade to stderr-only.
func newFileCore(logsDir string, lvl zapcore.Level, enc zapcore.EncoderConfig) (zapcore.Core, string) {
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return nil, ""
	}
	path := dailyFile(logsDir, time.Now())
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, ""
	}
	return zapcore.NewCore(
		zapcore.NewJSONEncoder(enc),
		zapcore.Lock(f),
		lvl,
	), path
}

// parseLevel maps a level string to zapcore.Level. Unknown values (including
// the empty string) fall back to InfoLevel, matching zap.NewProduction().
func parseLevel(level string) zapcore.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/logging/ -v`
Expected: PASS — all five tests green.

- [ ] **Step 5: Lint + race**

Run: `go test -race ./internal/logging/ && golangci-lint run ./internal/logging/`
Expected: PASS, no lint findings.

- [ ] **Step 6: Commit**

```bash
git add internal/logging/logging.go internal/logging/logging_test.go
git commit -m "feat(logging): configurable level + daily-file tee logger"
```

---

## Task 2: Scout `runAIPlanning` debug logs

**Files:**
- Modify: `internal/head/scout/direct_planning.go:60-83` (the `runAIPlanning` method)
- Test: `internal/head/scout/direct_planning_test.go` (add two tests + `zapcore`/`zaptest/observer` imports)

**Interfaces:**
- Consumes: `s.logger` (already on `*Scout`, scout.go:22) and `res.ToolCalls` (`[]llm.ToolCall`, each has `.Name string` + `.Input map[string]any`, llm/types.go:33).
- Produces: nothing new (observability only).

- [ ] **Step 1: Write the failing tests**

Append to `internal/head/scout/direct_planning_test.go`. First add imports — find the existing `import (` block and add these two lines alongside the already-present `"go.uber.org/zap"`:

```go
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
```

Then append these two tests (they mirror the existing `TestDirectPlan_ToolCallingAssembly` mock setup at direct_planning_test.go:168):

```go
// TestDirectPlan_LogsToolCallsAtDebug asserts runAIPlanning emits debug logs
// naming the tool calls received and the assembled case count, so a zero-case
// abort (the 2026-07-27 dogfood incident) is diagnosable from the run log.
func TestDirectPlan_LogsToolCallsAtDebug(t *testing.T) {
	mock := llm.NewMockClient(nil)
	mock.SetToolResponse("plan http + ws relay", []llm.ToolCall{
		{Name: "test_http_endpoint", Input: map[string]any{"method": "GET", "path": "/health"}},
		{Name: "begin_case", Input: map[string]any{"name": "r", "expectation": "ok", "service": "ws"}},
		{Name: "ws_connect", Input: map[string]any{"role": "a"}},
		{Name: "ws_connect", Input: map[string]any{"role": "b"}},
		{Name: "ws_send", Input: map[string]any{"role": "a", "type": "x"}},
		{Name: "ws_receive", Input: map[string]any{"role": "b", "type": "y"}},
	})
	driver := ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000))

	core, recorded := observer.New(zapcore.DebugLevel)
	sct := NewScout(driver, setupTestStore(t), &project.Config{
		Project:  project.ProjectMeta{Name: "log-plan"},
		Services: []project.Service{{Name: "api", URL: "http://localhost:8080"}},
	}, zap.New(core))

	_, err := sct.Plan(context.Background(), "plan http + ws relay", &project.ProjectModel{})
	require.NoError(t, err)

	recv := recorded.FilterMessage("scout planning tool calls received").All()
	require.Len(t, recv, 1, "tool-calls-received debug log should fire once")
	var tools string
	for _, f := range recv[0].Context {
		if f.Key == "tools" {
			tools = f.String
		}
	}
	require.Contains(t, tools, "test_http_endpoint")
	require.Contains(t, tools, "begin_case")
	require.GreaterOrEqual(t, recorded.FilterMessage("scout planning assembled").Len(), 1)
}

// TestDirectPlan_DebugLogsFilteredAtInfo asserts the planning debug logs are
// emitted at Debug level: an info-level observer captures none of them.
func TestDirectPlan_DebugLogsFilteredAtInfo(t *testing.T) {
	mock := llm.NewMockClient(nil)
	mock.SetToolResponse("plan http + ws relay", []llm.ToolCall{
		{Name: "test_http_endpoint", Input: map[string]any{"method": "GET", "path": "/health"}},
		{Name: "begin_case", Input: map[string]any{"name": "r", "expectation": "ok", "service": "ws"}},
		{Name: "ws_connect", Input: map[string]any{"role": "a"}},
		{Name: "ws_connect", Input: map[string]any{"role": "b"}},
		{Name: "ws_send", Input: map[string]any{"role": "a", "type": "x"}},
		{Name: "ws_receive", Input: map[string]any{"role": "b", "type": "y"}},
	})
	driver := ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000))

	core, recorded := observer.New(zapcore.InfoLevel) // captures Info+, drops Debug
	sct := NewScout(driver, setupTestStore(t), &project.Config{
		Project:  project.ProjectMeta{Name: "log-plan-info"},
		Services: []project.Service{{Name: "api", URL: "http://localhost:8080"}},
	}, zap.New(core))

	_, err := sct.Plan(context.Background(), "plan http + ws relay", &project.ProjectModel{})
	require.NoError(t, err)

	require.Equal(t, 0, recorded.FilterMessage("scout planning tool calls received").Len(),
		"debug log must not appear at info level")
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/head/scout/ -run 'TestDirectPlan_LogsToolCallsAtDebug|TestDirectPlan_DebugLogsFilteredAtInfo' -v`
Expected: FAIL — `FilterMessage("scout planning tool calls received").All()` is empty (the debug log does not exist yet).

- [ ] **Step 3: Implement the debug logs**

In `internal/head/scout/direct_planning.go`, in `runAIPlanning` (line 60), replace this block (lines 69-75):

```go
	if len(res.ToolCalls) == 0 {
		return nil, nil, fmt.Errorf("scout plan: zero tool calls (drift or quality)")
	}
	plan, covered := assemblePlan(res.ToolCalls, goal, s.resolveBaseURL(), s.config.Services)
	if len(plan.Cases) == 0 {
		return nil, nil, fmt.Errorf("scout plan: assembly produced zero cases")
	}
```

with:

```go
	if len(res.ToolCalls) == 0 {
		return nil, nil, fmt.Errorf("scout plan: zero tool calls (drift or quality)")
	}
	names := make([]string, len(res.ToolCalls))
	for i, tc := range res.ToolCalls {
		names[i] = tc.Name
	}
	s.logger.Debug("scout planning tool calls received",
		zap.Int("count", len(res.ToolCalls)),
		zap.String("tools", strings.Join(names, ",")),
	)
	plan, covered := assemblePlan(res.ToolCalls, goal, s.resolveBaseURL(), s.config.Services)
	s.logger.Debug("scout planning assembled",
		zap.Int("tool_calls", len(res.ToolCalls)),
		zap.Int("cases", len(plan.Cases)),
	)
	if len(plan.Cases) == 0 {
		s.logger.Debug("scout planning produced zero cases", zap.Int("tool_calls", len(res.ToolCalls)))
		return nil, nil, fmt.Errorf("scout plan: assembly produced zero cases")
	}
```

`strings` and `zap` are already imported in this file (verified: import block lines 3-12). No import change needed.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/head/scout/ -run 'TestDirectPlan_' -v`
Expected: PASS — both new tests green, and the existing `TestDirectPlan_ToolCallingAssembly` still green.

- [ ] **Step 5: Run the full scout package with race**

Run: `go test -race ./internal/head/scout/`
Expected: PASS, no regressions.

- [ ] **Step 6: Commit**

```bash
git add internal/head/scout/direct_planning.go internal/head/scout/direct_planning_test.go
git commit -m "feat(scout): debug-log planning tool calls and case assembly"
```

---

## Task 3: Wire `logging.NewLogger` across `cmd/` entrypoints

**Files:**
- Modify (12 sites, all already hold `cfg := config.Load()` in scope):
  - `cmd/cerberus/main_run.go:28`
  - `cmd/cerberus/main_verify.go:24`
  - `cmd/cerberus/main_serve.go:25`
  - `cmd/cerberus/main_mcp.go:23`
  - `cmd/cerberus/memory_command.go:38, 202, 294, 367`
  - `cmd/cerberus/regression_known_issue.go:26`
  - `cmd/cerberus/regression_accuracy.go:21`
  - `cmd/cerberus/regression_types.go:27`
  - `cmd/cerberus/init.go:95` (the `seedLogger` site)

**Interfaces:**
- Consumes: `logging.NewLogger(level, logsDir)` from Task 1; `cfg.LogLevel` + `cfg.Paths.LogsDir` from `config.Load()` (`cfg.Paths` is guaranteed non-nil — `runtime.GetPaths()` returns `&Paths{}`).

- [ ] **Step 1: Confirm the exact sites before editing**

Run: `grep -rn "zap.NewProduction()" cmd/ | grep -v "_test.go"`
Expected: exactly the 12 lines listed above. If the count differs, STOP and reconcile before editing (the list is the contract).

- [ ] **Step 2: Add the import to each affected file**

In each of the 8 files above, add to the `import (` block:

```go
	"github.com/binoctal/cerberus/internal/logging"
```

Keep the existing `"go.uber.org/zap"` import if the file still references `zap.*` (most do, via `zap.String`/`zap.Error` at call sites); `goimports` removes it only if unused — that is correct, do not force-keep.

- [ ] **Step 3: Replace the 12 call sites**

In each site, replace:

```go
			logger, _ := zap.NewProduction()
```

with:

```go
			logger := logging.NewLogger(cfg.LogLevel, cfg.Paths.LogsDir)
```

For `cmd/cerberus/init.go:95` the local is `seedLogger`:

```go
				seedLogger, _ := zap.NewProduction()
```

→

```go
				seedLogger := logging.NewLogger(cfg.LogLevel, cfg.Paths.LogsDir)
```

(`init.go:89` already has `cfg := config.Load()` in scope, verified.)

- [ ] **Step 4: Verify build + vet**

Run: `go build ./cmd/... && go vet ./cmd/...`
Expected: both succeed; no unused-import errors.

- [ ] **Step 5: Verify no `zap.NewProduction()` remains in cmd/**

Run: `grep -rn "zap.NewProduction()" cmd/ | grep -v "_test.go"; echo "remaining=$?"`
Expected: empty output and `remaining=1` (grep found nothing) — all 12 sites converted.

- [ ] **Step 6: Commit**

```bash
git add cmd/cerberus/
git commit -m "refactor(cmd): wire configurable logger across entrypoints"
```

---

## Task 4: Verification gate — `make check` + debug dogfood

**Files:** none (verification only).

- [ ] **Step 1: `make check`**

Run: `make check 2>&1 | tail -40; echo "EXIT=${PIPESTATUS[0]}"`
Expected: EXIT 0 (fmt + lint + test -race all green).

- [ ] **Step 2: Confirm default behavior is unchanged**

Run a normal (no env) command and confirm console output still carries the `caller` field and a new one-line `logging to file` notice:

```bash
make build
./build/cerberus version 2>&1 | head -5   # version has no logger; use a logged command instead:
DOG=$(mktemp -d /tmp/cerberus-log-XXXX)
cat > "$DOG/project.yaml" <<'YAML'
project:
  name: log-smoke
actors:
  - name: admin
YAML
./build/cerberus verify --config "$DOG/project.yaml" --dir "$DOG" 2>&1 | grep -E '"caller"|"logging to file"' | head -5
```
Expected: lines include `"caller":"..."` (AddCaller preserved) and a `"logging to file"` info line naming the daily path. Note the printed path.

- [ ] **Step 3: Confirm debug level surfaces Scout tool calls (the original goal)**

Run with the env var and grep for the new debug lines:

```bash
CERBERUS_LOG_LEVEL=debug ./build/cerberus verify --config "$DOG/project.yaml" --dir "$DOG" 2>&1 \
  | grep -E 'scout planning tool calls received|scout planning assembled|"logging to file"' | head -10
cat "$(grep -oE 'cerberus-[0-9-]+\.log' <<< "$DOG" | head -1)" 2>/dev/null || true
```
(If `verify` does not enter Scout planning, substitute the relay dogfood: `CERBERUS_LOG_LEVEL=debug ./build/cerberus run --config <relay project.yaml> --dir <tmp> --db <tmp.db> --goal "..."` per the spec's Verification §2.)
Expected: the daily file under the printed `runtime/logs/` path exists and contains the `scout planning tool calls received` line with actual tool names — this is the zero-case evidence that was previously invisible.

- [ ] **Step 4: No commit (verification-only task)**

If all green, the feature is complete. The implementation commits are Tasks 1-3.

---

## Self-Review (completed by plan author)

- **Spec coverage:** Component 1 (logging pkg) → Task 1. Component 2 (runAIPlanning debug) → Task 2. Component 3 (cmd/ wiring) → Task 3. Spec's Testing § → Tasks 1-2. Spec's Verification § → Task 4. Secrets stance → Global Constraints (planning-only). Daily file sink → Task 1. Findability line → Task 1 `logger.Info("logging to file", ...)`. All covered.
- **Placeholder scan:** none — every code step contains real, runnable code.
- **Type consistency:** `logging.NewLogger(level, logsDir) *zap.Logger` signature identical in Task 1 (produces) and Task 3 (consumes). `llm.ToolCall{Name, Input}` fields match Task 2's usage. `cfg.LogLevel` / `cfg.Paths.LogsDir` match config.go:16,49 + paths.go:32. zap.AddCaller applied in both NewLogger branches (preserves `caller` field — the trap caught during plan-time source review).
