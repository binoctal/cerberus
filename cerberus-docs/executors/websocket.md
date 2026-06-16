# WebSocket Executor

The WebSocket executor tests real-time communication endpoints. It supports
`ws_connect` for establishing connections and `ws_send` for sending messages
and reading responses.

## URL Auto-Conversion

The executor automatically converts HTTP(S) URLs to WebSocket URLs:

| Input                    | Resolved                |
|--------------------------|-------------------------|
| `http://host:8080/ws`    | `ws://host:8080/ws`     |
| `https://host:8080/ws`   | `wss://host:8080/ws`    |
| `ws://host:8080/ws`      | `ws://host:8080/ws`     |

## Actions

### `ws_connect`

| Field    | Type              | Required | Description          |
|----------|-------------------|----------|----------------------|
| `url`    | string            | yes      | WebSocket URL        |
| `headers`| map[string]string | no       | HTTP headers for handshake |

Connects to the server and reads one message with a **5-second** timeout as a
handshake confirmation.

### `ws_send`

| Field   | Type   | Required | Description                |
|---------|--------|----------|----------------------------|
| `url`   | string | yes      | WebSocket URL              |
| `message`| string | yes      | Message to send (text)    |

Sends a text message and reads one response with a **10-second** timeout.

## Examples

### Connect to echo server

```json
{
  "action_type": "ws_connect",
  "url": "ws://localhost:8080/echo"
}
```

### Send message and read response

```json
{
  "action_type": "ws_send",
  "url": "ws://localhost:8080/echo",
  "message": "Hello, WebSocket!"
}
```

### Authenticated connection

```json
{
  "action_type": "ws_connect",
  "url": "wss://api.example.com/ws",
  "headers": {
    "Authorization": "Bearer token123"
  }
}
```

### Send JSON payload

```json
{
  "action_type": "ws_send",
  "url": "wss://api.example.com/ws",
  "message": "{\"type\": \"subscribe\", \"channel\": \"updates\"}"
}
```

## Result

- **Success** -- connection established / message sent and response received
- **Evidence** -- received message content
- **Duration** -- time for full round-trip

## Notes

- Uses the `nhooyr.io/websocket` library
- Each action creates a new connection; connections are not persisted between actions
- Context cancellation is respected at every stage
