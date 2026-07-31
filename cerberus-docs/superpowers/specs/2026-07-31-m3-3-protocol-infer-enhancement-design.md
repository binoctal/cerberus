# M3-3 `protocol infer` Enhancement — Design Spec

> Date: 2026-07-31. Enhances the existing M3-3 `cerberus protocol infer`
> (`internal/protocoldiscover`) so its LLM output aligns with the `project.Protocol`
> model the engine already supports, and validates the result end-to-end against
> the real, undocumented WebSocket target `open-agents`.
>
> Related: M3-3 trigger evidence
> (`cerberus-docs/technical/dogfood/2026-07-23-ws-tier2-open-agents.md`),
> S2 tool-calling migration ADR
> (`cerberus-docs/technical/decisions/2026-07-25-s2-analyze-drift-degrade.md`),
> original M3-3 plan (`cerberus-docs/superpowers/plans/2026-07-21-ws-protocol-infer.md`).

## Context

`cerberus protocol infer` drafts a WebSocket protocol declaration from docs/source
files for human review. It was implemented in M3-3 (2026-07-21) but never run
against a real target: the 2026-07-23 Tier-2 dogfood against `open-agents`
discovered that target's protocol by **manually reading `realtime/room.ts`**, and
used that discovery cost as the trigger evidence justifying M3-3 — it did not
actually exercise the command.

Meanwhile the `project.Protocol` model evolved well past what `Infer` produces:
`Roles` (named multi-connection types), `Batches` (batch decomposition), and
`RoleHandshake.Optional` (peer-gated conditional handshake) all exist in the
schema. `Infer`'s LLM schema and prompt predate all of them.

## Current state

`internal/protocoldiscover/infer.go`:

- `Infer` calls `driver.Decide(ctx, prompt, &inferOutput)` — free-form JSON,
  parsed by `ParseStructuredOutput`. The prompt **hand-writes a JSON shape
  string** (the code comment states: "Driver.Decide does not inject the schema
  into the prompt").
- `inferOutput` carries `Found/Framing/TypePath/Auth/Roles/Notes`. It does **not**
  model `Batches`, `RoleHandshake.Optional`, or full role detail.
- `ErrNoProtocol` is returned when `Found == false`.

`project.Protocol` (`internal/project/protocol_schema.go`) already supports:

- `Roles map[string]*ProtocolRole` — per-role `Params/Headers/Subprotocols/Handshake`.
- `RoleHandshake.Optional bool` — when true, a timeout still succeeds the connect
  (the peer-gated conditional handshake).
- `Batches map[string]*ProtocolBatch` — `{ItemType, ItemsPath}` decomposition.

The case layer already supports multi-connection relay orchestration via
`connection_id`-keyed `Steps` (`internal/head/agent/execute_phases_steps.go`).

## Gap

The single real gap: **`Infer` is not told how to produce any of the structures
the Protocol model already supports.** Its schema and prompt target the earliest
single-connection RPC shape. No底层 model change is needed — this work does not
touch `project.Protocol`, validation, or the executor.

The 2026-07-23 dogfood surfaced five real-protocol wrinkles. After calibration,
four are expressible in the Protocol model and in scope for `Infer`; one is not:

| open-agents wrinkle | Protocol model support | In scope? |
|---|---|---|
| Envelope nesting `{type,payload,timestamp}` | `TypePath` dotted path | yes |
| Message batching (`session:output` → `-batch`) | `Protocol.Batches` | yes |
| Peer-gated conditional handshake | `RoleHandshake.Optional` | yes |
| Multi-role connections (web/bridge) | `Roles` | yes |
| Dynamic URL path `/ws/{userId}` | **actor/auth-flow layer** (`auth.path_params`, `generated_path_params`) | **no** |

Dynamic URL path is an actor/auth-discover concern, not a `Protocol` field.
`protocol_draft` mirrors `Protocol`, so it cannot carry path params. Path-param
inference is out of scope; `/ws/{userId}` is resolved via actor configuration.

## Decision

Migrate `Infer` from `driver.Decide` (free-form JSON) to `driver.DecideWithTools`
(typed tool call), and extend the produced schema to cover the four in-scope
structures. Validate end-to-end against `open-agents` at signal level (one run,
assess coverage, record iteration points — no live case execution).

### Why tool-calling (and the honest caveat)

A typed `protocol_draft` tool gives more controllable LLM output than free-form
JSON and aligns with the S2 migration direction (Scout already uses
`DecideWithTools`).

**Caveat (recorded, not relitigated):** cerberus has no struct→schema reflection;
every tool's `InputSchema` across the codebase (including Scout's `analyzeTools`)
is a hand-written `map[string]any`. Migrating to a tool therefore **moves** the
hand-written shape from the prompt into the tool definition — it does **not**
eliminate the struct/schema synchronization surface. The sync risk is equivalent
to the status quo; the wins are output controllability and directional
consistency. If a future struct→schema reflection helper is built (project-wide
benefit), this tool benefits automatically.

## Architecture

### New: `internal/protocoldiscover/tools.go`

A single LLM tool `protocol_draft` whose `InputSchema` (hand-written
`map[string]any`, mirroring Scout's style) is the inferable subset of
`project.Protocol`:

- `found` (bool) — explicit "I looked; no WS protocol here" signal (see Error
  handling).
- `framing` (json|text|binary), `type_path` (dotted).
- `auth` (`{strategy, param, credential_ref}`).
- `roles` (map; each `{credential_ref, params, headers, subprotocols,
  handshake{await_type, timeout, optional}}`).
- `batches` (map; each `{item_type, items_path}`).
- `notes` (string).

A pure `argsToProtocol` helper assembles the parsed arguments into a
`*project.Protocol`. It is independent of the LLM client and unit-testable.

### Changed: `Infer` (`infer.go`)

- `driver.Decide(ctx, prompt, &out)` →
  `driver.DecideWithTools(ctx, prompt, []llm.Tool{protocolDraftTool})`.
- Parse the returned tool call's arguments via the existing args struct (the
  `inferOutput` type, retained and extended with `Batches`/`Optional`/full role
  fields), then `argsToProtocol` → `ValidateProtocol`.
- `buildInferPrompt` rewritten: remove the hand-written JSON shape block;
  replace with recognition guidance for the four in-scope structures (envelope
  nesting → `type_path`; coalesced batching → `batches`; peer-gated handshake →
  `handshake.optional`; web/bridge roles → `roles`) plus the existing
  `credential_ref` safety constraint (name an actor, never emit tokens).

### Unchanged

- `project.ValidateProtocol` remains the output gate.
- CLI (`cmd/cerberus/main_protocol.go`), `--from` file/dir reading, and the
  `runProtocolInfer` write/confirm/dry-run flow are untouched.

### Data flow

```
source files → readInputs → buildInferPrompt (recognition guidance, no JSON shape)
  → DecideWithTools(protocol_draft tool) → parse tool args
  → argsToProtocol → *project.Protocol → ValidateProtocol → return / write
```

### Isolation

`protocoldiscover` stays client-free (driver injected). `tools.go` holds only the
tool schema + the `argsToProtocol` assembly, both independently unit-testable.

## Error handling (three states, aligned with S2 drift policy)

Retaining `found` in the tool args separates "looked and found nothing" from
drift, mirroring the S2 ADR's drift-vs-transient split:

| Model behavior | Classification | Result |
|---|---|---|
| Tool call, `found=true`, args valid | normal | assemble → `ValidateProtocol` → return |
| Tool call, `found=false` | model explicitly found nothing | `ErrNoProtocol` (CLI clean exit) |
| Tool call, args fail to deserialize | malformed output | hard error (`could not parse model output`); raw response not leaked |
| Tool call, args parse but `ValidateProtocol` fails | invalid protocol | hard error (`model produced an invalid protocol: …`); raw response not leaked |
| **Zero tool calls** | **drift** | **hard error** (`model produced no protocol tool call`); no graceful degrade |

The previous design conflated drift with "not found" by mapping zero/empty
output to `ErrNoProtocol`. That masked model malfunctions. Drift is now a hard
error; only an explicit `found=false` yields `ErrNoProtocol`.

## Testing

Mock driver returns tool calls (same mock shape used by
`driver_tools_test.go`). `llm.NewMockClient` already supports this.

- **Positive:** tool call with `batches` + `optional` handshake + multi-role →
  assembled `Protocol` passes `ValidateProtocol`.
- **`found=false`** → `ErrNoProtocol`.
- **Zero tool calls** → hard error (NOT `ErrNoProtocol`). Negative-verification
  test: RED if the code path wrongly returns `ErrNoProtocol`.
- **Malformed args / missing required** → hard error whose message contains no
  raw LLM response.
- **`argsToProtocol`** unit-tested independently of the driver (batches map,
  optional flag propagation, role detail).

## Dogfood (signal level)

Goal: run the enhanced `protocol infer` once against `open-agents` and assess
how many of the four in-scope structures it produces, versus the protocol the
2026-07-23 dogfood discovered manually. No live case is executed.

Setup (from the 2026-07-23 dogfood, no Supabase/OAuth/secrets):

- `fnm use 22 && cd apps/api && npm run dev` (wrangler dev, port **8989**).
- Dev auth backdoor: `?type=web&token=demo_token` reaches the WS layer; a bridge
  client needs a `POST /api/dev/setup`-issued `token_…`.

Run:

```
cerberus protocol infer --name open-agents \
  --from apps/api/src/realtime --service rt --dry-run
```

Assess coverage against the manually discovered protocol (envelope
`{type,payload,timestamp}`; `devices:sync` peer-gated handshake; web/bridge
two-socket relay roles; `session:output`→`session:output-batch` batching).

Output: `cerberus-docs/technical/dogfood/2026-07-31-m3-3-protocol-infer-dogfood.md`
records per-structure coverage and prompt iteration points. Path-param handling
is noted as an actor/auth-discover follow-up, not an `Infer` gap.

## Rejected alternatives

- **A — extend fields only (keep `Decide`).** Smallest diff; extend `inferOutput`
  and the hand-written prompt shape in lockstep. Equivalent sync risk to B
  (hand-written schema moves, not removed). Rejected for weaker output
  controllability and divergence from the S2 direction — but it remains the
  fallback if tool-calling proves problematic.
- **C — multi-pass inference** (base, then roles/batches). More accurate but
  costlier and more complex; unjustified for signal-level dogfood. YAGNI.
- **Struct→schema reflection helper.** Would genuinely eliminate the
  hand-written-schema sync surface project-wide. Largest scope; deferred. The
  `protocol_draft` tool adopts it automatically if it is ever built.

## Out of scope

- Dynamic URL path-param inference (actor/auth-discover layer).
- Changes to `project.Protocol`, `ValidateProtocol`, or the executor.
- Live multi-connection case execution against `open-agents` (closed-loop
  validation is a follow-up once signal-level coverage is acceptable).
