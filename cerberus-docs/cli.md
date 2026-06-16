# CLI Reference

## Global Flags

| Flag        | Default                | Description              |
|-------------|------------------------|--------------------------|
| `--config`  | `.cerberus/project.yaml` | Config file path       |
| `--db`      | (auto)                 | Database path            |

## Commands

### `cerberus init`

Initialize project configuration.

```bash
cerberus init --url https://api.example.com
```

Creates `.cerberus/project.yaml`, `.cerberus/credentials.yaml`, updates
`.gitignore`, seeds the database with default strategies, and configures
`.claude/settings.json` for MCP integration.

| Flag    | Description     |
|---------|-----------------|
| `--url` | Target base URL |

### `cerberus run`

Run intelligent tests: cognition, exploration, judgment.

```bash
cerberus run --url https://api.example.com --goal "smoke test"
```

| Flag          | Default | Description                          |
|---------------|---------|--------------------------------------|
| `--url`       |         | Target URL                           |
| `--goal`      |         | Test goal (required)                 |
| `--actor`     |         | Test actors (repeatable)             |
| `--db`        |         | Database path                        |
| `--config`    |         | Config file                          |
| `--deep-plan` | false   | Enable Tree-of-Thought planning      |
| `--parallel`  | false   | Enable parallel test execution       |
| `--workers`   | 4       | Max parallel workers                 |
| `--dir`       | `.`     | Project directory                    |

### `cerberus verify`

Regression mode against a known project model.

```bash
cerberus verify --url https://api.example.com --goal "regression"
```

Same flags as `run`. Uses stored project model for deterministic coverage.

### `cerberus serve`

Start HTTP API server for CI/CD integration.

```bash
cerberus serve --port 8090
```

| Flag     | Default | Description    |
|----------|---------|----------------|
| `--port` | `8090`  | Listen port    |

### `cerberus mcp`

Start MCP server for Claude Code integration. Reads from stdin, writes to stdout.

```bash
cerberus mcp
```

No flags. Configuration comes from environment variables and the project config file.

### `cerberus report`

Generate a test report.

```bash
cerberus report --session sess_abc123 --format html --output report.html
```

| Flag        | Default     | Description                    |
|-------------|-------------|--------------------------------|
| `--session` |             | Session ID (required)          |
| `--format`  | `markdown`  | Output: `html`, `markdown`, `json` |
| `--output`  |             | Output file path               |

### `cerberus dashboard`

Launch an interactive TUI dashboard for monitoring sessions.

```bash
cerberus dashboard
```

### `cerberus version`

Print version information.

```bash
cerberus version
```

Output includes version, git commit, and build date (injected via `-ldflags`).

## Environment Variables

| Variable              | Description                    |
|-----------------------|--------------------------------|
| `CERBERUS_PORT`       | HTTP server port               |
| `CERBERUS_DB_PATH`    | Database file path             |
| `CERBERUS_MIGRATION_DIR` | SQL migrations directory    |
| `CERBERUS_LOG_LEVEL`  | Log verbosity                  |
| `CERBERUS_LLM_MODEL`  | LLM model (default `claude-sonnet-4-6`) |
| `CERBERUS_LLM_API_KEY`| API key (or `ANTHROPIC_API_KEY`) |
