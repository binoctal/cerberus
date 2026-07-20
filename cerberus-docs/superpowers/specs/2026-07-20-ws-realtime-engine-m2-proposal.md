# WebSocket Realtime Engine (M2) — Multi-Role, Timing & Field Assertions (Proposal)

**Date:** 2026-07-20
**Status:** Proposal (directional; awaits M0/M1 dogfooding signals before detailed spec)
**Depends on:** M0 (`...-m0-design.md`), M1 (`...-m1-proposal.md`)

## Motivation

M0 can orchestrate multi-step, multi-connection conversations inside one case,
but with three gaps that hurt on complex realtime topologies (multi-device,
A2A, fan-out):

1. **No role semantics.** Connections are bare `connection_id`s; "bridge" vs
   "web" vs "device-3" lives only in the LLM's head. Multi-instance roles (two
   bridge devices) are awkward to express and read back.
2. **No field assertions.** M0 deliberately offloads content checks
   (`payload.approved == true`) to the Examiner (M0 Constraint 3 — no
   evaluator). That verdict is **late** (session end) and **non-deterministic**
   (LLM judgment). For safety-critical fields this is too soft.
3. **No timing assertions.** Message ordering / windows cannot be asserted;
   reordering bugs pass silently as long as the right `type` eventually arrives.

M2 closes these with role abstraction, lightweight field assertions, and
minimal timing assertions.

## Scope

**In:**
- **Role** as a first-class attribute of a connection (resolve by role, not just
  id).
- **Field assertions** on `WSReceive` (deterministic, machine-checked, optional).
- **Minimal timing assertions** (message ordering).
- **Handshake sequences** declared and executed by the executor (deferred from
  M1's description).

**Out:**
- General expression engine (M2 uses the same lightweight dotted-path + equality
  approach as M1's `type_path`; no new evaluator).
- Full temporal-logic / sliding-window assertions (future).
- Auto-generation of role topologies (M3).

## Key Design Choices

### D1 — Role model
- (a) Role is a label on a connection (`WSConnect {role:"bridge"}`); executor
  keeps `role → connection_id` so `WSReceive {role:"web"}` resolves. **(leaning)**
- (b) Roles declared up front as a case-level set; connections bind to them.

Lean **(a)** — minimal, composes with M0's `connection_id`. Same-protocol
multi-instance (two bridges) distinguishes by role + id. `connection_id`
remains the canonical handle; role is a convenience alias.

### D2 — Field assertions (the M0 trade-off revisited)
M0 chose Examiner-side content judgment to avoid an evaluator. M2 introduces a
**narrow, optional** machine assertion:
- `WSReceive` gains an optional `assert` field: dotted path + expected value
  (`{path:"payload.approved", equals:true}`).
- Implemented by JSON unmarshal + path lookup + equality — **not** a general
  expression engine.
- Stays optional: absent `assert`, M0 behavior (Examiner judges). Present
  `assert`, the receive fails fast on mismatch (deterministic, in-case).

This gives safety-critical fields a deterministic path without committing cerberus
to an expression engine. Note this changes `WSReceive` success semantics versus
M0: with `assert` present, a field mismatch fails the receive; without `assert`,
M0 behavior (arrival-only, content left to the Examiner) is unchanged.

### D3 — Timing assertions (minimal)
- The executor records per-case message arrival order. Note: M0 accumulates
  `SeenMessages` per-receive only; cross-receive ordering requires a new per-case
  global arrival log maintained by the executor.
- A small assertion expresses ordering: e.g. a `WSReceive` with
  `after: <prior-receive-ref>` requires its match to arrive after a named prior
  match; violation fails the action.
- Sliding windows / deadlines are explicitly out of scope (future).

Lean toward the smallest ordering primitive that catches real reorder bugs; do
not build a temporal DSL.

### D4 — Handshake (owned by M1, not M2)
Handshake sequences are **declared and executed in M1** (see M1 D2) using M0
primitives. M2 does not touch handshake; this section exists only to prevent
re-introducing the earlier M1/M2 contradiction.

## Relationship to M0/M1 / Trigger Conditions

- M0's `connection_id` gains a `role` alias in M2.
- M0's Examiner-side content judgment gets an optional deterministic companion
  (`assert`).
- M1's protocol description gains `handshake`, executed by M2.

**Start M2 when M0/M1 dogfooding shows any of:**
- Multi-role topologies (bridge + web + devices) become common and
  `connection_id` bookkeeping gets painful.
- A safety-critical field (e.g. `approved`, `risk`) is mis-judged by the Examiner
  late or inconsistently — deterministic assertion needed.
- A real reorder bug slips through M0 (right types arrive, wrong order).

## Open Questions (for the detailed spec)

1. Role ↔ M1 `credential_ref` coupling (role likely selects the credential).
2. `assert` operator set (start with `equals` only; add `contains`/`exists` on
   demand — avoid creeping into an expression engine).
3. How timing assertions reference prior receives (by a receive label/ref,
   since `connection_id` + `type` may repeat).
4. Whether role resolution needs scoping (role unique per case vs per session).
