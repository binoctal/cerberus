# WS Receive Type-Aliases / Match-Set (F4) — Design

Status: Design (autonomous; chosen 2026-07-24 as deferred polish, now requested).
Trigger: some WS protocols emit a logical message under one of several wire types —
notably open-agents coalesces `session:output` into `session:output-batch`
(`{payload:{lines:[…]}}`) every 50 ms. A `ws_receive` awaiting `session:output`
would miss the batched form. F4 lets one receive match a SET of types.

## Goal

A `ws_receive` (and a relay receive step) may declare additional type aliases that
also satisfy the match. cerberus receives the first frame whose type is the primary
`type` OR any alias, then applies field asserts to it. Backwards-compatible: no
aliases ⇒ today's single-type behavior, byte-identical.

## Approach (resolved fork)

**Action-level aliases** (not protocol-declared). The receive itself names the
match-set. This is sufficient for the use case (a receive awaiting `session:output`
lists `session:output-batch` as an alias), bounded, and backward-compatible.
Protocol-declared auto-aliases (Scout applies them automatically from a protocol
block) are a deferred, larger non-goal — they need a new schema concept and buy
only automation over what the LLM/author can already express per-receive.

## Design

### `WSReceiveAction` (`internal/types/actions_http.go`)

Add an `Aliases` field:

```go
type WSReceiveAction struct {
    ConnectionID string `json:"connection_id"`
    Type         string `json:"type"`
    // Aliases are additional routing types that also satisfy this receive (a
    // match-set). A frame whose type_path is Type OR any Aliases matches. Empty
    // ⇒ single-type behavior (backwards-compatible). Used for protocols that
    // emit one logical message under several wire types (e.g. session:output vs
    // session:output-batch). Asserts apply to whichever frame matched.
    Aliases  []string       `json:"aliases,omitempty"`
    Timeout  int            `json:"timeout,omitempty"`
    Decisive bool           `json:"decisive,omitempty"`
    Assert   map[string]any `json:"assert,omitempty"`
}
```

`Validate` is unchanged (Type still required; Aliases optional, may be empty).

### Match predicate (`internal/head/agent/ws_protocol.go`)

Keep `matchType(framing, data, want, typePath)` as-is (single type). Add a
set-aware helper that ORs over `{Type} ∪ Aliases`:

```go
// matchAnyType reports whether a received frame matches any of types under the
// connection's framing. It is matchType over a set; the empty set never matches.
func matchAnyType(framing string, data []byte, types []string, typePath string) bool {
    for _, t := range types {
        if matchType(framing, data, t, typePath) {
            return true
        }
    }
    return false
}
```

### `doReceive` (`internal/head/agent/websocket.go`)

Build the match-set once and use `matchAnyType`:

```go
want := []string{a.Type}
want = append(want, a.Aliases...)
matched, seen, status := readMatching(entry, func(m wsMsg) bool {
    return matchAnyType(framing, m.data, want, path)
}, timeout)
```

(For binary framing, each alias must be valid base64; `matchType` returns false for
an invalid-base64 alias, so the OR is safe. The "type not valid base64" fast-fail
in `doReceive` generalizes to: the primary `a.Type` must be valid base64 under
binary framing — aliases that are invalid base64 simply never match, no fast-fail
needed.)

### `TestStep` (`internal/head/agent/types.go`)

Add `Aliases`:

```go
type TestStep struct {
    Action       string         `json:"action"`
    ConnectionID string         `json:"connection_id,omitempty"`
    Role         string         `json:"role,omitempty"`
    Message      string         `json:"message,omitempty"`
    Type         string         `json:"type,omitempty"`
    Aliases      []string       `json:"aliases,omitempty"` // ws_receive: additional matching types
    Asserts      map[string]any `json:"asserts,omitempty"`
    Timeout      int            `json:"timeout,omitempty"`
}
```

### `stepToAction` (`internal/head/agent/execute_phases_steps.go`)

Pass `Aliases` through on the receive step:

```go
case "ws_receive":
    return types.WSReceiveAction{ConnectionID: s.ConnectionID, Type: s.Type,
        Aliases: s.Aliases, Assert: s.Asserts, Timeout: s.Timeout, Decisive: true}, nil
```

### `ws_relay` expander (`internal/head/scout/ws_relay.go`)

The relay intent's receive step gains optional aliases, passed through to the
assembled `ws_receive`:

```go
type relayStep struct {
    Do      string         `json:"do"`
    Role    string         `json:"role"`
    Type    string         `json:"type"`
    Aliases []string       `json:"aliases"`           // NEW (receive only)
    Assert  map[string]any `json:"assert"`
}
```

In assembly, the receive step carries `Aliases: st.Aliases`. Validation unchanged
(`type` still required; aliases optional).

## Behavior changes

- A `ws_receive` with `aliases` matches the first frame whose type is the primary
  OR an alias. The matched frame (whichever arrived) is the evidence + assert
  target.
- No aliases ⇒ byte-identical to today (`matchAnyType` over a 1-element set ==
  `matchType`).
- All existing WS + Steps + relay tests stay green.

## Constraints

- Go 1.25, pure-Go (no CGo); `coder/websocket v1.8.14` ONLY; no new deps; no
  expression evaluator.
- No protocol-schema change (aliases are action/step-level, not declared).
- Commit author `binoctal <binoctal@gmail.com>`; no Co-Authored-By; English; docs
  only in `cerberus-docs/`; `make check` green.
- Determinism: `matchAnyType` iterates the match-set but returns on first match
  (no order-dependent output); error/reporting paths unaffected.

## Testing

- `matchAnyType`: matches primary; matches an alias; matches when primary absent
  but alias present; no match when neither; empty set never matches; binary alias
  invalid-base64 never matches (OR safe).
- `doReceive` (existing test harness): a receive with `aliases` matches a frame of
  an alias type; asserts apply to the matched (alias) frame; backward-compat (no
  aliases) unchanged.
- Steps: a `TestStep` with `aliases` flows through `stepToAction` to
  `WSReceiveAction.Aliases` and matches.
- ws_relay expander: a receive step with `aliases` is assembled onto the
  `ws_receive` step.
- Existing WS/Steps/relay tests green (backwards-compat).

## Non-goals

- Protocol-declared auto-aliases (a protocol block mapping a logical type to its
  wire variants, applied automatically by Scout/executor). Deferred — the
  per-receive `aliases` covers the need manually.
- A match-set on the role handshake `await_type` (handshake stays single-type).
- F3 dynamic URL (separate cycle).

## Open questions (resolve in the plan)

1. Whether `Validate` should reject a duplicate alias equal to `Type` (harmless
   but redundant). Lean: no (the OR is idempotent; keep validation minimal).
2. Whether the doc/prompt should show the `session:output`/`session:output-batch`
   example explicitly. Lean: yes (one line in `websocket.md` + the relay prompt
   bullet), since it is the motivating case.
