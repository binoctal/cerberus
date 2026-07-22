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
| `message` | string | yes | Message to send. Under `binary` framing this is base64 (standard padding); decoded bytes are written as a binary frame. Under `json`/`text` (or no protocol) it is written as-is as a text frame. |

### `ws_receive`
| Field | Type | Required | Description |
|---|---|---|---|
| `connection_id` | string | yes | Connection to read from |
| `type` | string | yes | The awaited message. Under `json`/no-protocol: the value at `type_path` (top-level `type` by default). Under `text`: the whole frame must equal this string. Under `binary`: base64 (standard padding) of the exact expected bytes. |
| `timeout` | int | no | Seconds (default 10) |
| `decisive` | bool | no | Set true on the receive that should pass the case; defaults to false (intermediate) |
| `assert` | map[string]any | no | Path-to-value equality checks on the matched message (see [Field assertions](#field-assertions)) |

Non-matching messages are kept as evidence. At most one `decisive=true`
receive per case.

#### Field assertions

`assert` optionally declares deterministic, executor-side content checks on the
matched message — precise, immediate, and not deferred to the Examiner LLM.
Each key is a dotted JSON object path (e.g. `payload.approved`); each value is
the expected scalar (`bool`, `string`, `number`, or `null`).

```yaml
ws_receive:
  connection_id: conn1
  type: approval
  assert:
    payload.approved: true
    payload.role: admin
```

**Evaluation.** After the routing-key match succeeds (M1 behavior, unchanged),
the executor walks each path in the matched JSON message and compares the leaf
to the expected value. **All entries must hold** for the receive to succeed.
Go map iteration is non-deterministic, so entries are evaluated in **sorted
path order** — the error message names the lexicographically-first failing
path, so multi-assertion failures are reproducible.

**Failure semantics.** On the first failing entry, the receive returns
`OK=false` with an error of the form
`receive: assert <path>: expected <v>, got <actual-or-missing>`. The matched
message is still returned as `MatchedMessage` (evidence preserved), and any
non-matching messages accumulated so far remain in `SeenMessages`. For a
`decisive` receive, a failing assert fails the decisive verification step —
the case fails, exactly as a decisive receive that times out.

**Constrained equality (no evaluator).** `assert` is path-to-value equality
only. No `!=`, `>`, `contains`, boolean logic, array indexing, or wildcards
(M0 Constraint 3 preserved). A path that is absent or that traverses a
non-object node reports `got <missing>`; an explicit JSON `null` expected
value asserts the field is present-and-null (a distinct case from absent).

**Numeric normalization.** JSON decodes all numbers to `float64`, so an
expected `5` (YAML/JSON int) and an actual `5` (decoded float) compare equal
— `valueEqual` compares numeric operands as `float64`. No other coercion is
applied (`"5"` and `5` do not match).

**M1 fallback.** An absent or empty `assert` is byte-identical to M1: the
receive is arrival-only, and content is judged by the Examiner against the
free-text case expectation. Soft/non-failing observation is achieved by
omitting `assert` — there is no separate "warn" mode.

Design rationale and rejected alternatives (action-side vs. Examiner-side,
`ws_assert` vs. `assert` on `ws_receive`, array paths, `null` semantics) are
in the M2 field-assertions design spec:
[`cerberus-docs/superpowers/specs/2026-07-21-ws-field-assertions-design.md`](../superpowers/specs/2026-07-21-ws-field-assertions-design.md).

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
  Content is judged by the Examiner by default, or checked deterministically via
  `assert` on `ws_receive` (see [Field assertions](#field-assertions), M2) —
  `assert` works with or without a protocol declaration.
  Configurable type-field paths, auth injection, and framing selection are
  declarable via the [Protocol declaration](#protocol-declaration) (M1).
- Roles and the per-role mandatory handshake are declarable via
  [Protocol declaration > Roles](#roles) (M2). Field-level `assert` checks on
  `ws_receive` are documented under [Field assertions](#field-assertions) (M2).
  `text`/`binary` framing is documented under [Framing](#framing) (M2).

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
| `framing` | string | `json` | Wire framing: `json` (default), `text`, or `binary`. See [Framing](#framing). |
| `type_path` | string | `type` | Dotted path to the message-routing key (e.g. `type`, `data.event`). No array indexing or JSONPath. Empty also means top-level `type`. |
| `auth` | object | — | Optional. Declares where credentials go and which actor supplies them. |
| `auth.strategy` | string | — | `query` (url query param), `header` (dial header), or `subprotocol` (negotiated subprotocol entry). |
| `auth.param` | string | — | Query-param name, header name, or subprotocol name. Required when `auth` is set. |
| `auth.credential_ref` | string | — | Names an entry in `actors[]` whose resolved raw token is injected at `auth.param`. Must name a real actor. |

### Inline or referenced

The protocol declaration may be written **inline** on the service (`protocol:`),
or **referenced** by name (`protocol_ref: <name>`) from a standalone file
`.cerberus/protocols/<name>.yaml` loaded at config time. The two are mutually
exclusive. A referenced file is a YAML serialization of the same `Protocol`
fields documented here (framing, type_path, auth, roles) and behaves identically
once loaded. See the [project configuration reference](../configuration/project.md).

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

### Roles

A service with multiple distinct connection types (e.g. a `web` client and a
`bridge` device on the same realtime endpoint) may declare named **roles**
under `protocol.roles`. A role bundles the credential (`credential_ref`),
discriminator facts (`params`/`headers`/`subprotocols`), and an optional
mandatory `handshake`. The LLM only names the role
on `ws_connect`; the executor expands the bundle.

```yaml
protocol:
  framing: json
  type_path: type
  auth: { strategy: query, param: token, credential_ref: web-actor }
  roles:
    web:
      credential_ref: web-actor
      params: { type: web }
      handshake: { await_type: devices:sync, timeout: 5 }
    bridge:
      credential_ref: bridge-actor
      params: { type: bridge }
```

| Field | Type | Default | Description |
|---|---|---|---|
| `roles.<name>.credential_ref` | string | — | Names an entry in `actors[]` whose resolved raw token is injected for this role. Overrides `protocol.auth.credential_ref` for this connection. |
| `roles.<name>.params` | map[string]string | — | Discriminator query params applied (strip-then-inject) to the dial url. Must not include `protocol.auth.param` when `auth.strategy` is `query` (token-slot collision is rejected by validation). |
| `roles.<name>.headers` | map[string]string | — | Discriminator dial headers strip-then-injected (delete-then-set). Must not include `auth.param` when `auth.strategy` is `header`. |
| `roles.<name>.subprotocols` | []string | — | Discriminator subprotocol names offered (strip-then-injected: remove-then-append). Must not include `auth.param` when `auth.strategy` is `subprotocol`. |
| `roles.<name>.handshake` | object | — | Optional mandatory post-connect exchange. When set, the executor auto-awaits `await_type` (matched at `protocol.type_path`) before the connect returns success. |
| `roles.<name>.handshake.await_type` | string | — | Routing-key value to wait for. Required when `handshake` is set. |
| `roles.<name>.handshake.timeout` | int | — | Seconds to wait; must be > 0 (validation) so a mandatory handshake cannot hang a case indefinitely. |

`ws_connect` gains a `role` field:

| Field | Type | Required | Description |
|---|---|---|---|
| `role` | string | no | Names a declared protocol role. When set, the executor expands the role's credential, discriminator params/headers/subprotocols, and handshake; `credential_ref` on the action is ignored. |

**Executor expansion (strip-then-inject + auto-handshake):** when `role` is set,
the executor (1) resolves the role (unknown role, or a role on a service
without a `protocol:`, fails the connect with a non-secret error and does not
dial); (2) resolves the effective credential as `role.credential_ref` →
`protocol.auth.credential_ref`; (3) reuses M1's `injectAuth` to strip any
LLM-supplied value at `auth.param` and inject the resolved raw token; (4)
strip-then-injects each of `role.params` into the url query (delete then set,
so an LLM-supplied `?type=web` is normalized to exactly the role's value);
and (5) strip-then-injects each of `role.headers` (delete-then-set) and each
entry of `role.subprotocols` (remove-then-append) onto the dial — normalizing
any LLM-supplied value at those slots to exactly the role's; and (6) after
dial, if the role declares a `handshake`, runs an internal
receive loop (guarded by the connection's `readMu`, matching via
`extractTypePath(data, type_path)`) until `await_type` arrives or `timeout`
elapses. Non-matching messages during the handshake are accumulated as
evidence (same as `ws_receive`), not consumed silently.

**Handshake is non-decisive:** `await_type` arrival means the connection is
*ready*, not that the case passed — `ws_connect` stays an intermediate step.
The matched handshake message plus any non-matching handshake-period messages
go into the connect `WSResult.SeenMessages` (reusing the existing field), so
the exchange is visible to the Examiner; `MatchedMessage` stays empty for a
connect. On timeout, the dial succeeded but the mandatory handshake did not
complete, so the connection is unusable — the executor closes the connection,
removes its entry from the table (explicit cleanup), and returns a failure
result, driving the M0 recovery/retry path.

**No role → M1 fallback:** a `ws_connect` without `role` (or a service without
`roles:`) uses `protocol.auth.credential_ref` (or the action's
`credential_ref`) and no auto-handshake — exactly M1. Roles are a graceful
enhancement, never a replacement; M1 secret hygiene (strip-then-inject,
pre-injection url in `WSResult`, redaction backstop) is preserved verbatim.

Role discovery — how the LLM learns role names, given the static steer prompt
cannot carry them — remains an Open Question. M2 ships the mechanism plus the
graceful M1 fallback above; value-realization is via dogfooding / M3
(Scout-generated WS cases that emit role connects from the protocol
declaration). Design rationale and rejected alternatives are in the M2 design
spec:
[`cerberus-docs/superpowers/specs/2026-07-21-ws-realtime-engine-m2-roles-design.md`](../superpowers/specs/2026-07-21-ws-realtime-engine-m2-roles-design.md).

### Framing

`protocol.framing` declares the wire framing for the connection. It bundles
three facts the executor derives and acts on deterministically; the LLM only
authors the `message`/`type` content in the declared form.

| framing | send | receive match | `assert` |
|---|---|---|---|
| `json` (default) | text frame, message as-is | value at `type_path` equals `type` | path→value on the matched JSON |
| `text` | text frame, message as-is | whole frame text equals `type` (exact) | rejected (json-only) |
| `binary` | binary frame, `message` is base64 → bytes | whole frame bytes equal base64-decoded `type` (exact) | rejected (json-only) |

**Binary codec.** A JSON string cannot carry arbitrary bytes, so binary content
travels as base64 (`encoding/base64.StdEncoding`, standard padding) in the
string-typed `message` (send), `type` (receive match target), and
`MatchedMessage`/`SeenMessages` (receive result). An invalid-base64 `message`
on send, or `type` on receive, fails fast with a clear non-secret error (the
receive error fires before any read, rather than timing out).

**Matching is exact equality only.** `text` matches the whole frame string;
`binary` matches the whole frame bytes. There is no substring/prefix/regex
predicate (that would be an expression engine — M0 Constraint 3). When the full
frame cannot be predicted ahead (a computed binary response, a variable text
payload), exact-match is not usable as `type`; fall back to a non-decisive
`ws_receive` and let the Examiner judge content — the same fallback used for
unpredictable JSON field values. Scan-and-filter is preserved in all framings:
non-matching frames still accumulate into `SeenMessages`.

`assert` is JSON-only; under `text`/`binary` a receive with a non-empty `assert`
fails immediately with `receive: assert requires json framing`.

Design rationale (why exact-match over receive-next, why base64 over hex) and
the dogfooding recourse are in the M2 framing design spec:
[`cerberus-docs/superpowers/specs/2026-07-21-ws-framing-design.md`](../superpowers/specs/2026-07-21-ws-framing-design.md).

### Scout-generated cases (M3-2)

When a service declares a `protocol:` with roles, Scout auto-generates WS test
cases from it: one `ws_connect` setup per declared role plus a decisive
`ws_receive` per verification point (the role's handshake `await_type` and any
**receive-directed** routing type named in the goal), linked by `DependsOn`. A type whose immediately preceding word in the goal is a send-verb (`send`/`sends`/`sending`/`emit`/`emits`/`publish`/`publishes`) is client-sent, so it is not turned into a receive case. Tokens without a send-verb context default to receive. (Provisional — tune via dogfooding.) The `DependsOn` is
**ordering-only**: WebSocket connections are namespaced per case (see
[Per-case namespacing](#per-case-namespacing--receive-serialization)), so the
`ws_connect` setup case's connection is not shared with the receive cases — each
case connects independently within its own Steer loop. Sharing one connection
across a connect→send→receive sequence requires a single multi-step case (the
`TestCase.Steps` path; see [Deterministic multi-step cases](#deterministic-multi-step-cases-steps) below). The
Steer LLM still orchestrates the actual connect/send/receive within each case
(skeleton + fill), so declaring a protocol makes Scout ask for the WS scenario
deterministically instead of relying on the Steer LLM to improvise it each run. Design and trigger
rationale:
[`cerberus-docs/superpowers/specs/2026-07-21-ws-scout-cases-design.md`](../superpowers/specs/2026-07-21-ws-scout-cases-design.md).

### Deterministic multi-step cases (Steps)

A `TestCase` may carry ordered `Steps` (`connect → send → receive → assert`)
that execute **deterministically** — the Steer LLM does not improvise the action
sequence. Each step is a declarative `ws_connect`/`ws_send`/`ws_receive`
(`ws_disconnect`) carrying a `connection_id`; steps citing the same
`connection_id` share one connection (the per-case `<caseID>:<connection_id>`
table entry — see [Per-case namespacing](#per-case-namespacing--receive-serialization)).
The first failed step short-circuits the case; the decisive verdict is the final
`ws_receive`'s field assertions (constrained dotted-path → value, no expression
engine). Scout emits a `Steps` case when the goal pairs a client-sent type with
a following receive type — e.g. "send `device:command`, verify `device:ack`
approved=true" ⇒ `ws_connect` (role) → `ws_send {type:device:command}` →
`ws_receive device:ack` asserting `payload.approved=true`, all on one connection.
Goals without such an exchange keep the connect + receive-case form above.
Coexistence: non-`Steps` cases (HTTP, process, ad-hoc WS) are unchanged. Design:
[`cerberus-docs/superpowers/specs/2026-07-23-ws-deterministic-steps-design.md`](../superpowers/specs/2026-07-23-ws-deterministic-steps-design.md).

### M0 fallback

A service without a `protocol:` block behaves exactly as M0: `ws_receive`
matches top-level `type`, framing is JSON (text frames), and auth is **not**
auto-injected (the LLM puts credentials into url/headers/subprotocols itself).
A declared `text`/`binary` framing is a strict enhancement, never a
replacement.

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
