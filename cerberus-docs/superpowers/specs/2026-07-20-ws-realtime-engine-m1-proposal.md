# WebSocket Realtime Engine (M1) — Protocol Adaptation Layer (Proposal)

**Date:** 2026-07-20
**Status:** Proposal (directional; awaits M0 dogfooding signals before detailed spec)
**Depends on:** M0 — `2026-07-20-ws-realtime-engine-m0-design.md`

## Motivation

M0 is zero-config generic by design: the LLM re-derives protocol knowledge on
every run — how auth is attached (query / header / subprotocol), where the
message-routing field lives (M0 hard-assumes top-level `type`), and the wire
framing. This maximizes generality but costs:

- **Tokens:** the same protocol facts are inferred run after run.
- **Determinism:** the LLM can guess differently across runs (and can guess
  wrong, e.g. auth in the wrong place).
- **Reproducibility:** two runs of the same case may orchestrate differently.

M1 persists the protocol knowledge that is *stable and knowable ahead of time*, so
the executor reads a declaration instead of the LLM re-inferring it. The LLM is
still the orchestrator; the declaration just removes repetitive guesswork.

## Scope

**In:**
- Declared **auth strategy** (where credentials go) + credential reference.
- Declared **type-field path** (lifts M0's top-level-`type` assumption to a
  configurable path — unblocks protocols routed by `event` / `action` / nested
  fields).
- Declared **framing** (json / text / binary — M0 assumes JSON).

**Out (later milestones):**
- Role abstraction, handshake sequences, timing assertions (M2).
- Standalone protocol files, Scout-generated cases, auto-inference (M3).
- A general expression/JSONPath engine (M1 uses lightweight dotted paths only;
  cerberus has no evaluator today — see M0 Constraint 3).

## Key Design Choices

### D1 — Where the description lives
- (a) Inline in `project.yaml` under the service. **(leaning)**
- (b) Standalone `.cerberus/protocols/<name>.yaml`, referenced by `project.yaml`.
- (c) Both.

Lean toward **(a)** for M1 (YAGNI — cross-project protocol reuse is unproven).
Extract to standalone files in M3 only if reuse actually materializes. Note:
**(a)** requires extending the `project.yaml` service schema with a protocol
block — a prerequisite for M1.

### D2 — What the description covers
M1 declares four protocol facts: `auth`, `type_path`, `framing`, and
`handshake` (the static connect-time exchange). The first three are the
highest-frequency, highest-token knowledge. `handshake` is declared here because
it is static protocol description, and it is **executed via M0 primitives**
(connect→send→receive) — either by the executor automatically on connect or by
the LLM orchestrating M0 primitives from the declaration. No M2 dependency.

### D3 — Declaration vs LLM inference (fallback policy)
- (a) Declaration first; when absent, fall back to LLM inference (M0 behavior).
  **(leaning)**
- (b) Declaration required; no WS cases without it.

Lean **(a)** to preserve M0's zero-config generality. A new/unknown protocol
still runs with no declaration; a known protocol runs cheaper and more
deterministically with one. This is the same "graceful enhancement, not
replacement" posture as M0's `decisive` default.

### D4 — How the executor consumes it
- `WSConnect` reads `auth` and auto-attaches credentials (LLM no longer picks
  query-vs-header); the action can stay role/credential-driven.
- `WSReceive` reads `type_path` and matches by the configured path, not just
  top-level `type`.
- `framing` tells receive how to decode before path extraction.

## Relationship to M0 / Trigger Conditions

M0's parameterized auth and its top-level-`type` hard assumption both become
declaration-driven in M1. M0 is not rewritten — M1 layers configuration on top.

**Start M1 when M0 dogfooding shows any of:**
- The LLM re-derives the same auth/framing/type facts across runs (visible token
  waste in traces).
- A target whose routing field is **not** top-level `type` (M0 cannot test it;
  `type_path` is the fix).
- Run-to-run orchestration drift on the same case (determinism complaint).

## Open Questions (for the detailed spec)

1. `credential_ref` — how it references cerberus's existing actor/credentials
   system (avoid inventing a parallel secret store).
2. `type_path` syntax — dotted path (`data.event`) with a tiny resolver, vs
   importing a JSONPath library (prefer dotted; no new deps).
3. Validation of the description block (mirror `validate_invariants.go` style).
4. Whether `framing: binary` is worth M1 or deferred (most targets are JSON).
