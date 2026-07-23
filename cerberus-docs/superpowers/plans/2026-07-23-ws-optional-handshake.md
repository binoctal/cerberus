# F2 — Optional (Best-Effort) WS Handshake — Plan (full version, post-read-pump)

**Goal:** A declared role handshake can be **optional/best-effort**: the connect SUCCEEDS even when the awaited message never arrives (peer-gated handshakes like open-agents' `devices:sync`, which only arrives when a bridge is online). Now that the read-pump keeps the conn alive across a read timeout, this is the FULL design (await best-effort, capture if present, succeed on timeout), not the lite skip-await workaround.

**Foundation:** the read-pump refactor (merged `0ad3518`) — `readMatching` already returns "timeout" with the conn ALIVE (pump running). F2 just makes `doConnect` tolerate that timeout when the handshake is optional.

## Design

- `project.RoleHandshake` += `Optional bool` (`yaml:"optional,omitempty"`, default false = mandatory = today's behavior).
- `doConnect` handshake (now uses `readMatching`): on a timeout (`status=="timeout"`), if `role.Handshake.Optional` ⇒ return `WSResult{OK:true, ConnectionID:id, SeenMessages:seen, …}` (conn alive — pump still running; usable for ws_send/ws_receive). Else (mandatory) ⇒ the existing close+delete+`OK:false`.
- Validation: an optional handshake still requires non-empty `await_type` (already checked) and `timeout > 0`.

## Task 1 (sonnet; opus review — touches doConnect handshake): schema + doConnect + validation + tests

- `RoleHandshake.Optional` (`internal/project/protocol_schema.go`).
- `doConnect` optional-timeout ⇒ `OK:true`, conn kept (readMu/pump invariants preserved — only the timeout branch's outcome changes).
- Validation (`validate_protocol.go`).
- TDD tests (`internal/head/agent/websocket_test.go` + a project validation test):
  1. optional, no message within timeout ⇒ connect `OK:true` AND a follow-up `ws_send`+`ws_receive` on the same connection_id SUCCEEDS (conn alive — the headline, now real via the pump).
  2. optional, message arrives ⇒ `OK:true` + `SeenMessages` captures it (best-effort captures when present).
  3. mandatory (unset), no message ⇒ `OK:false`, conn closed/removed (REGRESSION — current behavior).
  4. validation: optional + empty `await_type` ⇒ error.

## Task 2 (docs, inline + opus-final): websocket.md + project.md note `handshake.optional`.

## Constraints

- Go 1.25, pure Go, no new deps, no expression evaluator.
- Backwards-compatible: `Optional:false`/unset ⇒ byte-identical current behavior.
- doConnect concurrency: the optional-timeout path keeps the conn under `-race` (readMu released, pump running, no double-close).
- Author `binoctal <binoctal@gmail.com>`, no Co-Authored-By, English, cerberus-docs only, make check green.
