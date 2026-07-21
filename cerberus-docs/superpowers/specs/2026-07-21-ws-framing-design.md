# WebSocket Realtime Engine (M2) — Text/Binary Framing (Design)

**Date:** 2026-07-21
**Status:** Design (brainstormed; pending spec review)
**Scope:** `internal/project/` (framing validation + schema comment), `internal/head/agent/` (websocket executor: framing-aware send/receive/handshake, codec helpers), `cerberus-docs/executors/websocket.md`
**Depends on:** M0 (`...-m0-design.md`), M1 (`...-m1-design.md`)
**Proposal:** `cerberus-docs/superpowers/specs/2026-07-20-ws-realtime-engine-m2-proposal.md`

## Background & Motivation

M1 reserves a `protocol.framing` field but accepts only `""`/`"json"`; `"text"` and
`"binary"` are rejected by validation (`internal/project/validate_protocol.go:7`,
`validProtocolFraming`). On the wire, M1 is also lopsided:

- **Send** (`doSend`, `websocket.go:433`) hard-codes
  `conn.Write(ctx, websocket.MessageText, []byte(a.Message))` — every send is a
  WS **text** frame. A binary-framed protocol (MessagePack, protobuf, a custom
  binary codec) cannot be exercised at all.
- **Receive** (`doReceive`, `websocket.go:466`) discards the frame type returned
  by `conn.Read` (`_, data, err := ...`) and **always** JSON-decodes via
  `extractTypePath` to find the routing key. A non-JSON text frame or any binary
  frame fails to decode, so the routing key is never found and the receive
  times out.

So cerberus can today only orchestrate JSON-over-text realtime protocols. This
sub-project closes that gap: the executor truly supports **text** and **binary**
frames on both send and receive, driven by the `protocol.framing` declaration.

The architecture philosophy is unchanged from M0/M1/M2:

- **Protocol details stay with the LLM.** The executor handles only generic,
  wire-level mechanics (the WS opcode, a byte↔text codec, a trivial match
  predicate). It does not parse MessagePack/protobuf or learn any system's
  business semantics.
- **No runtime expression evaluator** (M0 Constraint 3). The receive match
  predicate is plain equality (`==` / `bytes.Equal`), not a path/operator
  language. `assert` remains a JSON-only path→value feature.

## Goal

A framing declaration that makes the WS executor send and receive the correct
frame type with the correct content codec, while preserving M0/M1 behavior for
undeclared and JSON services.

Success criteria:

- `protocol.framing` accepts `json` (default), `text`, and `binary`.
- A `binary`-declared service: `ws_send` writes a **binary** frame whose payload
  is the decoded bytes of the action's `message`; `ws_receive` matches a
  **binary** frame whose bytes equal the decoded `type`.
- A `text`-declared service: `ws_send` writes a text frame as-is; `ws_receive`
  matches a text frame whose whole text equals `type`.
- A `json`-declared or undeclared service behaves exactly as M1/M0 (text frames,
  `type_path` routing, JSON `assert`). Byte-identical fallback.
- The roles handshake loop (`doConnect`) honors framing, so a text/binary
  protocol with a mandatory handshake still works.
- `make check` (fmt + lint + test -race) green; table-driven tests mirroring
  `internal/head/agent/websocket_test.go` and
  `internal/project/validate_protocol_test.go`.

## Non-Goals

- **Per-action `message_type` override.** Framing is protocol-level (declared on
  `protocol:`, applies to every send/receive on the connection). A protocol that
  mixes text and binary frames on one connection is not expressible; per-action
  override is deferred (see D1). This is YAGNI until a real mixed-frame target
  appears.
- **Binary format parsing** (MessagePack/protobuf/CBOR decoders). The executor
  treats binary payloads as opaque bytes. Content semantics are the LLM's /
  Examiner's job.
- **Substring / prefix / regex matching.** These creep toward an expression
  engine (M0 Constraint 3). The non-JSON predicate is exact equality only.
- **Array indexing / JSONPath.** Unchanged from M1 — no evaluator.
- **Auto-inference of framing** (Scout picking `text`/`binary` from docs/
  captures). That is M3.

## Design Decisions

### D1 — Framing is a protocol-level declaration (bundled opcode + codec + match predicate)

`protocol.framing` is one field on the existing `Protocol` block. It bundles
three tightly-coupled facts the executor derives from it: the **WS opcode**
(text vs binary), the **content codec** (raw string vs base64-decoded bytes),
and the **receive match predicate** (type-path routing vs exact equality). All
three are properties of the connection's protocol, not of an individual action,
so they live in the declaration — exactly M1's "protocol declares stable facts;
executor acts on them" model.

**Rejected — per-action `message_type` on `ws_send`/`ws_receive`.** This adds
fields to two action types, forces the LLM to set the opcode per action, and
exists only for mixed-frame protocols (text control frames + binary data frames
on one connection), which are rare. Protocol-level framing covers every
single-framing protocol (the common case). If a real mixed-frame target appears,
per-action override is an **additive, non-breaking** extension — declare a
connection default, let an action override it. Defer until needed (YAGNI).

**Rejected — three separate protocol fields** (`wire_opcode`, `codec`,
`match_mode`). Over-engineered: the three are never chosen independently in
practice (a binary frame wants binary bytes and byte-equality; a text frame
wants text and string-equality). One `framing` enum with three coherent bundles
is simpler and less error-prone to author.

### D2 — The receive match predicate (the core decision; recorded for dogfooding)

`ws_receive` is **not** "read one frame". It is *scan until a frame matching
`type` arrives, accumulating non-matching frames as evidence*:

```
while waiting for permission:response, the server may first push
devices:sync / heartbeat / other events → those non-matching frames go into
SeenMessages (evidence); only permission:response is returned as MatchedMessage.
```

This scan-and-filter loop is what `ws_receive` exists for: realtime protocols
interleave **async events with responses**, and the receive must filter the noise
and recognize the target. It underpins the `decisive` contract (target arrived ⇒
case passes), evidence accumulation, and timeout-as-failure. **Every framing
needs a match predicate** — otherwise the operation degenerates to "read next
frame".

For `json`, the predicate is M1's `type_path`: JSON-decode, read the routing key
at the dotted path, compare to `type`. This cannot be reused for `text`/`binary`
— there is no JSON structure to extract from a non-JSON text frame or an opaque
byte frame. So each framing gets its own minimal predicate:

| framing | match predicate |
|---|---|
| `json` (default) | `extractTypePath(data, type_path) == type` (M1, unchanged) |
| `text` | `string(data) == type` — whole-frame exact string equality |
| `binary` | `bytes.Equal(base64Decode(type), data)` — whole-frame exact byte equality |

All three are plain equality — deterministic, no evaluator — and all three
preserve scan-and-filter (non-matches still accumulate into `SeenMessages`).

**Rejected — `receive-next` for non-JSON** (drop `type` matching for `text`/
`binary`; return the next frame of the declared type; `type` becomes optional).
Simpler, but it **discards scan-and-filter**: an async event that arrives before
the response would be returned as `MatchedMessage`, with no way to wait
specifically for the target. That is strictly worse for the realtime
interleaving that is cerberus's reason for existing. It also splits the contract
(json/text match, binary does not), giving the LLM a special-case to remember.

**Honest shortcoming (the thing to validate in dogfooding).** For `binary`, and
for variable-length `text`, the orchestrator often **cannot predict the exact
frame bytes/text ahead of time** (a binary response is computed/encoded; a text
response carries a variable payload). In those cases exact-match is unusable as
a `type`. This is the same situation as a JSON protocol where a dynamic field
value cannot be predicted: the fallback is **non-decisive `ws_receive` +
Examiner judgment** (and `assert`, for JSON, likewise cannot pin a dynamic
value). Exact-match is the right tool when the full frame *is* predictable
(fixed tokens, handshake ACKs, binary magic-number headers); when it is not,
correctness degrades to the Examiner, exactly as in M0/M1. This is acceptable
and consistent — **not** a defect — but it is the behavior most likely to feel
limiting in practice. If dogfooding shows exact-match is too rigid for a real
binary target, the documented recourse is to revisit `receive-next` (or a
per-action override) for that framing; this spec records the trade-off so the
decision can be re-opened against evidence rather than re-derived.

### D3 — Binary content codec is base64

Action and result content fields (`WSSendAction.message`, `WSReceiveAction.type`,
`WSResult.MatchedMessage`, `WSResult.SeenMessages`) are all `string`, serialized
as JSON. A JSON string is Unicode text and **cannot carry arbitrary bytes** — a
binary frame may contain any byte `0x00–0xFF`, including invalid-UTF-8 sequences.
So binary bytes must be bridged to/from a string: on send, decode the string to
bytes and write a binary frame; on receive, encode the frame bytes to a string.
That bridge is the content codec.

**Decision: `encoding/base64.StdEncoding`** (standard alphabet, with padding),
applied uniformly to `message` (send), `type` (receive match target), and
`MatchedMessage`/`SeenMessages` (receive result).

**Rejected — hex.** Hex (`48656c6c6f`) is transparent for tiny hand-crafted
frames (a human reads ASCII byte values directly) and has a simpler alphabet
(no padding/case ambiguity), but: (1) any real binary protocol payload
(MessagePack/protobuf) is opaque in *both* encodings — you decode to inspect
either way — so hex's transparency wins only for toy frames; (2) base64 is the
universal convention for binary-in-JSON, so the LLM authors it reflexively and
any external tooling interops; (3) base64 is ~33% overhead vs hex's ~100%.
Transparency of small frames is a marginal, narrow win; convention, compactness,
LLM-familiarity, and interop are broad wins. base64.

**Variant:** `StdEncoding` (not URL-safe, not raw). Standard padding. Invalid
base64 in `message` (send) or `type` (receive) is a case-authoring error and
fails fast with a clear non-secret error (see Executor Changes).

### D4 — `assert` is JSON-only (runtime guard)

`assert` path-walks a JSON message (`extractPath` does `json.Unmarshal`). Under
`text`/`binary` framing there is no JSON to walk, so `assert` is meaningless.
Because `WSReceiveAction.Validate()` has no protocol context (framing is known
only at runtime, from the connection's stashed `*Protocol`), this cannot be a
config-time check. `doReceive` guards it at runtime: if the effective framing is
not `json` and `len(a.Assert) > 0`, the receive returns
`OK=false` with `error: "receive: assert requires json framing"` immediately
(no read, no dial effect). Under `text`/`binary`, the exact-match `type` already
pins the full expected frame, so `assert` would be redundant even if it were
defined.

### D5 — The roles handshake loop shares the framing-aware matcher

`doConnect`'s mandatory-handshake loop (`websocket.go:226–235`) currently matches
`await_type` via `extractTypePath`. Under `text`/`binary` framing that must use
the same framing-appropriate predicate, or a text/binary protocol with a
handshake never matches and the connect fails on timeout. Both the receive loop
and the handshake loop call one shared helper (see Executor Changes), so framing
semantics are defined in exactly one place. (`entry.protocol` is already stashed
on the `wsEntry` by M1, so the handshake path can read framing the same way
`doReceive` does.) An invalid-base64 `await_type` under binary framing manifests
as handshake timeout (the match never succeeds) — acceptable and rare; the
connect already fails cleanly on handshake timeout (M2-roles behavior).

### D6 — Fallback (no protocol, or empty framing) is byte-identical to M1/M0

A service with `Protocol == nil`, or a `protocol:` with `framing: ""`, is
treated as `json`: `ws_send` writes a text frame as-is; `ws_receive` matches via
`type_path` (default top-level `type`); `assert` works. This is the M0/M1 path,
unchanged. Framing is a strict enhancement, never a replacement.

## Schema & Validation Changes

**`internal/project/protocol_schema.go`** — update the `Framing` field comment
from "M1 supports json only; text/binary reserved for M2" to state all three
values and their meaning (text = text frame + exact-string match; binary =
binary frame + base64 codec + exact-byte match).

**`internal/project/validate_protocol.go`** —

```go
// validProtocolFraming is the set of framing values the WS executor supports.
var validProtocolFraming = map[string]bool{"": true, "json": true, "text": true, "binary": true}
```

and the error message loses the "in M1" framing (it is no longer M1-scoped):

```go
return fmt.Errorf("protocol.framing %q must be json, text, or binary", p.Framing)
```

No new validation rules: `text`/`binary` need no `type_path` or `auth` and impose
no extra constraints. (`type_path` under `text`/`binary` is simply unused by the
match predicate — it remains valid to declare and is ignored, which keeps the
schema permissive and avoids coupling framing to `type_path` presence.)

## Executor Changes

All framing logic keys off one helper. `entry.protocol` (M1) already carries the
declaration; framing is `entry.protocol.Framing`, defaulting to `""` (= json)
when the entry has no protocol.

```go
// framingOf returns the effective wire framing for a connection. Empty (no
// protocol, or protocol with no framing) means json — the M0/M1 default.
func framingOf(entry *wsEntry) string {
	if entry.protocol != nil {
		return entry.protocol.Framing
	}
	return ""
}
```

**Framing-aware match predicate** (`ws_protocol.go`, alongside `extractPath`).
One helper used by both `doReceive` and the handshake loop:

```go
// matchType reports whether a received frame satisfies the awaited type under
// the connection's framing. json routes by type_path; text matches the whole
// frame text exactly; binary matches the whole frame bytes exactly (type is
// base64). A binary type that is not valid base64 never matches.
func matchType(framing string, data []byte, want, typePath string) bool {
	switch framing {
	case "text":
		return string(data) == want
	case "binary":
		got, err := base64.StdEncoding.DecodeString(want)
		if err != nil {
			return false
		}
		return bytes.Equal(got, data)
	default: // "" or "json"
		t, ok := extractTypePath(data, typePath)
		return ok && t == want
	}
}
```

**Result encoding** — `WSResult.MatchedMessage`/`SeenMessages` are `string`. For
binary framing they carry base64; otherwise the raw text (M1 behavior):

```go
// frameForResult renders received bytes for a WSResult string field under the
// connection's framing. binary frames are base64-encoded; text/json frames are
// the raw UTF-8 text.
func frameForResult(framing string, data []byte) string {
	if framing == "binary" {
		return base64.StdEncoding.EncodeToString(data)
	}
	return string(data)
}
```

**`doSend`** (`websocket.go:425`) — pick opcode + codec from framing:

- framing `binary`: decode `a.Message` (base64.StdEncoding) to bytes; on error
  return `WSResult{OK:false, Err:"send: message is not valid base64", ...}`
  (no write). On success `conn.Write(ctx, websocket.MessageBinary, decoded)`.
- framing `""`/`json`/`text`: unchanged — `conn.Write(ctx, websocket.MessageText, []byte(a.Message))`.
  (json and text sends are byte-identical: a text frame carrying the message
  string as-is.)

**`doReceive`** (`websocket.go:439`) —

1. Resolve `framing := framingOf(entry)`; `path` (type_path) as today.
2. **D4 guard:** if `framing != "" && framing != "json" && len(a.Assert) > 0`,
   return `OK:false, Err:"receive: assert requires json framing"` (no read).
3. **Fast-fail invalid binary type:** if `framing == "binary"`, decode `a.Type`
   once; on error return `OK:false, Err:"receive: type is not valid base64"`
   (avoids a confusing timeout when the target can never match).
4. Read loop as today (`readMu`-guarded), but:
   - match via `matchType(framing, data, a.Type, path)` instead of the inline
     `extractTypePath` call;
   - on match, evaluate `checkAsserts` **only when framing is json** (the D4
     guard already excluded non-json + assert; for json this is the M1 path,
     byte-identical when `assert` is empty);
   - `MatchedMessage` and `SeenMessages` entries pass through `frameForResult`
     so binary frames appear as base64.

The json path (no protocol, or `framing: json`, `assert` empty) is provably
identical to M1: `matchType` default case is exactly the current
`extractTypePath` comparison, and `frameForResult` returns `string(data)`.

**Handshake loop** (`doConnect`, `websocket.go:226–235`) — replace the inline
`extractTypePath(data, path); t == role.Handshake.AwaitType` with
`matchType(framingOf(entry), data, role.Handshake.AwaitType, path)`. The
`framing`/`path` locals are already in scope in `doConnect`. Non-matching
handshake-period frames still accumulate into the connect `WSResult.SeenMessages`
(M2-roles behavior, unchanged).

**Unchanged:** `MultiExecutor` routing, action registry/deref groups (no new
action type — framing reuses the existing `Protocol.Framing` field and the
existing `message`/`type` string fields), Scout, the decisive/intermediate
judgment, the Examiner, secret hygiene (framing touches neither credentials nor
the result url), concurrency (`readMu` guards the read loop exactly as before;
different connections still parallelize).

## Judgment Model

Unchanged from M0/M1/M2. `ws_send`/`ws_disconnect` and non-decisive `ws_receive`
stay intermediate; a `decisive=true` receive whose match predicate holds passes
the case. Framing changes *how* the match predicate is evaluated, not the
decisive/intermediate contract or the Phase-7 recovery guard. Content judgment
beyond the exact-match `type` remains the Examiner's job (or `assert`, for json)
— D2's shortcoming is intentional: when the exact frame is unpredictable, fall
back to non-decisive receive + Examiner.

## Testing Strategy

Table-driven, mirroring `internal/head/agent/websocket_test.go`,
`internal/head/agent/http_test.go`, and
`internal/project/validate_protocol_test.go`. A capture test WS server (existing
`newWSTestServerCapture` pattern) records `(opcode, payload)` for send assertions
and replays frames for receive assertions.

- **validation:** `text`/`binary`/`json`/`""` accepted; an invalid value (e.g.
  `"raw"`) rejected with the new error string (no "in M1").
- **send opcode + payload:** json/text framing → text frame, payload == message;
  binary framing → binary frame, payload == `base64Decode(message)`; binary +
  invalid-base64 message → `doSend` error, no frame written; no protocol (M0) →
  text frame (regression).
- **receive match:** text exact-match returns `MatchedMessage` and accumulates
  non-matches into `SeenMessages`; text non-match until timeout → `OK:false`;
  binary exact-bytes match (type = base64 of expected frame); binary non-match
  accumulates; binary invalid-base64 `type` → fast error; json unchanged
  (regression, incl. `assert`).
- **assert guard:** non-json framing + non-empty `assert` →
  `"receive: assert requires json framing"` (no read).
- **result encoding:** binary `MatchedMessage`/`SeenMessages` are base64 of the
  frame bytes; text/json are the raw text.
- **handshake under framing:** a `text`-framed protocol with a mandatory role
  handshake — `await_type` matched via exact-string; non-matching handshake
  frames accumulate; success returns the connect normally.
- **concurrency:** two concurrent `ws_receive` on one connection still serialize
  via `readMu; `-race` clean (unchanged from M1/M2).
- **fallback:** a service with `Protocol == nil` behaves as M0 (text send,
  type_path receive) — byte-identical.

Integration against a live binary/text realtime target is **deferred** (no such
target is running in this session); the framing facts are encoded as fixture/test
shapes, as in M1/M2.

## Relationship to M0 / M1 / M2 / M3

- **M0/M1** are not rewritten; framing layers a third `protocol.framing` value
  pair on top of the existing declaration, with byte-identical fallback.
- **M2-roles** composes: the handshake loop becomes framing-aware (D5). A role
  on a `text`/`binary` protocol works the same as on `json`.
- **M2-field-assertions** composes: `assert` remains json-only (D4); the guard
  makes the boundary explicit.
- **M3** may have Scout infer `framing` from descriptions/docs/captures and emit
  `text`/`binary` cases. The dogfooding signal most worth collecting (D2) is
  whether exact-match is workable for a real binary target, or whether
  `receive-next` / a per-action override should be revisited.

## Pitfalls (from M1/M2 implementation; do not repeat)

- Plan test snippets often use **unprefixed** action types (`WSSendAction`/
  `WSReceiveAction`) — `websocket_test.go` is `package agent` with **no alias**,
  so they need the `types.` prefix. The controller greps to confirm symbol
  locations (`newWSTestServerCapture`, `extractTypePath`, `framingOf`, etc.)
  before dispatching a task.
- Counters incremented by the capture-server goroutine must be `atomic`
  (`-race`).
- `promptSteerSystem` is a **single raw-string literal**: any steer-prompt edit
  is inline, no concatenation, no backticks. (This sub-project may not need a
  prompt edit at all; if it does, edit inline.)
- **Shipping a feature that changes existing doc wording → audit every doc
  bullet that still carries the old wording.** `websocket.md` currently says
  framing is "json only … text/binary reserved for M2 and rejected by
  validation" in at least three places (the `framing` table row, a Notes bullet,
  the M0-fallback section). All must be updated; a stale "reserved for M2" bullet
  is exactly the M2-field-assertions final-review defect class.
- Base64 string comparisons are exact (case-sensitive, padding-sensitive); test
  fixtures must use canonical `StdEncoding` output, not hand-written shorthand.

## Open Questions

1. **Exact-match workability for binary (D2).** Does a real binary target let the
   orchestrator predict whole response frames, or is non-decisive + Examiner the
   norm? Validate in dogfooding; `receive-next` / per-action override is the
   documented recourse.
2. **Mixed-frame protocols (D1).** Does any real target send text and binary
   frames on one connection? If so, add per-action `message_type` (additive).
3. **Framing + roles param/handshake combinatorics.** `text`/`binary` framing
   with roles is covered (D5) but not yet exercised on a live target; the
   static test shape may miss protocol-specific quirks.
