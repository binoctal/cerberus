# Protocol Vocabulary Discovery — Design Spec

- Date: 2026-08-03
- Status: Draft (pending review)
- Author: binoctal
- Dogfood target: `open-agents/apps/api/src/realtime/room.ts`

## 1. Problem

cerberus's knowledge of a system-under-test's WebSocket protocol is split across
two layers of very different maturity:

1. **Connection contract (mature).** `*.cerberus/protocols/<name>.yaml` declares
   `framing` / `type_path` / `auth` / `roles` / `handshake`. The executor
   consumes this deterministically. `cerberus protocol infer`
   (`internal/protocoldiscover`) can auto-derive it via LLM.
2. **Message vocabulary + routing semantics (human-transcribed).** The lists of
   `type` values, their direction (who sends, who receives), route fields, and
   side effects are **hand-copied from the SUT source into test code**. Evidence:
   `internal/head/agent/execute_phases_steps_integration_test.go` hardcodes ~37
   bridge→web and ~24 web→bridge `type` values with comments pinning source line
   numbers (`room.ts:178-220`, `room.ts:295`, …).

Consequence: cerberus does not "understand" what the SUT does at the message
level — a human does, then transcribes that understanding into tests. The tests
rot as the SUT evolves, and the human's knowledge is nowhere machine-readable.

The `switch(msg.type)` block in `room.ts` is a **deterministically parseable
routing table**: every `case` is a string literal, each block carries a
`meta.type === 'web'|'bridge'` guard, and each emits via a small set of methods
(`broadcastToWeb` / `sendToBridge` / `notifyOrchestrator` / `batchOutput`). This
spec captures that table automatically.

## 2. Goal

A **protocol vocabulary discovery** pipeline whose output (`<name>.vocab.yaml`)
is the single source of truth driving three consumers:

1. **Dynamic regression tests** — a runtime test reads `vocab.yaml` and builds
   the `TestCase` table currently hand-maintained, so tests stop rotting.
2. **Scout runtime context** — `project` loader exposes vocab so Scout can reason
   about message flow (interface reserved; first version produces the file,
   Scout consumption is a follow-up).
3. **Human-readable spec** — `vocab.yaml` is itself the routing spec.

### Non-goals (first version)

- **Payload schema.** Field-level types per message (requires cross-file type
  resolution) — out of scope. Vocab captures routing, not payload shape.
- **Semantic annotations** (one-line human descriptions per type) — out of scope
  (YAGNI); the file is readable from `type`/`direction`/`source` alone.
- **Full Scout consumption.** Vocab is produced and loadable; wiring it into
  Scout's prompt is a separate change.
- **Non-TypeScript SUTs.** The discovery layer is language-pluggable in
  principle, but only the TypeScript adapter (`ts-morph`) is implemented.

## 3. Design principles

1. **Emit-point anchored, not switch-anchored.** The unit of discovery is a
   *point that sends a frame out* (`broadcastToWeb` / `sendToBridge` / `ws.send`
   / `notifyOrchestrator`), reached by walking up to its triggering context.
   This naturally spans `handleMessage`, `fetch` (connect), `webSocketClose`
   (disconnect), the `/broadcast` endpoint, and batch flushes — not just the
   switch.
2. **Skeleton authoritative.** Structurally-derived facts (type literals, role
   guards, route fields, delivery mode) are the authority for test correctness.
3. **Pure-AST, zero-LLM (first version).** Cross-function transforms
   (`session:output` → `session:output-batch`) are marked `batch + partial`
   rather than precisely resolved; the dynamic test **skips** them and records a
   finding (precise transform assertion needs dataflow/LLM work = future). This
   removes LLM nondeterminism entirely from discovery and makes output
   reproducible.
4. **Type is an edge label, not a key.** The same `type` may have multiple edges
   (e.g. `session:send` is web→bridge **and** web→web; `workflow:task_assign`
   appears in both `/broadcast` and `handleMessage`).
5. **YAGNI on the adapter interface.** First version ships the `ts-morph`
   adapter only; no abstract `ASTAdapter` interface until a second language
   actually arrives.

## 4. Architecture

```
SUT source (room.ts)
   │
   ▼
┌──────────────────────────────────────────────────┐
│ Discovery  (cerberus protocol vocabulary)         │
│                                                  │
│ ts-morph adapter (node subprocess, only adapter) │
│   anchor = every outbound emit point             │
│   → walk up to case/fall-through/guard/method    │
│   → structured edge skeleton (deterministic)     │
│                                                  │
│ partial resolver: batchOutput/flushBatch pattern │
│   → mark batch + partial (no LLM, no transform)  │
└──────────────────────────────────────────────────┘
   │
   ▼
<name>.vocab.yaml   (edge set + source hash)
   │
   ├──────────────┬──────────────┐
   ▼              ▼              ▼
① dynamic test  ② Scout (load)  ③ readable spec
(run-time read, (loader; Scout  (the file itself)
hash-checked)   wiring = later)
```

### Why ts-morph via node subprocess (not tree-sitter, not regex)

- **tree-sitter**: every Go binding requires cgo. cerberus is pure-Go by
  constraint (`modernc.org/sqlite`, `No CGo dependency`). Ruled out.
- **regex**: only matches `room.ts`'s exact `switch(msg.type)` shape; a SUT using
  a dispatch table or router library defeats it. Not generic.
- **ts-morph**: real TypeScript CST with ancestor/sibling traversal; the
  emit-point rule adapts to varied structures. Runs as a `node` subprocess
  (`extractor.mjs` bundled via `embed.FS`); Go side parses JSON stdout. Discovery
  is a development-time tool — the node dependency does not enter the production
  binary.

### Generality — honest scope

The first-version adapter is tuned for the **Cloudflare DurableObject + private
broadcast-method** shape (`broadcastToWeb`/`sendToBridge` by name). The generic
foundation is the *emit-point* rule (anchor on `ws.send` outbound); method-name
recognition is that rule's specialization for `room.ts`. A SUT with a different
structure needs new recognition sub-strategies inside the adapter — the
upper-edge model is unchanged. This boundary is stated explicitly rather than
claimed as plug-and-play for arbitrary TS.

## 5. vocab schema (directed-edge model)

`type` is an edge label, not a primary key. Edge uniqueness key =
`(from_role, to_role, type, trigger)`; multiple emit points with the same key
merge their `source.spans` (no error).

```yaml
source:
  files:
    - path: apps/api/src/realtime/room.ts
      hash: a3f1c0...        # whole-file hash; conservative — unrelated edits
                             # also trigger a (non-blocking) warning
  protocol_ref: open-agents  # links open-agents.yaml (framing/auth/roles/handshake)

edges:
  - from_role: bridge          # web | bridge | null (null = DO-spontaneous)
    to_role: web               # web | bridge
    type: workflow:task_progress
    trigger: message_handled   # see trigger domain below
    guard: "meta.type === 'bridge'"   # provenance only; execution reads from_role
    delivery:
      mode: broadcast_web      # broadcast_web | send_bridge_by_device | unicast_web
      exclude_sender: false    # only valid for broadcast_web
    route_field: payload.deviceId        # only for send_bridge_by_device
    on_missing_route: { kind: send_error, code: MISSING_DEVICE_ID }
    requires_present_role: bridge        # conditional; absent role ⇒ edge absent
    side_effects:
      - kind: notify_orchestrator
        when_types: [workflow:task_result, workflow:task_error,
                     workflow:task_progress, workflow:task_question]
    batch: { window_ms: 50, key: payload.sessionId }
    unsupported: false         # true ⇒ test generator skips, logged as finding
    source: { spans: [{ start: 372, end: 399 }] }
```

`partial` is set **only** on batch edges (Section 6 Step 7); it is not shown
above because `workflow:task_progress` is not a batch edge.

### Field domains

- `from_role`: `web | bridge | null`. `null` = DO-spontaneous or external
  injection; distinguished by `trigger`.
- `to_role`: `web | bridge`. For `unicast_web`, the recipient is the triggering
  connection itself.
- `trigger`: `connect_web | connect_bridge | disconnect_bridge |
  message_handled | broadcast_endpoint`. Orthogonal to `from_role`.
- `delivery.mode`: `broadcast_web | send_bridge_by_device | unicast_web`.
  `exclude_sender` only valid for `broadcast_web`.
- `guard`: TS expression text — **provenance, not executed**; the test generator
  always uses `from_role` for execution decisions.
- `side_effects[].when_types`: the only `when` form implemented in v1; the field
  is named `when_types` to leave room for non-type conditions later.
- `requires_present_role`: the only `conditional` form implemented in v1.
- `partial`: set **only** on batch edges whose final outbound type is not
  statically resolvable. The dynamic test **skips** the edge and records a
  finding; precise transform assertion is future work.
- `unsupported`: set when a condition/shape is unrecognized; the test generator
  **skips** the edge and records a finding rather than emitting a wrong test.

## 6. Extraction algorithm (ts-morph adapter)

The adapter (`extractor.mjs`) takes a source path, emits JSON
`{ edges: [...] }`. Nine steps:

1. **Enumerate emit points.** Collect every call to `this.broadcastToWeb`,
   `this.sendToBridge`, `this.notifyOrchestrator`, and `ws.send`. Classify by
   callee name → `delivery` / `side_effect`.
2. **Resolve type + fall-through chain.** For each emit point, walk up to the
   nearest `CaseClause`. The type label comes from the case expression; **if
   preceding sibling CaseClauses have empty bodies, they are fall-through —
   enumerate the whole chain, emitting one edge per type label.** (This is the
   fix for the v1 draft's fatal gap: `room.ts:349-401` shares one
   `broadcastToWeb` across ~40 fall-through cases.) If the emit arg is an object
   literal, read `type` directly.
3. **Resolve trigger / from_role / guard.** Walk up to the enclosing
   `MethodDeclaration` and nearest `IfStatement`:
   - `handleMessage` + `if (meta.type===X)` → `message_handled`, `from_role=X`.
   - `fetch` branch by `url.pathname`/`type` param → `connect_web` /
     `connect_bridge` / `broadcast_endpoint`, `from_role=null`.
   - `webSocketClose` + `meta.type==='bridge'` → `disconnect_bridge`.
   - For `ws.send` (unicast), trace the `ws` variable to its connection role for
     `to_role`; if untraceable, mark `unsupported`.
4. **route_field / on_missing_route.** `sendToBridge(devId, msg)`'s first arg
   (e.g. `payload.deviceId`) → `route_field`. A sibling
   `else { sendError(...,'MISSING_DEVICE_ID',...) }` → `on_missing_route`.
5. **side_effects.when_types.** `notifyOrchestrator` emit point: if enclosed by
   `if (msg.type===A || msg.type===B ...)`, extract that type subset; else
   `when_types` = all types of the enclosing fall-through chain.
6. **conditional.** An enclosing guard like `onlineDevices.length > 0` or
   `!hasOtherBridge` → `requires_present_role: <role>`; if the pattern is
   unrecognized → `unsupported: true`.
7. **batch.** Emit points reached via `batchOutput`→`flushBatch` (setTimeout
   deferral) → `batch { window_ms, key }`. Because the data flows through a
   `Map` (not a direct call), the final outbound type is **not** statically
   resolvable → set `partial: true`, do not invent `transforms_to`.
8. **(no LLM step).** v1 has no LLM. `transforms_to` is never set; batch edges
   are `partial`.
9. **Dedup.** Merge edges with equal `(from_role, to_role, type, trigger)` by
   unioning `source.spans`.

### Validation gate (structural, deterministic)

There is no LLM, so there is no LLM/AST conflict to mediate. The only validation
is structural: the adapter must report every emit point (no silent drops), and
unrecognized shapes become `unsupported` edges (logged), never guesses.

## 7. Consumers

### 7.1 Dynamic test (first-version deliverable)

A new `//go:build integration` test `TestVocabularyDriven` in
`internal/head/agent/`:

- Loads `<dogfood>/.cerberus/vocab/<name>.vocab.yaml` at test start.
- Checks `source.files[].hash` against the on-disk source; on mismatch, logs a
  non-blocking warning (conservative hash — does not fail the test).
- Iterates `edges`. For each non-`unsupported` edge, emits a `t.Run` sub-case:
  - `from_role`/`to_role`/`trigger` → connection roles + which side sends.
  - `delivery.mode` → expect broadcast / single-bridge / unicast.
  - `requires_present_role` → set up that role first.
  - `side_effects` → where observable (e.g. `notify_orchestrator` via the
    capture server on `:9099`, reusing existing `gap E` wiring).
  - `partial` → skip and record a finding (precise transform assertion is future).
  - `unsupported` → skip + record finding.
- Supersedes the hardcoded `rows` in `TestBridgeToWebRelay` /
  `TestWebToBridgeRouting` (those tests can be deleted once parity is shown).

### 7.2 Scout runtime (interface reserved)

`project` loader gains a `Vocabulary` field populated from `vocab.yaml`
alongside `Protocol`. Scout can read it. **Wiring it into Scout's prompt/plan is
explicitly a follow-up**, not in this spec's deliverables.

### 7.3 Readable spec

The YAML file itself. Field domains and `source.spans` make it auditable
alongside the SUT source.

## 8. Command & file layout

- New subcommand: `cerberus protocol vocabulary <name> --from <source-file>`
  (sibling to `cerberus protocol infer`). Spawns `extractor.mjs`, writes
  `<project>/.cerberus/vocab/<name>.vocab.yaml`.
- `extractor.mjs` is bundled in the cerberus binary via `embed.FS`; runtime
  requires `node` on PATH (discovered at invocation; clear error if absent).
- `project` package gains a `Vocabulary` type + loader (mirrors `Protocol`).

## 9. Empirical validation (prototype, 2026-08-03)

A throwaway `extractor.mjs` (Steps 1–3 + fall-through) was run against the real
`room.ts`:

- **bridge→web: 38 types** — covers all **37** hardcoded in
  `TestBridgeToWebRelay` (1:1), plus `device:online` (a path the test misses).
- **web→bridge: 24 types** — **exact match** with `TestWebToBridgeRouting`.
- **Additional edges the hand-written tests miss:**
  - `session:send` web→web self-broadcast (`exclude_sender`).
  - `sessions:disconnected` on bridge disconnect.
  - `session:output-batch` (batch transform — a test blind spot).
  - `device:online` dual-trigger (system + bridge relay).

Coverage: 37/37 + 24/24 hardcoded types reproduced; 4 blind spots surfaced. The
emit-point + fall-through algorithm is empirically validated and the pure-AST
(zero-LLM) property confirmed (output fully deterministic).

Prototype simplifications (covered by the full algorithm, not algorithm gaps):
`ws.send` unicast typed as `(dynamic)`; `fetch` branches not yet split into the
three `trigger` values.

## 10. Risks & boundaries

| Risk | Impact | v1 mitigation |
|---|---|---|
| `ws.send` recipient role untraceable | unicast `to_role` unknown | mark `unsupported`, skip |
| Batch transform causality via `Map` | final type not statically resolvable | `partial: true`, weak-assert |
| TS grammar evolution vs locked ts-morph | parse failure on new syntax | pin ts-morph version; fail loudly, never silently drop |
| `node` runtime dependency | discovery not in production binary | accepted; dev-time tool; clear error if `node` absent |
| Whole-file hash false positives | spurious warnings on unrelated edits | warning is non-blocking; documented as conservative |

## 11. Open questions (resolve in plan, not here)

- Exact `extractor.mjs` packaging (single bundled file vs minimal node project
  extracted from `embed.FS` to a temp dir).
- Whether `cerberus protocol vocabulary` should also emit a human-facing
  markdown summary alongside the YAML.
- Capture-server reuse details for `notify_orchestrator` side-effect assertion
  in the dynamic test.
