# LLM Call Logging & Configurable Log Level — Design

> Brainstormed 2026-07-27. Approach chosen by the user: **all three points**
> — (1) wire `cfg.LogLevel` to the logger, (2) debug-log Scout planning tool
> calls / assembly, (3) always-on **daily** file sink (`logsDir/cerberus-YYYY-MM-DD.log`).
> New `internal/logging` package. Secrets stance: **planning-phase only** (no
> tokens); execution-phase logging is explicitly out of scope.
> Companion: plan `2026-07-27-llm-call-logging-plan.md` (to be written).
> Motivating incident: the 2026-07-27 `cerberus run` dogfood aborted with
> `assembly produced zero cases`; the cause was invisible because Scout never
> logs what GLM returned.

## Background

While re-running the WS relay dogfood to validate the credentials.yaml fix,
`cerberus run` aborted twice with:

```
Error: session run: scout plan: scout plan: assembly produced zero cases
```

`Scout.Plan` (plan_phases.go:67) aborts in Phase 2 when the LLM assembles zero
cases (direct_planning.go:74), **before** Phase 3 (`appendExecutorCases` →
`WSCasesCovered`) can emit the deterministic relay case. The abort is in the
LLM planning round, but nothing in the run log shows what GLM actually
returned — so the failure is a black box.

## Problem

cerberus has a zap JSON logging system, but it cannot answer "what did the LLM
emit?" Three gaps, all unfinished observability rather than deliberate design:

1. **`CERBERUS_LOG_LEVEL` is a dead field.** `config.Load()` parses it into
   `cfg.LogLevel` (config.go:16, :49, default `"info"`), but nothing wires it to
   the logger. Every `cmd/cerberus/*.go` site calls `zap.NewProduction()`
   directly (hardcoded `InfoLevel`): `main_run.go:28`, `main_verify.go:24`,
   `main_serve.go:25`, `main_mcp.go:23`, `memory_command.go:38/202/294/367`,
   `regression_known_issue.go:26`, `regression_accuracy.go:21`,
   `regression_types.go:27`, `init.go:95`. So `CERBERUS_LOG_LEVEL=debug` has no
   effect.

2. **The LLM driver never logs the raw response.** `DecideWithTools`
   (driver_tools.go:20) returns `resp.ToolCalls` (driver_tools.go:50) and logs
   nothing about them at any level. The `llm/*` complete functions have no
   logging either. So even at debug level, GLM's tool calls are invisible.

3. **No persistent log file.** `cerberus run` writes to stderr only.
   `runtime.Paths.LogsDir` (paths.go:32) is defined but the run command never
   writes there; the directory is empty (the stray `*.log` files under
   `.cerberus/runtime/` are old manual stderr captures from 2026-07-18/19).

## Root cause

Observability for the LLM call path was never finished: the level field exists
but is unwired, the call sites have no raw-response logging, and there is no
file sink. This is a gap in deferred infrastructure, not a principled tradeoff.
(The one legitimate tension — payload logging is verbose and can leak secrets
— argues for a default-off, debug-level switch, which is exactly what
`CERBERUS_LOG_LEVEL` was meant to be.)

## Approach

### Component 1 — `internal/logging`: level-aware tee logger

A new package with a single responsibility: build a configured `*zap.Logger`.

```go
package logging

// NewLogger builds a zap logger that tees JSON output to stderr AND a daily
// file under logsDir, at the parsed level. A file-open failure degrades
// gracefully to stderr-only (logging must never sink a run).
func NewLogger(level string, logsDir string) *zap.Logger

// parseLevel maps "debug"|"info"|"warn"|"error" to zapcore.Level; any other
// value (including "") falls back to InfoLevel.
func parseLevel(level string) zapcore.Level
```

- Parse `level` → `zapcore.Level` (default info — **behavior-preserving** vs the
  current `zap.NewProduction()`).
- Tee core: `stderr` (JSON, `zap.NewProductionEncoderConfig()`) **+** a daily
  file `filepath.Join(logsDir, "cerberus-"+time.Now().Format("2006-01-02")+".log")`,
  opened `O_APPEND|O_CREATE|O_WRONLY`.
- `os.MkdirAll(logsDir, 0o755)` before opening. **File-open failure → stderr-only
  logger** (do not return an error that could abort the run); emit one stderr
  line noting the fallback.
- **Findability**: the logger emits one info line at construction naming the
  resolved daily file path (e.g. `logging to <logsDir>/cerberus-2026-07-27.log`)
  so an always-on sink is discoverable; on fallback it names stderr-only. `cfg.Paths`
  is guaranteed non-nil (`GetPaths()` returns `&Paths{}`), so `logsDir` needs no
  nil-guard.
- Date is fixed at process start (`time.Now()`); a run spanning midnight keeps
  writing to its start-date file. Acceptable for a dev/debug tool.

### Component 2 — planning-phase debug logs (`runAIPlanning`)

The `Driver` has no logger and is not modified. Logging lives in
`runAIPlanning` (`internal/head/scout/direct_planning.go:60`), a `*Scout` method
that already holds `s.logger` (scout.go:22) and receives the full `res.ToolCalls`:

- **Before assembly** (debug): log each tool call's `name` and `input` —
  planning tool calls carry role/type names, not large payloads, so the full
  input is small and safe — `s.logger.Debug("llm tool calls", zap.Int("n", len(res.ToolCalls)), ...)`.
- **After assembly** (debug): `s.logger.Debug("assembled plan", zap.Int("tool_calls", n), zap.Int("cases", len(plan.Cases)))`.
- **On zero cases** (debug, alongside the existing error at line 74): a debug
  line so the run log shows *why* assembly produced nothing.

At info level these emit nothing — default runs keep their current noise level.

### Component 3 — wire `cmd/` (~12 sites)

Replace every `logger, _ := zap.NewProduction()` with:

```go
logger := logging.NewLogger(cfg.LogLevel, cfg.Paths.LogsDir)
```

All sites already hold `cfg := config.Load()` in scope. Includes `init.go:95`
(`seedLogger`). Sites that currently build no logger (`main_auth.go`,
`main_protocol.go`) are left as-is unless they enter the LLM path.

### Data flow

```
CERBERUS_LOG_LEVEL ─→ config.Load() ─→ cfg.LogLevel ──┐
                                          cfg.Paths.LogsDir ─┤
                                                               ↓
                              logging.NewLogger(level, logsDir) → zap tee core
                                          ↓                          ↓
                                  stderr (console)        logsDir/cerberus-YYYY-MM-DD.log
                                          ↓
                       Scout.runAIPlanning → s.logger.Debug(tool calls / assembly)
```

## What stays unchanged

- `DecideWithTools` / `Driver` signatures and behavior (no logger injected).
- `config.Load()` and the `Config` struct (already produce `LogLevel` + `Paths`).
- Default behavior: `CERBERUS_LOG_LEVEL` unset → info level → identical console
  output to today, plus a daily file at info volume (small).
- The deterministic relay case path (`WSCasesCovered`) — unaffected; this change
  only adds visibility into why Phase 2 aborts.

## Files

- **new** `internal/logging/logging.go` — `NewLogger` + `parseLevel`.
- **new** `internal/logging/logging_test.go` — level parsing, file tee, fallback.
- `internal/head/scout/direct_planning.go:60` — three `s.logger.Debug` calls in
  `runAIPlanning`.
- `internal/head/scout/direct_planning_test.go` (or nearest scout test) —
  zaptest/observer assertion that debug logs fire at debug and not at info.
- `cmd/cerberus/*.go` — ~12 sites: `zap.NewProduction()` → `logging.NewLogger(...)`.

## Testing (TDD — write the test first, watch it fail, then implement)

`internal/logging/logging_test.go`:
- `TestParseLevel` — `debug/info/warn/error` map correctly; `""`, `"verbose"`,
  `"TRACE"` (case) → info.
- `TestNewLogger_WritesDailyFile` — point `logsDir` at `t.TempDir()`, log a
  debug line, assert a file matching `cerberus-*.log` exists and contains the
  line.
- `TestNewLogger_LevelFilters` — at `level="info"`, a `Debug(...)` call writes
  nothing to the file; at `level="debug"` it does.
- `TestNewLogger_FileOpenFails_Graceful` — unwritable `logsDir` → returns a
  non-nil stderr-only logger and does not panic.

`internal/head/scout/*_test.go`:
- Inject a `zaptest/observer` logger at debug into a `Scout` whose driver
  returns known tool calls; assert an observer record contains a tool-call name.
- At info level the observer captures nothing for the debug calls. (Reuse
  existing Scout test scaffolding where present; if setup is heavy, assert via
  the observer captured at debug only.)

## Verification

1. `make check` (fmt + lint + test -race) EXIT 0.
2. Manual: `CERBERUS_LOG_LEVEL=debug` + the relay dogfood tmpdir config → the run
   log (and the daily file under `runtime/logs/`) shows GLM's tool-call names
   and the `assembled N → M cases` line, revealing the zero-case cause.
3. Default (no env): a normal `cerberus run` behaves as today (info console
   output) and additionally appends to the daily file at info volume.

## Out of scope (explicit)

- **Execution-phase logging** that would include injected auth tokens (e.g.
  `doConnect` / `injectAuth`). Secrets stay out of logs. A future redacting
  logger could cover this; not now.
- lumberjack or size-based rotation — daily files are sufficient for a dev tool.
- Logging the full LLM prompt / response payloads (only each tool call's name
  + input is logged, not the surrounding prompt/response text).
- Caching-aware tool-call logging (deferred per the S2 spec's cache note).
- Changing the `Driver` to carry a logger.

## Related

- cccmemory `credentials-yaml-not-loaded-bug` (the fix this logging was meant to
  validate) and `llm-ws-flow-emission-unstable` (the orthogonal cause of the
  zero-case abort, which this logging will make visible).
- Prior specs in `cerberus-docs/superpowers/specs/` for style conventions.
