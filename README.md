# Cerberus

Universal AI-powered testing framework for SaaS applications.

Three-head architecture — **Scout** discovers, **Agent** executes, **Examiner** judges.

## Features

- **Scout** — Analyzes your project config and generates intelligent test plans
- **Agent** — Executes tests via ReAct loop (Reason-Act-Observe) with LLM steering
- **Examiner** — Judges results, learns from failures, stores procedural memory
- **Parallel execution** — Run independent test cases concurrently with dependency ordering
- **Cascade skip** — Failed dependencies automatically skip downstream cases
- **LLM retry** — Exponential backoff for transient errors (5xx, 429, timeout)
- **HTTP API** — REST server for CI/CD integration

## Install

```bash
go install github.com/binoctal/cerberus/cmd/cerberus@latest
```

## Quick Start

```bash
# Initialize project
cerberus init

# Edit configuration
vim .cerberus/project.yaml

# Run tests
cerberus run --url http://localhost:3000 --goal "Test all API endpoints"

# Start API server for CI
cerberus serve --port 8090
```

## Architecture

```
┌─────────────────────────────────────────┐
│                Cerberus                  │
├───────────┬───────────┬────────────────┤
│   Scout   │   Agent   │   Examiner     │
│ Analyze   │  ReAct    │  Judge + Learn │
│  Plan     │  Execute  │  Reflect       │
│   ToT     │ Parallel  │  Memory        │
└───────────┴───────────┴────────────────┘
      │           │            │
      ▼           ▼            ▼
┌─────────────────────────────────────────┐
│           Shared Infrastructure          │
│  LLM Client │ Store (SQLite) │ Session  │
│  AI Driver  │ Migrations    │ Prompts   │
└─────────────────────────────────────────┘
```

## Project Structure

```
cmd/cerberus/        CLI entry point
internal/
├── ai/              Token budget, prompt builder, driver with retry
├── config/          Environment configuration
├── head/
│   ├── scout/       Analyze + Plan (with ToT beam search)
│   ├── agent/       ReAct loop, rules engine, HTTP executor, parallel
│   └── examiner/    Judge, learn, reflect
├── llm/             LLM client abstraction + mock
├── prompts/         Template registry (embed.FS + project overrides)
├── project/         Project config loader + credential resolution
├── server/          HTTP API server (CI/CD)
├── session/         Session lifecycle + summary
├── smoke/           End-to-end integration tests
└── store/           SQLite store, migrations, evidence, strategies
migrations/          SQL schema versions
```

## Commands

| Command | Description |
|---------|-------------|
| `cerberus init` | Initialize `.cerberus/` config + seed strategies |
| `cerberus run` | Run intelligent tests |
| `cerberus verify` | Regression mode against known model |
| `cerberus serve` | Start HTTP API server |

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Health check |
| `POST` | `/api/v1/sessions` | Trigger test run |
| `GET` | `/api/v1/sessions` | List sessions |
| `GET` | `/api/v1/sessions/:id` | Get session status |
| `GET` | `/api/v1/sessions/:id/report` | Test report (JSON/text) |
| `POST` | `/api/v1/sessions/:id/cancel` | Cancel running session |

## Configuration

Cerberus uses `.cerberus/project.yaml`:

```yaml
project:
  name: my-saas

services:
  - name: web
    url: "http://localhost:3000"
    health: "/"

actors:
  - name: admin
    credentials:
      email: "${ADMIN_EMAIL}"
      password: "${ADMIN_PASS}"

settings:
  ai_budget:
    session_total_tokens: 200000
    per_call_limit: 10000
    model: "claude-sonnet-4-6"
```

## License

MIT
