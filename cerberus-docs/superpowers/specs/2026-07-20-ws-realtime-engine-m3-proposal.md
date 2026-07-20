# WebSocket Realtime Engine (M3) — Declarative Protocol Descriptions & Auto-Adaptation (Proposal)

**Date:** 2026-07-20
**Status:** Proposal (directional; awaits M0/M1 dogfooding signals before detailed spec)
**Depends on:** M0 (`...-m0-design.md`), M1 (`...-m1-proposal.md`), M2 (`...-m2-proposal.md`)

## Motivation

M1 lets a human declare protocol knowledge; M2 adds roles/assertions. But the
description is still hand-written and the cases are still LLM-orchestrated at
runtime. Two remaining frictions:

1. **Descriptions are written per project, not reusable or versioned as
   artifacts.** Two systems speaking the same protocol (e.g. multiple
   open-agents-compatible deployments) re-declare the same facts.
2. **Every run still depends on the Agent's LLM orchestration quality.** Users
   who want reproducible WS tests — not "the LLM composes differently each time"
   — have no deterministic path.

M3 promotes the protocol description to a **first-class, shareable artifact**
and lets **Scout generate WS cases from it**, with an optional auto-inference
path from documentation.

## Scope

**In:**
- Standalone protocol description files (`.cerberus/protocols/<name>.yaml`),
  referenced from `project.yaml`; superset of M1's inline description (adds
  M2's roles/handshake/assertion hooks).
- Scout generates WS test cases from a description + goal.
- `cerberus protocol infer` — LLM drafts a description from protocol docs /
  message examples (human reviews before use).

**Out:**
- Capture-based inference (record a WS session, infer the description) — future.
- A public protocol registry / marketplace — future (files can be shared
  manually until then).
- Removing the LLM entirely — payloads and exception handling still need it.

## Key Design Choices

### D1 — Description format
- (a) YAML files, superset of M1's inline schema. **(leaning)**
- (b) A dedicated DSL.

Lean **(a)** — consistency with M1 and `project.yaml`; no new parser. M3 is
chiefly the extraction of M1's inline block into a versioned, referenceable
file, plus the fields M2 added. Once standalone files exist, M1's inline form
is a transitional stepping stone and the standalone file is the target.

### D2 — Scout-generated cases
Scout does not generate WS cases today (M0 is LLM-driven). M3 teaches it:
- (a) Scout emits complete cases (connection graph + message sequence +
  assertions) from the description. Too rigid.
- (b) Scout emits a **case skeleton** (roles involved, key interaction points,
  expected message types) and the Agent/LLM fills payload details and handles
  dynamics. **(leaning)**

Lean **(b)** — combines determinism of structure with LLM flexibility for
payloads. This is the "reduces but does not remove LLM orchestration" posture
consistent with M0–M2.

### D3 — Auto-inference (`protocol infer`)
- (a) From protocol documentation (Markdown / OpenAPI-like / message examples)
  via LLM. **(leaning for M3)**
- (b) From recorded captures (pcap/ws replay). Future.

Produce a **draft** description the human reviews and commits — never an
implicitly-trusted inferred protocol. This keeps a human gate on correctness
while removing the blank-page cost.

### D4 — Determinism posture
M3 is the milestone where a user can get a **reproducible** WS test: description
+ Scout skeleton + M2 assertions means the case structure is stable across runs,
and the LLM only varies payload-level detail. Users not caring about that stay
on pure M0 LLM-orchestration. Both paths coexist.

## Relationship to M0/M1/M2 / Trigger Conditions

- M1's inline description → M3 standalone artifact.
- M2's roles/handshake/assertions → fields in the M3 description file.
- Scout's case generation (currently Go/Node/Python only) → gains a WS path
  that consumes descriptions.

**Start M3 when M0–M2 dogfooding shows any of:**
- The same protocol declared across multiple projects (reuse signal → extract
  to artifact).
- Users asking for reproducible WS tests (orchestration-drift complaints).
- Hand-writing descriptions is a repeated blank-page cost (auto-inference
  demand).

## Open Questions (for the detailed spec)

1. Description file versioning and sharing model (manual git sharing vs a
   registry — start manual).
2. What "complete enough" means for a Scout-generated WS skeleton (coverage of
   roles + key message types; mirroring the existing Scout depth tiers).
3. Auto-inference quality gate (mandatory human review; how to surface
   low-confidence inferred fields).
4. Whether `protocol infer` belongs in Scout or as a standalone `cerberus`
   subcommand (leaning standalone — it produces an artifact, not a test run).
