# WS Per-Connection Read Pump — Design

Status: Design (autonomous; chosen 2026-07-23 as the foundational refactor).
Trigger: the F2 (optional handshake) attempt discovered that
`coder/websocket@v1.8.14` closes the socket on ANY read-context timeout
(`setupReadTimeout` registers `context.AfterFunc(ctx, func(){ c.close() })`,
conn.go:183-189). See memory `ws-coder-websocket-closes-on-read-timeout`.

## Goal

Decouple read-timing from connection lifecycle. A per-connection background
goroutine (the **pump**) owns `conn.Read` (single reader — exactly what
`coder/websocket` requires). handshake/receive **consume from a buffer with a
timeout**, never calling `conn.Read` with a timeout context. A read timeout no
longer closes the connection.

## Why

- **Latent bug:** `doReceive`'s `context.WithTimeout` read closes the conn on
  timeout (`websocket.go:509-517`) — currently masked because receive-then-
  continue isn't a pattern. The pump fixes this: a receive timeout leaves the
  conn alive for a later send/receive.
- **F2 (optional handshake):** "await best-effort, tolerate timeout, keep conn"
  becomes possible (timeout ≠ closed).
- **F4 (receive-then-continue) + F1 (multi-connection):** both need a live conn
  across multiple operations; the pump is the foundation.
- **Correctness:** centralizing reads in one goroutine removes any possibility
  of concurrent `conn.Read` (the `readMu` currently defends against it; the pump
  makes it structurally impossible).

## Design

### `wsEntry` changes

```go
type wsEntry struct {
    conn     *websocket.Conn
    ctx      context.Context
    protocol *project.Protocol
    // read pump state
    msgs     chan wsMsg      // buffered; pump pushes every inbound frame
    pumpErr  error           // set when the pump exits (read error / ctx done)
    done     chan struct{}   // closed when the pump has exited
    readMu   sync.Mutex      // serializes channel consumption (one consumer at a time)
}
type wsMsg struct {
    data []byte
    binary bool
}
```

### The pump (`store()` starts it)

On `store`, start:

```go
go func() {
    defer close(entry.done)
    for {
        mt, data, err := conn.Read(entry.ctx)   // NO timeout: blocks until frame/error/ctx-done
        if err != nil {
            entry.pumpErr = err                 // peer close, dial aftermath, or ctx cancel
            return
        }
        select {
        case entry.msgs <- wsMsg{data: data, binary: mt == websocket.MessageBinary}:
        case <-entry.ctx.Done():
            return
        }
    }
}()
```

- Reads with `entry.ctx` only (no `WithTimeout`) → a frame arrival or ctx-done
  is the only thing that unblocks it. **No read-timeout-close.**
- On exit (`pumpErr` set, `done` closed), the connection is dead; the existing
  case-exit cleanup (ctx-cancel → close conn) still applies. The pump does not
  itself close the conn on a bare read error unless the error indicates the conn
  is already broken — `conn.Read` returning an error means the conn is unusable,
  so the consumer reports it; lifecycle closure stays with the ctx-cancel handler
  + `doDisconnect`.

### Consumer (handshake in `doConnect`, and `doReceive`)

Under `readMu` (one consumer per conn at a time), loop:

```go
// drain buffered, then wait for new with a timeout
deadline := time.Now().Add(timeout)
for {
    // non-blocking drain of whatever has arrived
    for {
        select {
        case m, ok := <-entry.msgs:
            if !ok { /* pump gone */ return closed }
            if match(m) { return matched }
            seen = append(seen, frame(m))
        default:
            goto wait
        }
    }
wait:
    select {
    case m, ok := <-entry.msgs:
        if !ok { return closed(entry.pumpErr) }
        if match(m) { return matched }
        seen = append(seen, frame(m))
    case <-time.After(time.Until(deadline)):
        return TIMEOUT (conn still alive — do NOT close)
    case <-entry.done:
        return closed(entry.pumpErr)
    }
}
```

- A timeout returns `OK:false` **with the conn still alive** (pump still running).
  This is the bug fix + the F2 enabler.
- Pump-gone (`done` closed) returns `OK:false` with `pumpErr` (peer closed).

### `doConnect` mandatory handshake

On timeout: still close+delete+`OK:false` (current behavior) — the pump is
stopped by closing the conn. On `Optional` (F2, downstream): return `OK:true`
(conn alive, pump running). (F2 itself lands as a follow-up consuming this pump;
this refactor does not add the `Optional` flag — it only makes it possible.)

### `doSend`

Unchanged — writes are not affected by the read-timeout-close issue.

### Concurrency invariants (must hold under `-race`)

- **Single reader:** only the pump calls `conn.Read`. Consumers never do.
- **Single consumer per conn:** `readMu` serializes channel consumption (a second
  consumer blocks on `readMu` until the first returns). Preserves today's
  "serialize receives per conn" semantics.
- **No `readMu`/`e.mu` nesting:** `readMu` is acquired only around the consumer
  loop; `e.mu` (the table lock) is never held while taking `readMu` (mirror the
  current invariant).
- **Lifecycle:** ctx-cancel (case exit) closes the conn; the pump observes it via
  `entry.ctx.Done()` and exits. `doDisconnect` closes+deletes as today.
- **Channel back-pressure:** `msgs` is buffered (cap generous, e.g. 256); on full,
  the pump blocks on send (back-pressure) rather than dropping — test scenarios
  never approach the cap. Documented; not tunable for now.

## Behavior changes

- **`doReceive` timeout no longer closes the conn** (bug fix). Any test asserting
  the conn is dead after a receive timeout must be updated; receive-then-continue
  now works.
- Mandatory handshake timeout still fails+closes (unchanged).
- All existing WS tests (handshake, framing, field-asserts, role carriers,
  connection echo, M0 fallback) must stay green.

## Testing

- Existing WS tests green (backwards-compat).
- **New:** receive-then-continue — `ws_receive` times out (no match), conn stays
  alive, a later `ws_send` + `ws_receive` succeeds on the SAME connection (the
  headline proof).
- **New:** pump exit — peer closes the conn mid-case → subsequent receive returns
  the pump error (not a hang).
- **New:** concurrent cases on different conns still parallel; one consumer per
  conn serialized (no concurrent-Read panic under `-race`, high-iteration).
- Run the full WS suite under `-race` repeatedly (the pump is goroutine-heavy).

## Non-goals

- Does NOT add `RoleHandshake.Optional` (that's F2, a downstream consumer of this
  pump — lands after).
- Does NOT add multi-connection orchestration (F1) or receive type-aliasing (F4).
- Does NOT change `doSend`, framing, auth, or the connection table keying.

## Open questions (resolve in the plan)

1. Channel cap (256?) — generous default, documented.
2. Whether the pump closes the conn on a read error itself, or leaves it to the
   ctx-cancel handler / doDisconnect (lean: the pump does NOT close on error —
   `conn.Read` erroring means the conn is already broken; closure stays with the
   existing ctx-cancel cleanup + doDisconnect, avoiding double-close).
3. Whether `readMu` can be relaxed (single reader makes concurrent conn.Read
   impossible) — keep it for now (serializes consumers); revisit if F1 needs
   concurrent consumers.
