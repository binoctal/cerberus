# Cerberus — Universal AI Testing Framework

Cerberus is an AI-powered testing framework that uses a **three-head architecture** (Scout → Agent → Examiner) to intelligently plan, execute, and judge tests for your applications.

## Key Features

- **🧠 AI-Powered** — Uses LLMs to generate, execute, and evaluate tests
- **🎯 Multi-Executor** — HTTP, Database, GraphQL, WebSocket, Browser, Process, File, Code, MCP, Wait (10 executors)
- **🔒 Sandbox Isolation** — Linux namespace sandboxing for safe execution
- **🔌 MCP Integration** — Native Claude Code integration via stdin/stdout JSON-RPC
- **📊 Rich Reporting** — Markdown, HTML, JSON, and TUI/Web dashboards
- **⚡ Zero-Token Rules** — Rule engine routes known patterns without LLM calls

## Quick Start

```bash
# Install
go install github.com/binoctal/cerberus@latest

# Initialize project
cerberus init

# Run tests
cerberus run --goal "test all API endpoints" --url http://localhost:3000

# View results
cerberus report --session <id> --format html -o report.html
```

## Architecture

```
┌─────────┐     ┌─────────┐     ┌───────────┐
│  Scout   │────▶│  Agent  │────▶│ Examiner  │
│ (Plan)   │     │(Execute)│     │  (Judge)  │
└─────────┘     └─────────┘     └───────────┘
     │               │                │
  Analyze        ReAct Loop       Verdict &
  + Plan         + Recovery        Learn
```

## License

MIT
