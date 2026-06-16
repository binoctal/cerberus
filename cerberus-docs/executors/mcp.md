# MCP Executor

The MCP executor calls tools on external MCP (Model Context Protocol) servers.
It supports two transport modes: TCP and stdio.

## Actions

### `mcp_call`

| Field    | Type           | Required | Description               |
|----------|----------------|----------|---------------------------|
| `server` | string         | yes      | Registered server name    |
| `method` | string         | yes      | Tool/method to call       |
| `params` | map[string]any | no       | Method parameters         |

The `server` field must match a named endpoint configured in the executor's
server registry.

## Transport Modes

### TCP

Connects to `host:port`, sends a JSON-RPC request as newline-delimited JSON,
and reads the response. Deadline: **10 seconds**.

```go
MCPEndpoint{
    Name:    "my-service",
    Address: "localhost:3000",
}
```

### stdio

Launches a subprocess, sends JSON-RPC via stdin, reads response from stdout.
The subprocess is persistent -- reused across multiple calls. Read timeout:
**10 seconds**.

```go
MCPEndpoint{
    Name:    "my-tool",
    Command: "my-mcp-server",
    Args:    []string{"--verbose"},
}
```

## Examples

### Call a TCP MCP server

```json
{
  "action_type": "mcp_call",
  "server": "database-service",
  "method": "query",
  "params": {
    "sql": "SELECT COUNT(*) FROM users"
  }
}
```

### Call a stdio MCP tool

```json
{
  "action_type": "mcp_call",
  "server": "filesystem",
  "method": "read_file",
  "params": {
    "path": "/tmp/report.txt"
  }
}
```

### Retrieve available tools

```json
{
  "action_type": "mcp_call",
  "server": "my-service",
  "method": "tools/list",
  "params": {}
}
```

## Server Registration

Servers are registered when building the MultiExecutor:

```go
endpoints := map[string]MCPEndpoint{
    "database-service": {
        Name:    "database-service",
        Address: "localhost:3000",
    },
    "filesystem": {
        Name:    "filesystem",
        Command: "fs-mcp-server",
    },
}
```

## Result

- **Success** -- JSON-RPC response without error
- **Evidence** -- raw JSON-RPC response body
- **Duration** -- round-trip time

## Cleanup

Call `Close()` on the executor to terminate all persistent stdio subprocesses.
