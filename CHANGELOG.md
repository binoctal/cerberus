# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
