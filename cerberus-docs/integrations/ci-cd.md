# CI/CD Integration

Cerberus provides an HTTP API server for integration with CI/CD pipelines.
Start it with `cerberus serve`.

## Starting the Server

```bash
cerberus serve --port 8090
```

Default port is `8090`. Override with `--port` or the `CERBERUS_PORT`
environment variable.

## API Endpoints

| Method | Path                          | Description               |
|--------|-------------------------------|---------------------------|
| GET    | `/health`                     | Health check              |
| POST   | `/api/v1/sessions`            | Create a test session     |
| GET    | `/api/v1/sessions`            | List sessions             |
| GET    | `/api/v1/sessions/{id}`       | Get session status        |
| GET    | `/api/v1/sessions/{id}/report`| Get session report        |
| POST   | `/api/v1/sessions/{id}/cancel`| Cancel a session          |

### Create Session

```bash
curl -X POST http://localhost:8090/api/v1/sessions \
  -H "Content-Type: application/json" \
  -d '{"mode": "run", "goal": "smoke test", "url": "https://api.example.com"}'
```

Response: `{"id": "sess_abc123", "status": "running"}`

### List Sessions

```bash
curl http://localhost:8090/api/v1/sessions?limit=10
```

### Get Report

Content negotiation via `Accept` header:

```bash
curl -H "Accept: text/markdown" http://localhost:8090/api/v1/sessions/sess_abc123/report
```

Supported formats: `text/plain`, `text/markdown`, `text/html`, `application/json`.

## Exit Codes

When running `cerberus run` directly in CI:

| Code | Meaning          |
|------|------------------|
| 0    | All tests passed |
| 1    | Test failures    |
| 2    | Configuration error |

## GitHub Actions Example

```yaml
name: Cerberus Tests
on: [push]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Install Cerberus
        run: |
          curl -sL https://github.com/binoctal/cerberus/releases/latest/download/cerberus-linux-amd64 -o /usr/local/bin/cerberus
          chmod +x /usr/local/bin/cerberus

      - name: Run Tests
        env:
          CERBERUS_LLM_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
        run: |
          cerberus run \
            --url https://staging.example.com \
            --goal "smoke test all endpoints" \
            --config .cerberus/project.yaml

      - name: Upload Report
        if: always()
        run: |
          cerberus report --session $SESSION_ID --format html --output report.html
```

## Session Execution Model

Sessions run asynchronously in goroutines. The server tracks active sessions
with cancellation support. Graceful shutdown on `SIGINT`/`SIGTERM` completes
in-flight requests before stopping.

## Dashboard

A web dashboard is available at `/dashboard/` for real-time monitoring.
