# WS Per-Connection Read Pump — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`).

**Goal:** Replace "read on demand with a timeout context" (which closes the conn on timeout under `coder/websocket`) with a per-connection read pump (single background reader → buffered channel) consumed with a timeout. Fixes the latent `doReceive`-timeout-closes-conn bug; foundation for F2/F4/F1.

**Spec:** `cerberus-docs/superpowers/specs/2026-07-23-ws-read-pump-design.md` (read it — it has the full design, the consumer loop, and the concurrency invariants).

**Tech Stack:** Go 1.25 · `coder/websocket` v1.8.14 · testify. No new deps.

## Global Constraints

- Go 1.25, pure Go, no new deps, `coder/websocket v1.8.14` only.
- **Concurrency invariants (must hold under `-race`):** single reader (only the pump calls `conn.Read`); single consumer per conn (`readMu` serializes channel consumption); NO `readMu`/`e.mu` nesting; ctx-cancel closes the conn (pump exits via `entry.ctx.Done()`).
- Backwards-compat: existing WS tests (handshake, framing, field-asserts, role carriers, connection echo, M0 fallback) stay green. **One behavior change:** `doReceive` timeout no longer closes the conn (bug fix — receive-then-continue now works).
- Author `binoctal <binoctal@gmail.com>`, no Co-Authored-By, English, cerberus-docs only, make check green.
- Run the WS suite under `-race`; the pump is goroutine-heavy — race-detector + repeated runs are the gate.

## File Structure

- Modify `internal/head/agent/websocket.go` — `wsEntry` (pump state), `store` (start pump), a new `readMatching` consumer helper, rewire `doConnect` handshake + `doReceive` to consume from the channel.
- Modify `internal/head/agent/websocket_test.go` — receive-then-continue + pump-exit + concurrency-stress tests; update any test asserting conn-dead-after-receive-timeout.
- Modify `cerberus-docs/executors/websocket.md` — note the read pump in Per-case namespacing.

---

### Task 1 (ATOMIC — pump + both consumers together; sonnet, opus task review)

The pump and both consumers MUST land together — the pump owns `conn.Read`, so any leftover `conn.Read` in doReceive/handshake would concurrent-read and corrupt the frame stream.

**Files:** `internal/head/agent/websocket.go`, `internal/head/agent/websocket_test.go`.

**Interfaces (match the spec exactly):**
- `wsEntry` gains `msgs chan wsMsg` (cap 256), `pumpErr error`, `done chan struct{}`; `wsMsg{data []byte; binary bool}`.
- `store` starts the pump goroutine (reads with `entry.ctx`, no timeout; pushes to `msgs`; on error sets `pumpErr`, `close(done)`, returns). The existing ctx-cancel cleanup goroutine (close conn on ctx.Done) STAYS.
- `readMatching(entry, match func(wsMsg) bool, timeout) (matched wsMsg, seen []string, status)` — the shared consumer: under `entry.readMu`, drain buffered + wait (select on `msgs` / `time.Until(deadline)` / `done`); returns matched-or-timeout-or-pump-gone. Timeout ⇒ conn ALIVE (do not close).
- `doReceive` uses `readMatching` (match by type via `matchType`; then asserts on the matched frame). Timeout ⇒ `OK:false`, conn alive.
- `doConnect` handshake uses `readMatching` (match `role.Handshake.AwaitType`); on timeout, mandatory ⇒ close+delete+`OK:false` (unchanged); matched ⇒ `OK:true`.

- [ ] Step 1: failing tests (RED):
  1. **receive-then-continue** (headline): a receive that times out (no matching msg) leaves the conn ALIVE — a subsequent `ws_send` + `ws_receive` on the SAME connection_id succeeds.
  2. **pump-exit**: peer closes the conn mid-case → a subsequent `ws_receive` returns an error (the pump error), not a hang.
  3. existing handshake/framing/assert tests still pass (run them — they're the backwards-compat guard).
- [ ] Step 2: verify RED (readMatching/pump undefined; or receive-then-continue fails under the old conn.Read-close model).
- [ ] Step 3: implement (pump state + goroutine + readMatching + rewire both consumers), per the spec's consumer loop + invariants.
- [ ] Step 4: verify GREEN — `go test ./internal/head/agent/ -race -count=3` (repeated: goroutine-heavy).
- [ ] Step 5: commit — `feat(ws): per-connection read pump (conn survives read timeout)`.

**Self-review:** single reader (grep — only the pump calls `conn.Read`); no `readMu`/`e.mu` nesting; ctx-cancel still closes; receive-timeout conn-alive proven; existing WS tests green.

---

### Task 2 (concurrency stress; sonnet)

- [ ] Tests under `-race`, high iteration:
  1. **concurrent cases on different connections** run in parallel (no concurrent-Read panic; each conn's consumer serialized by its own `readMu`).
  2. **two consumers on one conn** (if reachable via the API) serialize (second waits on `readMu`) — or document why it can't happen.
  3. pump exit on ctx-cancel (case exit) closes conn + pump exits cleanly (no goroutine leak — check via `-race` + a runtime goroutine count or just rely on -count=N stability).
- [ ] Commit — `test(ws): read-pump concurrency stress (parallel cases, pump lifecycle)`.

---

### Task 3 (docs; inline + opus-final)

- [ ] `cerberus-docs/executors/websocket.md` Per-case namespacing subsection: note each connection now has a background read pump (single reader; reads are buffered), so a `ws_receive` timeout no longer closes the connection (receive-then-continue works). Cross-link the spec. Commit — `docs(ws): note per-connection read pump`.

---

## Self-Review (after writing — done inline)

- Spec coverage: pump+wsEntry (T1), readMatching+both consumers (T1), lifecycle/invariants (T1+T2), behavior change documented (T1+T3), tests (T1+T2), docs (T3). Open Q1 (cap 256) ⇒ T1; Q2 (pump doesn't close on error, closure via ctx-cancel/doDisconnect) ⇒ T1; Q3 (keep readMu for now) ⇒ T1. Covered.
- Atomicity: Task 1 lands pump + both consumers together (no broken intermediate). Task 2/3 are additive.
