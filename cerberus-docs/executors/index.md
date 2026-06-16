# Executors

Cerberus routes test actions to specialized executors via the **MultiExecutor**. Each executor handles specific action types.

## Built-in Executors

| Executor | Action Types | Description |
|----------|-------------|-------------|
| **HTTP** | `api_request`, `navigate` | HTTP API calls with method/headers/body |
| **Database** | `db_query`, `db_assert` | SQL queries with assertion validation |
| **GraphQL** | `graphql_query` | GraphQL queries with variables |
| **WebSocket** | `ws_connect`, `ws_send` | WebSocket connect and message exchange |
| **Browser** | `browser_goto/click/fill/eval` | Browser automation via Playwright/CDP |
| **Process** | `process_exec`, `process_build` | Shell command execution |
| **File** | `file_read/write/exists/glob` | File system operations |
| **Code** | `code_analyze/lint/symbols` | Language-aware code analysis |
| **MCP** | `mcp_call` | MCP server tool calls |
| **Wait** | `wait` | Timed delays |

## Rule Engine

The **Rule Engine** matches test cases to executors **without LLM calls** (zero tokens). Known patterns (URL paths, action names) are routed deterministically. Only novel cases fall through to the ReAct loop.

## Plugin System

Extend Cerberus with custom executors via the `ExecutorPlugin` and `RulePlugin` interfaces:

```go
type ExecutorPlugin interface {
    Name() string
    Executor() TypedExecutor
    ActionTypes() []types.ActionType
}
```

Register plugins through `PluginRegistry` before calling `ApplyTo()`.
