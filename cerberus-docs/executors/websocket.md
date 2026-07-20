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
| `credential_ref` | string | no | Overrides the service `protocol.auth.credential_ref` for this connection (see [Protocol declaration](#protocol-declaration)). |

Opens a persistent connection. Put credentials in the url query string,
headers, or subprotocols as the target requires. When the service declares a
`protocol.auth`, omit credentials — the executor injects them.

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

- Without a `protocol:` block, matching is by top-level JSON `type` only (M0).
  Field-level assertions are judged by the Examiner from the received message.
  Configurable type-field paths, auth injection, and framing selection are
  declarable via the [Protocol declaration](#protocol-declaration) (M1).
- Handshake sequences, role abstraction, field-level assertions, and
  `text`/`binary` framing remain deferred to M2.

## Protocol Declaration

A service may declare its stable WebSocket protocol facts via an optional
`protocol:` block. When present, the executor consumes the declaration
deterministically — the LLM keeps orchestrating (which connection, when, what
message content) but no longer re-infers auth placement, the routing field, or
the wire framing on every run.

```yaml
services:
  - name: open-agents-realtime
    url: http://localhost:8787
    protocol:
      framing: json
      type_path: type
      auth:
        strategy: query            # query | header | subprotocol
        param: token                # query-param / header / subprotocol name
        credential_ref: web-actor   # names an entry in actors[]
```

| Field | Type | Default | Description |
|---|---|---|---|
| `framing` | string | `json` | Wire framing. M1 supports `json` only; `text`/`binary` are reserved for M2 and rejected by validation. |
| `type_path` | string | `type` | Dotted path to the message-routing key (e.g. `type`, `data.event`). No array indexing or JSONPath. Empty also means top-level `type`. |
| `auth` | object | — | Optional. Declares where credentials go and which actor supplies them. |
| `auth.strategy` | string | — | `query` (url query param), `header` (dial header), or `subprotocol` (negotiated subprotocol entry). |
| `auth.param` | string | — | Query-param name, header name, or subprotocol name. Required when `auth` is set. |
| `auth.credential_ref` | string | — | Names an entry in `actors[]` whose resolved raw token is injected at `auth.param`. Must name a real actor. |

### Executor-authoritative auth (strip-then-inject)

When `protocol.auth` is declared, the executor is authoritative over credentials
at `auth.param`:

1. **Strip** any value the LLM placed at `auth.param` (from the url query, the
   headers, or the subprotocols).
2. **Inject** the resolved raw token for the named actor at that slot.

The LLM should omit credentials for declared services (a best-effort hint is in
the steer prompt), but correctness does not depend on the LLM cooperating —
exactly one correct credential reaches the server either way. A `credential_ref`
on an individual `ws_connect` overrides the service default for that connection;
this is how M1 represents two distinct credentials on one service (e.g. a web
JWT and a bridge device token) without role abstraction.

The injected value is the actor's resolved **raw token** — the unformatted value
extracted at session setup (the same value used for `{token}` substitution),
cached alongside the formatted HTTP header so repeated connects do not re-run
the login. `InjectAs` formatting stays HTTP-only; a WS protocol that needs a
formatted header value (e.g. `Authorization: Bearer …`) is not expressible in
M1 and lands with roles in M2. If the named actor is missing or has no
resolvable token, `ws_connect` fails with a non-secret error and does not dial.

### M0 fallback

A service without a `protocol:` block behaves exactly as M0: `ws_receive`
matches top-level `type`, framing is JSON, and auth is **not** auto-injected
(the LLM puts credentials into url/headers/subprotocols itself). A nil
`protocol` is the zero-config default; a declared protocol is a strict
enhancement, never a replacement.

### Per-case namespacing & receive serialization

- **Namespacing:** the connection-table key is internally namespaced as
  `<caseID>:<connection_id>`, so parallel cases that happen to choose the same
  LLM-supplied `connection_id` (e.g. `"conn1"`) never collide. Auto-generated
  ids (`ws-<seq>`) remain globally unique. Case exit (normal, timeout, or
  cancellation) closes only that case's connections.
- **Serialization:** concurrent `ws_receive` calls on the same connection
  serialize through a per-connection read mutex (`coder/websocket` forbids
  concurrent `Read` on one conn). Different connections still run in parallel.

### Secret hygiene

The url returned in `WSResult` is the **pre-injection** url — the LLM-supplied
url with the auth param stripped, before the resolved token is injected — so a
stray LLM-supplied credential never reaches the result. As a backstop,
`WSResult.Summary()`/`Evidence()` redact known-sensitive query params
(`token`, `password`, `secret`, `key`, `apikey`, `api_key`, `authorization`)
to `<redacted>`. The resolved credential value is held only in local scope
during `ws_connect` and is never logged.

Design rationale and rejected alternatives are in the M1 design spec:
[`cerberus-docs/superpowers/specs/2026-07-20-ws-realtime-engine-m1-design.md`](../superpowers/specs/2026-07-20-ws-realtime-engine-m1-design.md).
