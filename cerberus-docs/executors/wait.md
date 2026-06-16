# Wait Executor

The wait executor introduces timed delays. Useful for waiting for asynchronous
operations, server startup, or rate limiting.

## Actions

### `wait`

Pause execution for a specified duration.

| Field      | Type   | Required | Description                        |
|------------|--------|----------|------------------------------------|
| `duration` | string | yes      | Go duration string (e.g. `"5s"`)  |

Valid duration units: `ns`, `us`, `ms`, `s`, `m`, `h`.

## Configuration

- No external dependencies
- Respects context cancellation (abortable)

## Examples

### Wait 5 seconds

```json
{
  "action_type": "wait",
  "duration": "5s"
}
```

### Wait 500 milliseconds

```json
{
  "action_type": "wait",
  "duration": "500ms"
}
```

### Wait 1 minute

```json
{
  "action_type": "wait",
  "duration": "1m"
}
```

## Result Fields

- **Success** — always `true` unless context was cancelled
- **Summary** — actual duration waited
- **Evidence** — start time and end time
