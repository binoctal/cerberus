# Quick Start

## 1. Initialize Project

```bash
cerberus init
```

Creates `.cerberus/` with:
- `project.yaml` — project configuration
- `credentials.yaml` — actor credentials (add to `.gitignore`)
- `cerberus.db` — SQLite database with seeded strategies
- `.claude/settings.json` — MCP integration (if Claude Code detected)

## 2. Configure

Edit `.cerberus/project.yaml`:

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
    entry: "/admin"

settings:
  max_duration: 30m
  confidence_threshold: 0.7
  ai_budget:
    session_total_tokens: 200000
    model: "claude-sonnet-4-6"
```

## 3. Run

```bash
# Set API key
export ANTHROPIC_API_KEY=sk-ant-...

# Run tests
cerberus run --goal "test the user registration and login flow"

# With parallel execution
cerberus run --goal "test all APIs" --parallel --workers 4

# Deep planning mode (comprehensive)
cerberus run --goal "full regression test" --deep-plan
```

## 4. View Results

```bash
# List sessions
cerberus report --session <id>

# HTML report
cerberus report --session <id> --format html -o report.html

# JSON output
cerberus report --session <id> --format json

# TUI dashboard
cerberus dashboard

# Web dashboard (start API server)
cerberus serve --port 8090
# Open http://localhost:8090/dashboard/
```

## Local-Only Testing (No URL Needed)

```bash
cerberus run --dir . --goal "build and test this project"
```

Uses process/file/code executors for local testing without a running server.
