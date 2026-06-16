# Process Executor

The process executor runs shell commands in a sandboxed environment. It supports
`process_exec` for arbitrary commands and `process_build` for build pipelines
with automatic dependency installation.

## Actions

### `process_exec`

| Field     | Type              | Required | Description              |
|-----------|-------------------|----------|--------------------------|
| `command` | string            | yes      | Command to execute       |
| `args`    | []string          | no       | Command arguments        |
| `env`     | map[string]string | no       | Environment variables    |
| `workDir` | string            | no       | Working directory        |
| `timeout` | string            | no       | Go duration (e.g. `"60s"`) |

Default timeout: **60 seconds**. Context cancellation is respected.

### `process_build`

Same fields as `process_exec`, plus automatic dependency detection:

- If `go.mod` exists: runs `go mod download` first
- If `package.json` exists: runs `npm install` first

Build actions receive a broader sandbox policy (read/write filesystem, outbound
network for downloading dependencies).

## Sandbox Isolation

All process execution runs through a `Sandbox` interface. On Linux, the executor
attempts namespace-based isolation. If unavailable, it falls back to a no-op sandbox.

The sandbox restricts:

- Filesystem access to the project directory
- Network access (except for build actions)
- Resource limits

## Policy Override

Sandbox policy can be customized via `.cerberus/policy.yaml`:

```yaml
process:
  allow_network: false
  allow_write: false
  max_memory_mb: 512
  timeout: 120s
```

## Examples

### Run tests

```json
{
  "action_type": "process_exec",
  "command": "go",
  "args": ["test", "-v", "-race", "./..."],
  "timeout": "120s"
}
```

### Build project

```json
{
  "action_type": "process_build",
  "command": "go",
  "args": ["build", "-o", "bin/app", "./cmd/app"]
}
```

### Command with environment

```json
{
  "action_type": "process_exec",
  "command": "npm",
  "args": ["test"],
  "env": {
    "NODE_ENV": "test",
    "CI": "true"
  },
  "workDir": "./frontend"
}
```

### Lint

```json
{
  "action_type": "process_exec",
  "command": "golangci-lint",
  "args": ["run", "./..."],
  "timeout": "90s"
}
```

## Result

- **Success** -- exit code 0
- **Evidence** -- stdout and stderr output
- **Duration** -- wall-clock execution time
