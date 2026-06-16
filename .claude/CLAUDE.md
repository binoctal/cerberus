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

## Runtime Files <!-- added: 2026-06-16 -->
- **Development**: `runtime/` in project root (gitignored)
  - `runtime/data/` — SQLite database
  - `runtime/logs/` — Log files
  - `runtime/cache/` — Temporary cache
- **Production** (user installation):
  - Linux/macOS: `~/.local/share/cerberus/` (data), `~/.config/cerberus/` (config), `~/.cache/cerberus/` (cache)
  - Windows: `%LOCALAPPDATA%\Cerberus\`
  - Docker: `/app/data/`, `/app/logs/`, `/app/cache/`
- Auto-detection: `internal/runtime/` package detects development vs production automatically
