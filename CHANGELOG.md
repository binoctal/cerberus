# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
