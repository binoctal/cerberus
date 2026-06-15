# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## v0.10.0 — 2026-06-16

### Added

#### AutoTest Parallel Test Generation
- **Concurrent Test Generation**
  - Worker pool pattern for processing multiple coverage gaps in parallel
  - MaxConcurrency field to control parallel workers (default 1 for backward compatibility)
  - executeSerial() and executeParallel() methods for dual execution modes
  - Thread-safe gap processing with proper synchronization
  - Channel-based work distribution and result collection

- **Performance Improvements**
  - Reduces test generation time for projects with many coverage gaps
  - Maintains per-test verification for stability
  - Respects MaxGaps limit and safety modes (DryRun/Approve/Auto)
  - Opt-in parallel execution by setting MaxConcurrency > 1

#### ReAct Loop JSON Parsing Improvements
- **Enhanced Error Detection**
  - isParseError() now detects more parsing failure patterns
  - JSON unmarshaling errors (unmarshal, invalid character, unexpected end)
  - JSON syntax/format errors (json + syntax/error/format/invalid)
  - Structured output parsing errors (parse output)

- **Fixed Fallback Action Mappings**
  - click correctly maps to BrowserClickAction (not NavigateAction)
  - type/fill map to BrowserFillAction
  - goto maps to NavigateAction
  - Added support for common browser interaction keywords

- **Improved Error Handling**
  - Better cleanup of error messages (removes raw content dump)
  - More robust extraction of intent from error messages
  - Enhanced ParseStructuredOutput with input length and content preview
  - Improved brace matching algorithm for nested JSON objects

### Changed
- AutoTest defaults to serial execution (MaxConcurrency = 1) for backward compatibility
- FallbackParseAction now handles browser actions correctly
- JSON parsing errors provide more detailed debugging information

### Fixed
- Fixed file read error handling in parallel AutoTest mode (now ignores errors like serial mode)
- Fixed browser action keyword mappings in fallback parsing
- Updated test expectations for improved error detection patterns

## v0.9.0 — 2026-06-16

### Added

#### AutoTest Mocha Provider Support
- **Node.js (Mocha + nyc) Support**
  - Automatic project detection via package.json and node_modules
  - Mocha + nyc (Istanbul) coverage parsing
  - Istanbul JSON format parsing (reuses Jest coverage parser)
  - Mocha describe/it style test generation with assert library
  - Support for multiple file extensions (`.js`, `.jsx`, `.tsx`, `.ts`)
  - Intelligent test file organization: test/ directory vs same-directory
  - Test file naming: `src/users.js` → `test/users.test.js` or `src/users.test.js`
  - Hybrid detection confidence levels: 0.5 (base) → 0.7 (loose) → 0.9 (strict) → 1.0 (validated)
  - Detection priority: Go > Node (Jest) > Node (Mocha) > Python

- **Project Detection Enhancements**
  - MochaProjectDetector with mocha/nyc keyword detection
  - Updated NodeProjectDetector to not interfere with Mocha projects
  - Enhanced DetectProjectType to include Mocha detector
  - Updated CreateProvider to handle ProjectTypeMocha

- **Configuration System**
  - DefaultMochaCoverageConfig with npm test command
  - Coverage configuration: `coverage/coverage-final.json`
  - 5-minute timeout with NODE_ENV=test

- **Testing and Fixtures**
  - Comprehensive unit tests for MochaProjectDetector (4 test cases)
  - MochaTestFilePath tests for both directory modes
  - Integration test fixture: mocha_project with calculator.js
  - Test coverage: Mocha provider, generator, and detection logic

- **Code Quality**
  - Reuses Istanbul JSON parsing from Jest provider (DRY principle)
  - Consistent error handling and logging patterns
  - All existing tests pass with no breaking changes

### Changed
- Updated AutoTest feature description to include Mocha support
- NodeProjectDetector now returns confidence 0 for non-Jest projects (lets Mocha detector handle them)

### Fixed
- Updated test expectations for NodeProjectDetector to reflect new behavior
- Fixed MochaTestFilePath to properly trim extensions in test directory mode

## v0.8.0 — 2026-06-15

### Added

#### AutoTest Multi-Language Support
- **Node.js (Jest) Support**
  - Automatic project detection via `package.json` and `node_modules`
  - Jest JSON coverage parsing (`coverage/coverage-final.json`)
  - Regex-based AST extraction for exported functions and classes
  - Jest describe/it style test generation
  - Support for multiple file extensions (`.js`, `.jsx`, `.tsx`, `.ts`)
  - Test file naming: `src/users.js` → `src/users.test.js`

- **Python (pytest) Support**
  - Automatic project detection via `requirements.txt`, `setup.py`, or `pyproject.toml`
  - Virtual environment detection (venv, .venv, env)
  - coverage.py SQLite and JSON parsing
  - Python `ast` module integration for function/class extraction
  - pytest fixtures and parametrize style test generation
  - Smart test file organization: prefers `tests/` directory, falls back to same directory

- **Project Detection Infrastructure**
  - `ProjectDetector` interface with confidence scoring (0.0-1.0)
  - Tool availability verification (Jest, pytest, coverage.py)
  - Detection priority: Go > Node > Python
  - `DetectAndCreateProvider()` for automatic provider selection

- **Provider Configuration System**
  - Unified `CoverageConfig` structure for all providers
  - Project-level overrides via `.cerberus/project.yaml`
  - Provider-specific settings: test command, coverage args, timeout, environment
  - Default configurations for Node and Python

- **Test Generation Enhancements**
  - Line-number-based snippet extraction for accurate context
  - Support for class methods and nested structures
  - Fallback mechanisms for missing tools or parse errors

- **Comprehensive Testing**
  - Unit tests for detectors, providers, and generators
  - Integration test fixtures (Node + Jest project, Python + pytest project)
  - Test coverage for new code paths

- **Documentation**
  - Complete user guide (50+ pages) with examples
  - Design spec document with architecture decisions
  - Updated README.md with AutoTest features
  - Quick start examples for Node and Python

### Changed
- README.md updated with AutoTest documentation links and feature list
- Quick start guide enhanced with AutoTest examples
- Project structure documentation updated to include `autotest/` package

### Fixed
- Flaky test `TestServer_CreateSession_Success`: implemented clientFactory dependency injection pattern
- Duplicate Scan field in `ListSessions` handler (removed duplicate `sess.Stats`)
- Node provider test expectations for `.jsx` file handling
- Node project detector test now creates `node_modules` directory for accurate testing

## v0.7.1 — 2026-06-13

### Changed

- Apply gofmt and resolve all remaining golangci-lint findings (errcheck, staticcheck, unused) across the codebase
- Migrate WebSocket dependency from `nhooyr.io/websocket` to `github.com/coder/websocket`
- Total test coverage: 78.6% → 80.1% (policy Validate, sandbox resource/seccomp/mount/cgroup builders, agent CLI analysis via fake sandbox, project credential resolution)

## v0.7.0 — 2026-06-13

### Added

- CodeExecutor tests: GoAnalyze, GoLint, GoSymbols, unsupported action, language detection, parseRuffJSON, parseESLintJSON (20 tests)
- Dashboard refresh/loadSelected/Update tests with real store (5 tests)
- AI Driver SetCache + PromptBuilder tests (4 tests)
- CLI integration tests: report markdown/junit/html/toFile/badFormat/noSession, version, init, unknown command (10 tests)
- E2E real LLM tests: ExaminerJudge (judge + reflexion) and CodeExecutor GoAnalyze (2 tests, `//go:build e2e`)

### Changed

- Total test coverage: 75.1% → 78.6%
- cmd/cerberus coverage: 34.7% → 44.8%

## v0.6.0 — 2026-06-13

### Fixed

- Resume() now records "failed" status on error instead of always "completed"
- JUnit format wired in `cerberus report --format junit` CLI command
- `--resume <session-id>` flag now calls Resume() instead of Run()

### Added

- Gemini stream mock server tests (mock SSE + non-200 error handling)
- DecideWithVision + budget exhausted tests via retryTestClient
- UpdateSemanticTimestamp integration test
- HTTP executor tests: GET/POST/PUT/DELETE, server error, custom headers, cancelled context, navigate dispatch
- Browser truncateStr unit tests (short, exact, truncated, unicode)

### Changed

- SearchSemanticForProject: project-scoped cosine search avoids loading full table
- Executor FinishTrace errors logged instead of silently discarded (4 locations)
- MCP RecoverOrphanSessions logs UpdateSessionStatus errors

## v0.5.0 — 2026-06-13

### Fixed

- Server crash: `SetupHeadDrivers` moved after nil check in `handleCreateSession`
- MCP server version: hardcoded `"0.1.0"` replaced with build-time `Version` variable
- Flaky `TestServer_CreateSession_Success`: clientFactory DI + sync verification replaces `Eventually` wait

### Added

#### MCP Streaming Notifications
- `conn` mutex for safe concurrent JSON-RPC writes
- `writeNotification` method for server-pushed progress events (no `id` field)
- `handleRun` streams `notifications/progress` to MCP host in real time
- `notifications` capability declared in `initialize` response
- Tests: notification format, nil params, concurrent writes, capability check

#### Embedding Provider Interface
- `embed.Provider` interface: `Embed(ctx, text)`, `Dimension()`, `ModelName()`
- `TrigramProvider` wrapping existing `Generate()` as default implementation
- Scout and Examiner Learner now use Provider interface instead of direct calls
- 7 TrigramProvider tests: interface compliance, determinism, parity with `Generate`

#### Test Hardening
- 10 SSE scanner tests: single/multi event, multiline data, comments, `[DONE]` sentinel
- Claude and OpenAI stream integration tests with httptest mock SSE servers
- Stream error handling tests for non-200 responses
- Session lifecycle `fmt.Println` replaced with `zap.Logger.Info`

## v0.4.0 — 2026-06-13

### Added

#### Auto-Fix System (Phase 1)
- ExaminerConfig.AutoFix field: "off", "low_only" (default), "aggressive"
- ShouldAutoFix policy: gates auto-fix on mode + severity + verdict status
- AutoFixer with LLM-based repair analysis and skip downgrade
- Auto-fix integrated into Examiner Examine() loop after verdict
- Invariant severity propagation to TestCase for auto-fix decisions
- AutoFix wiring in session Run() and Resume()
- Table-driven ShouldAutoFix tests (3 modes × 4 severities × 3 statuses)

#### L2 Semantic Memory (Phase 2)
- V004 migration: add embedding/embedding_model columns to memory_semantic
- Local char-trigram embedding generator (deterministic, no API)
- CosineSimilarity, ParseEmbedding, FormatEmbedding helpers
- Semantic memory CRUD: StoreSemantic, GetSemanticByID, SearchSemantic, DeleteSemantic
- Brute-force cosine search with threshold filtering and limit
- Examiner learner stores reflections as L2 semantic memory after Reflexion
- Scout buildEpisodicContext appends L2 semantic search results to plan context

#### Streaming Support (Phase 3)
- Client.Stream interface method with StreamEvent types (delta/done/error)
- SSE scanner for parsing Server-Sent Events from HTTP responses
- Claude streaming: stream=true, parse content_block_delta/message_stop
- OpenAI streaming: stream=true, parse choices delta/finish_reason
- Gemini streaming: streamGenerateContent?alt=sse, parse candidates
- Driver.DecideStreamCollect: collect all streaming events, parse structured output
- MockClient streaming: emit full content as delta + done events

#### Tool/Function Calling (Phase 4)
- Request.Tools and Response.ToolCalls fields on llm types
- Tool and ToolCall types for provider-agnostic function definitions
- Claude: tools with input_schema, parse tool_use content blocks
- OpenAI: tools with function format, parse tool_calls response
- Gemini: tools with functionDeclarations, parse functionCall parts
- Driver.DecideWithTools: send prompt with tools, return ToolCallResult
- types.ToolDefinitions: generate 14 tool schemas from action types

## v0.3.0 — 2026-06-13

### Added

#### Configuration
- Confidence threshold wiring: `Settings.ConfidenceThreshold` now drives Examiner verdict policy
- Verdict degradation: pass verdicts with `CorrectnessConfidence < threshold` auto-downgraded to uncertain
- Environment config overlays: `CERBERUS_ENV=staging` merges `project.staging.yaml` onto base config
- `mergo` dependency for struct-level partial config merge

#### Intelligence
- L1 episodic memory activation in Scout planning: previous test outcomes injected into plan prompt
- `buildEpisodicContext` queries up to 10 historical records per known endpoint target

#### Testing
- E2E evidence tests: `GetEvidenceBySession`, markdown/HTML evidence rendering
- Session resume integration test: skips completed cases, handles missing plan
- Per-head driver fallback test
- Threshold degradation unit tests: downgrade, pass-through, zero-threshold

## v0.2.0 — 2026-06-13

### Added

#### LLM Configuration
- Custom Base URL / proxy support: use Azure OpenAI, Bedrock, Ollama, or any OpenAI-compatible endpoint
- `CERBERUS_LLM_BASE_URL` env var and `base_url` in `project.yaml` ai_budget
- Per-head model configuration: assign different models to Scout, Agent, Examiner, and Critic
- Critic driver activation enables Examiner Self-Refine (previously nil)

#### Execution
- Multi-dependency support: `depends_on` accepts both single string and array of strings
- Cycle detection with Kahn's algorithm: breaks intra-cycle edges and logs warnings
- Coverage calculation: `coverage_pct` now correctly computed from verdict results
- Session resumption: `cerberus run --resume <session-id>` skips Scout, continues from first uncompleted case
- Test plan persistence via `session_plans` table (V003 migration)

#### Reporting
- JUnit XML output: `cerberus report --format junit` for CI/CD integration (Jenkins, GitLab, GitHub Actions)
- Server content negotiation: `Accept: application/junit+xml`
- Evidence enrichment in reports: Markdown collapsible panels, HTML details/summary, JUnit failure/error contents
- `GetEvidenceBySession` for batch evidence loading (avoids N+1 queries)

## v0.1.0 — 2026-06-12

### Added

#### Core Architecture
- Three-head architecture: Scout (plan) → Agent (execute) → Examiner (judge)
- ReAct execution loop with recovery and configurable max attempts
- Session lifecycle: run mode, verify mode, SQLite persistence
- LLM client with retry and exponential backoff (Claude API)
- AI driver with budget tracking and response cache (SHA-256 key, TTL eviction)
- Prompt registry: `embed.FS` defaults + project-level overrides

#### Multi-Executor System
- 8 built-in executors: HTTP, Browser (CDP), File, Process, Code, Wait, Navigation, MCP
- Plugin/extension mechanism: `ExecutorPlugin`, `RulePlugin`, `PluginRegistry`
- Rule engine with zero-token routing and hit-rate observability
- Sandbox isolation (Linux namespaces, noop fallback)
- Parallel execution with dependency ordering (C4 sub-agent)

#### Planning & Intelligence
- Tree-of-Thought (ToT) deep planning with parallel candidate scoring
- Strategy templates seeded on `cerberus init` (L3 memory)
- Project type detection (Go, Node, Python, Rust, etc.)
- Project model with InfoScore for exploration progress

#### MCP Integration
- `cerberus mcp` — stdin/stdout JSON-RPC server for Claude Code
- 5 tools: cerberus_run/status/report/decide/cancel
- 4 escalation checkpoints: budget_warning, systemic_failure, destructive_risk, target_unreachable
- `cerberus init` auto-writes `.claude/settings.json`

#### CLI & Reporting
- `cerberus run` — intelligent test execution (--parallel, --deep-plan, --workers)
- `cerberus verify` — regression mode
- `cerberus serve` — HTTP API server for CI/CD
- `cerberus report` — Markdown, HTML, JSON output
- `cerberus dashboard` — TUI dashboard (bubbletea)
- `cerberus version` — version info with ldflags
- `cerberus init` — project scaffolding with MCP auto-config

#### Quality & Testing
- Config validation for project.yaml (services, actors, invariants, settings)
- Performance benchmarks: cache, rule engine, HTTP executor, multi-executor build
- E2E integration tests: CRUD pipeline, progress events, rule engine stats
- Dogfood self-test: cerberus tests itself (4 test cases)
- Full lint cleanup: 64 errcheck/staticcheck/unused issues resolved

#### Infrastructure
- GitHub Actions CI (build, test, lint)
- GoReleaser config (linux/darwin/windows, amd64/arm64)
- MIT License
