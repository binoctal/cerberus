# Claude Code Integration

Cerberus integrates with Claude Code via the MCP (Model Context Protocol).
This lets Claude Code run tests, check progress, and handle escalations
directly from your coding session.

## Setup

### Automatic

Run `cerberus init` in your project directory. It writes the MCP configuration
to `.claude/settings.json`:

```json
{
  "mcpServers": {
    "cerberus": {
      "command": "cerberus",
      "args": ["mcp"]
    }
  }
}
```

The MCP server shares the `ANTHROPIC_API_KEY` already configured for Claude Code.
No separate API key is needed.

### Manual

Start the MCP server manually for testing:

```bash
cerberus mcp
```

This starts a JSON-RPC 2.0 server on stdin/stdout (stdio transport), using
protocol version `2024-11-05`.

## MCP Tools

| Tool                | Purpose                           | Required Params          |
|---------------------|-----------------------------------|--------------------------|
| `cerberus_run`      | Start a test session              | `goal`, `url`            |
| `cerberus_status`   | Poll session progress             | `session_id`             |
| `cerberus_report`   | Get final test report             | `session_id`             |
| `cerberus_decide`   | Respond to an escalation event    | `session_id`, `action`   |
| `cerberus_cancel`   | Cancel a running session          | `session_id`             |

### `cerberus_decide` actions

- `continue` -- proceed despite the warning
- `abort` -- stop the entire session
- `skip_case` -- skip the current test case

### Streaming Progress

On `cerberus_run`, the server pushes `notifications/progress` events in real
time (added v0.5.0) so MCP hosts can observe progress without polling
`cerberus_status`. Polling remains available as a fallback for hosts that do
not surface server-initiated notifications.

## Escalation Checkpoints

The MCP server pauses execution at four checkpoints and waits for a human
decision via `cerberus_decide`:

| Checkpoint           | Trigger                              |
|----------------------|--------------------------------------|
| `budget_warning`     | Token usage exceeds 80% of budget    |
| `systemic_failure`   | 5 consecutive test failures          |
| `destructive_risk`   | Destructive action detected (DELETE, DROP, rm) |
| `target_unreachable` | 3 consecutive connection failures    |

## Example Session

```
User:      Run a smoke test on https://api.example.com

Claude:    [calls cerberus_run with goal="smoke test", url="https://api.example.com"]
           Session started: sess_abc123

Claude:    [calls cerberus_status for sess_abc123]
           Status: running, 3/10 cases complete

Claude:    [calls cerberus_status]
           Status: pending_decision
           Event: destructive_risk — DELETE /users/:id detected

User:      Skip that test case

Claude:    [calls cerberus_decide with action="skip_case"]
           Resumed.

Claude:    [calls cerberus_report for sess_abc123]
           9/10 passed, 1 skipped.
```

## Orphan Recovery

If the MCP server crashes mid-session, running `cerberus mcp` again recovers
orphaned sessions by marking them as `interrupted`.
