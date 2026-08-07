# Cerberus — Project Constraints

## Tech Stack <!-- added: 2026-06-07 -->
- Go 1.25, module: `github.com/binoctal/cerberus`
- SQLite via `modernc.org/sqlite` (no CGo)
- Single binary: `cmd/cerberus`

## Architecture <!-- added: 2026-06-07 -->
- Three-head: Scout (plan) → Agent (execute) → Examiner (judge)
- Internal packages: `ai/`, `config/`, `head/`, `llm/`, `project/`, `prompts/`, `server/`, `session/`, `smoke/`, `store/`
- Prompt registry: `embed.FS` defaults + project-level overrides
- Migrations in `migrations/`

## Commands <!-- added: 2026-06-07 -->
```
make build       # go build → build/cerberus
make test        # go test -v -race ./...
make lint        # golangci-lint run ./...
make fmt         # gofmt + goimports
make check       # fmt + lint + test
make clean       # remove build/ and runtime/
make integration-openagents  # live open-agents suite (starts wrangler, runs, tears down) <!-- added: 2026-08-07 -->
```

## Constraints <!-- added: 2026-06-07 -->
- Commit author: `binoctal <binoctal@gmail.com>`, no Co-Authored-By
- No CGo dependency (pure Go SQLite)
- Code comments and commit messages in English
- Follow existing comment density and naming idiom
- **ALL documentation MUST be in `cerberus-docs/` directory** <!-- added: 2026-06-16 -->
- **NEVER create documents in `docs/` directory** (legacy location, gitignored)

## Key Files <!-- added: 2026-06-07 -->
- `internal/head/` — Scout / Agent / Examiner heads
- `internal/llm/` — LLM client with retry
- `internal/server/` — HTTP API (CI/CD)
- `internal/store/` — SQLite persistence
- `internal/prompts/` — embedded prompt templates
- `internal/runtime/` — Cross-platform runtime path management <!-- added: 2026-06-16 -->
- `internal/escalation/` — EscalationGate interface (NoOpGate, MCPGate) <!-- added: 2026-06-12 -->
- `internal/mcp/` — MCP server for Claude Code integration <!-- added: 2026-06-12 -->
- `migrations/` — DB schema versions

## MCP Integration <!-- added: 2026-06-12 -->
- `cerberus mcp` — stdin/stdout JSON-RPC server for Claude Code
- `cerberus init` — auto-writes `.claude/settings.json` with MCP config
- 5 tools: cerberus_run/status/report/decide/cancel
- 4 escalation checkpoints: budget_warning, systemic_failure, destructive_risk, target_unreachable
- Shares ANTHROPIC_API_KEY with Claude Code, no separate config

## Documentation <!-- added: 2026-06-16 -->
- **ALL documents MUST be created in** `cerberus-docs/` directory
- Never use `docs/` for new documentation
- Document structure: `cerberus-docs/<category>/<type>/YYYY-MM-DD-<topic>-<type>.md`
- Examples:
  - Design specs: `cerberus-docs/superpowers/specs/YYYY-MM-DD-<topic>-design.md`
  - Implementation plans: `cerberus-docs/superpowers/plans/YYYY-MM-DD-<topic>-plan.md`
  - Technical docs: `cerberus-docs/technical/<category>/YYYY-MM-DD-<topic>.md`
- This constraint is enforced via `.gitignore` (cerberus-docs is NOT ignored, docs/ may be)

## Runtime Files <!-- added: 2026-06-16, updated: 2026-06-16 -->
- **All environments**: `.cerberus/runtime/` in project directory (gitignored)
  - `.cerberus/runtime/data/` — SQLite database (`cerberus.db`)
  - `.cerberus/runtime/logs/` — Log files
  - `.cerberus/runtime/cache/` — Temporary cache
- **Configuration**: `.cerberus/` directory
  - `project.yaml` — Project definition (version control)
  - `credentials.yaml` — API credentials (gitignored)
- Each project has its own isolated runtime environment
- No system-wide installation paths (simpler cross-platform support)
