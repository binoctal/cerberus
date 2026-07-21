# WebSocket Realtime Engine (M3-3) — `cerberus protocol infer` (Design)

**Date:** 2026-07-21
**Status:** Design (brainstormed; **PROVISIONAL — implementation deferred pending dogfooding signals**, per the M3 proposal trigger conditions)
**Scope:** `internal/protocoldiscover/` (new package, the inference core), `cmd/cerberus/main_protocol.go` (new `protocol infer` subcommand), `cerberus-docs/`
**Depends on:** M1/M2 (Protocol schema + ValidateProtocol), M3-1 (standalone protocol files — the output artifact)
**Proposal:** `cerberus-docs/superpowers/specs/2026-07-20-ws-realtime-engine-m3-proposal.md`

## Status note (read first)

Like M3-2, this spec is written ahead of dogfooding. The M3 proposal gates
`protocol infer` on a real blank-page-cost signal (users repeatedly hand-writing
descriptions). The architecture is well-bounded (it mirrors the shipped
`auth discover` command nearly exactly), so the spec/plan are concrete and
ready — but the **LLM prompt content and the input heuristics are provisional**
and must be tuned against real protocol docs/examples before trusting. Sections
marked 🐶 are the ones dogfooding should validate.

## Background & Motivation

M1/M2 protocol declarations are hand-written YAML. M3-1 made them standalone
shareable files. The remaining friction (proposal motivation #3): hand-writing a
description is a blank-page cost — the author must read the target's docs/source,
identify auth placement, the routing field, framing, and roles. `cerberus
protocol infer` has the LLM **draft** a description from protocol docs / message
examples, which the human reviews and commits (proposal D3: "Produce a draft the
human reviews — never an implicitly-trusted inferred protocol").

The architecture is a near-exact mirror of the shipped `auth discover` command
(`internal/authdiscover/discover.go:44` + `cmd/cerberus/main_auth.go`): a
package-private inference core that takes an `*ai.Driver` and returns a
validated `*project.Protocol`, and a Cobra subcommand that builds the driver,
calls the core, prints the draft, and writes it on confirmation.

## Goal

`cerberus protocol infer --name <name> --from <path> [--service <svc>] [--dry-run]`
asks the LLM to draft a `*project.Protocol` from the given docs/examples, validates
it (`ValidateProtocol`), prints it for review, and on confirmation writes
`.cerberus/protocols/<name>.yaml` (the M3-1 artifact). The human then reviews,
edits, and commits — the inferred description is never trusted implicitly.

Success criteria (🐶):

- Given protocol docs / message-example files, the command produces a
  `ValidateProtocol`-passing `*project.Protocol` draft (framing, type_path, auth,
  roles as the docs support).
- `--dry-run` prints the draft without writing.
- On write, the file lands at `.cerberus/protocols/<name>.yaml`; an existing file
  triggers an overwrite confirm (mirror `auth discover`).
- No credential values are ever sent to the LLM or written (auth uses
  `credential_ref` names + placeholders, never raw tokens — mirror
  `authdiscover`'s secret hygiene).
- `auth discover` continues to work unchanged (the new command is additive).

## Non-Goals

- **Capture-based inference** (record a WS session, infer the description) —
  proposal D3 (b), future.
- **Implicit trust / auto-wiring.** The command writes a draft file for human
  review; it does NOT auto-add `protocol_ref:` to `project.yaml` (the human wires
  it after reviewing). Keeps a human gate on correctness (proposal D3).
- **Inferring actor credentials.** The draft's `auth.credential_ref` / role
  `credential_ref` name an actor that must already exist in `project.yaml`; the
  command does not create actors or resolve tokens.
- **A protocol registry.** The file is shared manually via git (proposal OQ1).

## Design Decisions

### D1 — Mirror `auth discover` (package + command shape)

- `internal/protocoldiscover/infer.go` — `Infer(ctx, driver, cfg, serviceName, inputs) (*project.Protocol, error)`, mirroring `authdiscover.Discover`. Takes the driver (tests inject a mock; never builds LLM clients). Returns a validated `*project.Protocol` or a typed error.
- `cmd/cerberus/main_protocol.go` — `protocolCmd()` parent + `protocolInferCmd()` child, mirroring `authCmd`/`authDiscoverCmd` (`cmd/cerberus/main_auth.go:30,39`).
- Driver helper: mirror `newAuthDiscoverDriver` (`main_auth.go:168`). (Optionally factor a shared `newLLMDriver()`; out of scope — a `newProtocolInferDriver` copy is fine for now, factoring later if a third caller appears.)
- Testable core `runProtocolInfer(ctx, workDir, driver, opts) error` mirroring `runAuthDiscover` (`main_auth.go:75`): load project.yaml, call `Infer`, print, confirm, write.

### D2 — Input is docs / message-example files (🐶)

`--from <path>` accepts a file or directory. The core reads text files (Markdown,
OpenAPI YAML, `.json` message examples, source snippets) — the same
`selectSourceFiles`-style enumeration `authdiscover` uses, generalized to a
provided path rather than `cfg.Code.Root`. The prompt inlines the file contents
(mirror `buildDiscoverPrompt`). 🐶 which file types/structures yield good
inference — tune against real docs. Size-bounded (truncate large inputs) to keep
the prompt tractable.

**Rejected — capture replay (D3 (b)).** Future; needs a recording format first.

### D3 — Output is the M3-1 artifact; draft + human gate

The draft is written to `.cerberus/protocols/<name>.yaml` (`--name` required,
validated as a plain name via the M3-1 `checkProtocolRefName` rule — no path
traversal). The command:
1. Prints the rendered YAML.
2. `--dry-run` → stop.
3. Else confirm prompt (overwrite confirm if the file exists) → write.

The human then reviews, edits, commits, and wires `protocol_ref: <name>` onto the
service themselves. The command never auto-wires (proposal D3 human gate).

### D4 — Secret hygiene (mirror authdiscover)

The inferred `auth`/roles use `credential_ref` (an actor name) and placeholders
only. The prompt instructs the LLM NEVER to emit real credential values, and the
inputs (docs/examples) the user provides are the user's responsibility — but the
core never injects credentials into the prompt and the written file carries no
secrets (only refs/placeholders). `ValidateProtocol` runs on the draft; an
invalid draft is reported, not written.

### D5 — Validation before write

`project.ValidateProtocol(p, cfg.Actors)` runs on the LLM output (the same Phase
6 check config load uses). A draft referencing an unknown `credential_ref` (no
such actor) fails validation → reported to the user, not written. This catches
the most common inference error (naming a non-existent actor) before it lands.

## Schema / Code Changes (sketch — full code in the plan)

- **New** `internal/protocoldiscover/infer.go` — `Infer`, `inferOutput` (JSON shape), `buildInferPrompt`, `selectDocFiles`.
- **New** `cmd/cerberus/main_protocol.go` — `protocolCmd`, `protocolInferCmd`, `runProtocolInfer`, `newProtocolInferDriver`.
- **Modify** `cmd/cerberus/main.go:15` — register `protocolCmd()` in `AddCommand`.
- **No** change to `internal/project/` (Protocol/ValidateProtocol exist), the executor, or Scout.

## Testing Strategy

Mirror `authdiscover`'s tests:
- `Infer` unit test with a mock `*ai.Driver` returning a canned `inferOutput` JSON → asserts the returned `*project.Protocol` matches + passes `ValidateProtocol`; a `found:false`-style output → a typed "no protocol found" error; a structurally-invalid LLM output → a parse error that does NOT leak the raw response.
- `runProtocolInfer` test with a mock driver + temp dir: `--dry-run` prints without writing; confirm=yes writes `.cerberus/protocols/<name>.yaml`; existing-file → overwrite confirm; `--name` with a path-traversal value → rejected.
- Regression: `auth discover` still works (additive command; no shared code mutated).
- (🐶 integration) real docs → a usable draft — deferred to dogfooding.

## Relationship to M0–M3

- Produces the M3-1 artifact (`.cerberus/protocols/<name>.yaml`); M3-1's loader resolves it.
- Output feeds M3-2 (Scout WS cases consume the declared protocol).
- Mirrors the shipped `auth discover` pattern end-to-end.

## Open Questions (🐶)

1. **Input quality.** Which doc formats (OpenAPI? Markdown? raw message JSON?) yield reliable inference? Start permissive (any text), tune via dogfooding.
2. **Role inference.** Can the LLM reliably infer roles + handshake from docs, or only framing/type_path/auth? Start with whatever the docs support; flag low-confidence fields in `notes`.
3. **Driver helper duplication.** `newAuthDiscoverDriver` / `newProtocolInferDriver` duplicate ~20 lines. Factor a shared `newLLMDriver()` when a third caller appears (YAGNI now).
4. **Auto-wire protocol_ref.** Should the command optionally add `protocol_ref: <name>` to the named service after writing? Deferred (human-gate posture); revisit if users ask.
