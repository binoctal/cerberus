# WS Batch Frame Decomposition (design / spec)

> Status: design spec, 2026-07-30. Roadmap #5 — the last and most cross-cutting
> item. This is the SDD spec; implementation is a focused follow-up. The
> "aliasing half" already ships (`matchAnyType`); this covers the remaining
> "batch → N asserts decomposition" half.

## Problem

A realtime server often sends a SINGLE batch frame that carries N logical
messages, e.g.:

```json
{"type":"session:output-batch","payload":{"lines":["hello","world"]}}
```

represents two logical `session:output` messages. Today cerberus can MATCH such
a frame as a whole (the aliasing half: `ws_receive` of `session:output` with
alias `session:output-batch` matches the batch frame and asserts its payload
`payload.lines == [...]`). What it CANNOT do is **decompose** that one batch
frame into N per-item messages so a receive can assert EACH item individually
(e.g. "each line contains 'hello'") or so N separate receives each consume one
item. The batch is an opaque single match.

The gap spans three subsystems (hence "sprawling"): **schema** (how a batch is
declared), the **read pipeline** (where one frame becomes N), and **assert
semantics** (per-item vs whole-batch).

## Current state (confirmed)

- `matchAnyType` (`ws_protocol.go:88`) — a `ws_receive` matches a frame whose
  routing key is `Type` OR any `Aliases`. A batch type is just an alias; the
  whole batch frame is one match.
- `doReceive` (`websocket.go:705`) — `readMatching` returns the first matching
  frame; `checkAsserts` evaluates field asserts against that one frame's JSON.
  Asserts are whole-frame, not per-item.
- No batch schema field exists on `Protocol` / `ProtocolRole`
  (`protocol_schema.go`).

## Design rulings

### R1. Decomposition point: pump expansion (not receive-time)

Expand ONE batch frame into N synthetic frames inside the **read pump**
(converting 1 `wsMsg` → N), not inside the matcher. Rationale:
- Every existing consumer (handshake auto-await, `ws_receive`, decisive verdict,
  evidence) then "just works" per-item with no per-callsite change.
- The alternative (receive-time expansion) duplicates decomposition logic across
  the matcher AND redefines assert semantics per-item — more surface, more forks.

Trade-off accepted: the pump must know the batch schema, and expansion happens
for all consumers on that connection (a connection either sees items or the raw
batch, not both). That is the desired semantic for a batch channel.

### R2. Schema declaration: protocol-level batch map

Declare batches on the **Protocol** (a batch is a property of a wire type, not a
role):

```yaml
protocol:
  type_path: type
  batches:
    session:output-batch:          # the batch routing type (the alias target)
      item_type: session:output    # routing key applied to each expanded item
      items_path: payload.lines    # dotted JSON path to the array
```

`Protocol.Batches map[string]*ProtocolBatch` where `ProtocolBatch{ItemType,
ItemsPath}`. Each expanded item becomes a synthetic json frame
`{"type":<item_type>, "payload": <item>}` (or carries the item at a declared
sub-path in a later phase). Keep the item envelope shape FIXED in phase 1
(item becomes the whole `payload` of the synthetic frame) to avoid a third
schema knob now.

### R3. Scope: json framing only (phase 1)

Decomposition is defined for `json` framing (the only framing with structure to
iterate). Text/binary batches are out of scope (a text/binary "batch" has no
machine-readable item boundary without an app-level codec). Validate rejects a
batch declaration under non-json framing.

### R4. Assert semantics: per-item, unchanged engine

Because expansion happens in the pump, a `ws_receive` of `item_type` matches
each expanded item frame in arrival order; `checkAsserts` runs against each
item's JSON unchanged. The FIRST failing item fails the receive (existing
first-failure semantics). A decisive receive matches the first item — phase 2
can add a "match all items" mode if needed, but phase 1 keeps one-match = pass
(the common case: assert the first/all-equal item).

### R5. Interaction with aliasing

Aliasing and batching compose: a receive may list `session:output` (item type)
with alias `session:output-batch`. Post-expansion the raw batch frame is gone
(replaced by items), so the alias never matches the batch type — only items
match. The alias becomes redundant once a batch is declared for that type; this
is benign (document it).

## Phased plan

- **Phase 1 (this feature):** `Protocol.Batches` schema + validation (json only,
  item_type/items_path required, item_type must not collide with auth token slot);
  pump expansion under json framing (1 batch frame → N synthetic json item
  frames); tests (expansion correctness, ordering, decisive receive on item,
  per-item assert pass/fail, no-batch backwards-compat).
- **Phase 2 (later):** configurable item envelope (item at a sub-path, not just
  whole-payload); "match all items" decisive mode; consider text-framed
  newline-delimited batches.

## Open questions (phase-1 decisions needed at impl time)

- **Ordering:** expand in array order (natural) — confirm the pump preserves it
  under the readMu/buffer model.
- **Empty batch:** an empty `items_path` array → zero expansions (the batch frame
  vanishes). Acceptable; document (a receive awaiting the item type then times
  out, which is correct — nothing was sent).
- **Items_path missing/non-array on a live frame:** log + pass the frame through
  unexpanded (degrade, don't fail the pump).

## Why a spec, not impl, now

#5 is the roadmap's most cross-cutting item (schema + read pipeline + assert).
This spec fixes the design (pump expansion, protocol-level declaration, json-only,
per-item via existing engine) so implementation is a focused, de-risked follow-up
rather than a sprawling inline exploration. The aliasing half already shipped;
this spec closes the conceptual design of the batching half.
