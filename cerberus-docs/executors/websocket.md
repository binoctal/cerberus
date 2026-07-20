# WebSocket Executor

Tests realtime communication over persistent WebSocket connections. The LLM
orchestrates four protocol-agnostic primitives; connections are referenced by
`connection_id` and live for the duration of a test case.

Uses `github.com/coder/websocket`.

## Actions

### `ws_connect`
| Field | Type | Required | Description |
|---|---|---|---|
| `url` | string | yes | WebSocket URL (http(s) auto-converts to ws(s)) |
| `headers` | map[string]string | no | Handshake headers |
| `subprotocols` | []string | no | WS subprotocols |
| `connection_id` | string | no | Name for this connection (assigned if omitted) |

Opens a persistent connection. Put credentials in the url query string,
headers, or subprotocols as the target requires.

### `ws_send`
| Field | Type | Required | Description |
|---|---|---|---|
| `connection_id` | string | yes | Connection from `ws_connect` |
| `message` | string | yes | Message to send |

### `ws_receive`
| Field | Type | Required | Description |
|---|---|---|---|
| `connection_id` | string | yes | Connection to read from |
| `type` | string | yes | Wait for a message whose top-level `type` matches |
| `timeout` | int | no | Seconds (default 10) |
| `decisive` | bool | no | Set true on the receive that should pass the case; defaults to false (intermediate) |

Non-matching messages are kept as evidence. At most one `decisive=true`
receive per case.

### `ws_disconnect`
| Field | Type | Required | Description |
|---|---|---|---|
| `connection_id` | string | yes | Connection to close |

## Lifecycle

Connections are bound to the per-case context and close automatically when the
case ends (normal exit, timeout, or cancellation). Parallel cases are isolated.

## Result

- **Success** — connect/send succeeded, or the awaited `type` arrived.
- **Evidence** — matched message plus non-matching messages seen.
- **Duration** — time for the operation.

## Notes

- Matching is by top-level JSON `type` only (M0). Field-level assertions are
  judged by the Examiner from the received message. Configurable type-field
  paths and a declarative protocol layer arrive in later milestones (M1/M2).
