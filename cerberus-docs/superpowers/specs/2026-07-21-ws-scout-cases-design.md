# WebSocket Realtime Engine (M3-2) — Scout-Generated WS Cases (Design)

**Date:** 2026-07-21
**Status:** Design (brainstormed; **ACTIVE — greenlit by the 2026-07-21 WS Tier-1 dogfood**: Finding 3 — Steer-LLM WS orchestration drift (`ws_*` vs `api_request` for the same target in one session) + breakage (connect×2, no send) — is the trigger the M3 proposal required)
**Scope:** `internal/head/scout/` (WS case generation from protocol declarations; planning-prompt WS awareness), `internal/head/agent/` (no change in the skeleton scope), `cerberus-docs/`
**Depends on:** M0–M2 (WS executor + protocol declaration), M3-1 (standalone protocol files)
**Proposal:** `cerberus-docs/superpowers/specs/2026-07-20-ws-realtime-engine-m3-proposal.md`

## Status note (read first)

This spec is written ahead of dogfooding. The M3 proposal gates Scout WS case
generation on real usage signals (users wanting reproducible WS tests;
orchestration-drift complaints). The design below captures the approach the
proposal leans toward (D2: case skeleton + LLM fills payload) so it is reviewable
and ready to execute when dogfooding greenlights it — but the LLM-prompt content
and the skeleton/coverage thresholds are explicitly **provisional** and must be
tuned against a real target before trusting. Sections marked 🐶 are the ones
dogfooding should validate.

## Background & Motivation

Today the WS executor (M0–M2) is exercised **only through the agent head's
runtime Steer LLM**: Scout's `Plan` (`internal/head/scout/plan_phases.go:59`)
emits `agent.TestCase` rows, but (a) the planning prompts know nothing of WS
primitives (`internal/head/scout/prompts.go`), and (b) `agent.TestCase`
(`internal/head/agent/types.go:22`) carries a single `Action` string hint with
no multi-step sequence, and (c) the rule engine (`matchRules`,
`internal/head/agent/rules.go:49`) has no WS phase — so even a
`Action:"ws_connect"` case falls through to the Steer LLM. WS execution is
therefore discovered at runtime, run-by-run, by the Steer LLM reading
`promptSteerSystem`.

That works (M0–M2 dogfood the Steer path) but is non-deterministic: two runs of
the same case may orchestrate the WS exchange differently, and the Steer LLM
re-derives the protocol's roles/message-types each run. M3-2 lets **Scout
generate WS-oriented cases from the protocol declaration** so the plan carries
the WS intent (which role, which decisive message type, which service)
deterministically, while the Steer LLM still fills payload-level detail. This is
the proposal's D2 posture: "reduces but does not remove LLM orchestration."

## Goal

When a project declares WS protocols (inline `protocol:` or M3-1
`protocol_ref:`), Scout's plan includes WS cases — one per declared
interaction/verification point — derived from the protocol's roles and the case
goal, so the agent head executes a WS scenario the plan explicitly asked for
(not one the Steer LLM improvised). The Steer LLM remains the orchestrator of
connect/send/receive within each case (skeleton + fill).

Success criteria (🐶 to validate):

- A project whose `project.yaml` declares a service with a `protocol:` (roles +
  message types) yields Scout cases targeting that protocol's roles/decisive
  types.
- Each WS case carries enough structure (service, role, awaited type, decisive
  flag) for the Steer LLM to execute it without re-inferring the protocol.
- Projects with no WS protocol declaration are unaffected (no WS cases; existing
  Go/Node/Python/HTTP behavior byte-identical).
- The Steer prompt's existing WS guidance (M0–M2) is reused unchanged; this
  sub-project adds Scout-side generation, not agent-side execution logic.

## Non-Goals

- **Full step-level determinism (D4).** A single `agent.TestCase` cannot carry a
  connect→send→receive→disconnect sequence (no `Steps` field). Achieving
  byte-reproducible WS orchestration would require either (a) a `Steps []…`
  extension to `TestCase` + a rule-engine WS matcher (`matchWSRules`) so the
  sequence executes without the Steer LLM, or (b) multi-case `DependsOn` chains.
  Both are larger changes whose value is unknown until dogfooding shows users
  actually want reproducible WS runs. **Deferred** — see Open Questions.
- **Scout generating message payloads.** The Steer LLM fills message content
  (proposal D2). Scout emits the structure (role, decisive type), not the JSON
  payloads.
- **Auto-inference of the protocol itself.** That is M3-3 (`protocol infer`).
  M3-2 consumes an already-declared protocol.
- **Capture-based case generation** (record a session, emit cases) — future
  (proposal D3 (b)).

## Design Decisions

### D1 — WS is a service-level concern, not a project type

`GenerateExecutorCases` (`internal/head/scout/plan_executor.go:11`) dispatches on
**project language** (Go/Node/Python via `DetectProjectType`, which sniffs
`go.mod`/`package.json`/`pyproject.toml`). WS is orthogonal: a service has a
`protocol:` regardless of the project's language. So M3-2 does **not** add a
`ProjectWS` type to `DetectProjectType`. Instead it adds a config-driven WS case
generator, `wsCases(cfg, goal) []agent.TestCase`, called from
`appendExecutorCases` (`plan_phases.go:86`) **alongside** (not instead of) the
language-based cases. A project can be Go *and* have WS services; both case sets
apply.

**Rejected — `ProjectWS` project type.** Conflates language detection (filesystem
markers) with service capability (config declaration). A WS API written in Go
would be misclassified.

### D2 — Skeleton scope: one case per verification point, Steer fills the rest (🐶)

Each generated WS case represents one **verification point** — a (service, role,
decisive message type) triple the plan wants confirmed — not a full message
sequence. The case's fields carry the intent:

| TestCase field | WS case content |
|---|---|
| `ID` | stable, e.g. `ws-<service>-<role>-<type>` |
| `Name` | human label, e.g. "bridge receives permission:response" |
| `Service` | the declaring service name |
| `Action` | `"ws_receive"` (the decisive verification primitive) |
| `Body` | JSON hints: `{"role":"bridge","type":"permission:response"}` |
| `Expectation` | free-text goal-aligned expectation (Examiner judges) |
| `DependsOn` | a `ws_connect` setup case for the same service+role (see D3) |
| `Priority` | from the goal/protocol salience |

The agent Steer LLM (which already knows WS primitives via `promptSteerSystem`)
reads `Body`/`Service`/`Action` and orchestrates the connect + any sends + the
decisive receive. The protocol declaration (M1/M2 + M3-1) is already in the
session context, so the Steer LLM does not re-infer auth/routing/framing — the
executor applies them.

**Rejected — full sequence per case (Steps field).** Requires extending
`agent.TestCase` and the rule engine; value unknown pre-dogfooding. The skeleton
reuses M0–M2's Steer path verbatim and is the proposal's leaning choice.

### D3 — A connect setup case per (service, role), linked by DependsOn

For each role the plan exercises, emit one `ws_connect`-hinted setup case
(`Action:"ws_connect"`, `Background:true`, `Body:{"role":…}`) and make the
decisive `ws_receive` case `DependOn` it. `DependsOn` (`agent.Deps`,
`internal/head/agent/types.go:41`) already exists and the plan executor honors
dependencies. This gives the agent a structural connect-before-receive ordering
without a multi-step case. (🐶 whether the agent head's dependency scheduling
actually runs the connect case before the receive case in practice — verify.)

### D4 — Which roles/types become cases (🐶)

For a service with `protocol.roles`, generate one decisive receive case per
(role, goal-relevant message type). "Goal-relevant" is the open question: the
simplest deterministic rule is "every role's handshake `await_type` + the goal's
named target type if the goal mentions one." A smarter rule needs the planning
LLM. **M3-2 starts deterministic and minimal** (handshake await_types + any type
named in the goal string), and dogfooding decides whether to involve the
planning LLM for richer coverage. This keeps generation testable without an LLM
call (unlike M3-3).

### D5 — Planning-prompt WS awareness (🐶)

`promptPlanSystem` / `promptPlanSystemLocal` (`internal/head/scout/prompts.go`)
today emit HTTP-only or code-only cases. When the config has WS services, the
planning prompt should mention that WS cases will be auto-generated (so the LLM
does not duplicate them) and may surface WS-flavored expectations. This is a
best-effort, inline edit to the raw-string prompt literals. Provisional content,
tune via dogfooding.

## Schema / Code Changes (sketch — full code in the plan)

- `internal/head/scout/plan_executor.go` — add `WSCases(cfg *project.Config, goal string) []agent.TestCase` (exported; reads `cfg.Services` for `Protocol != nil`).
- `internal/head/scout/plan_phases.go` — `appendExecutorCases` calls `WSCases` and appends.
- `internal/head/scout/prompts.go` — conditional WS-awareness sentence (provisional).
- **No** `agent.TestCase` struct change (skeleton scope reuses existing fields).
- **No** rule-engine / executor change (Steer path unchanged).

## Testing Strategy

- `WSCases` unit test: a `*project.Config` with a service declaring roles+handshake yields the expected connect+receive cases (IDs, Action, Body role/type, DependsOn); a config with no WS protocols yields nil; the goal string mentioning a type adds that decisive receive.
- Regression: a project with no protocol declarations → `appendExecutorCases` output unchanged (existing Go/Node/Python cases intact).
- (🐶 integration) an end-to-end Scout→agent run against a WS test server actually executes a generated WS case — deferred to dogfooding (needs a target).

## Relationship to M0–M3

- Consumes M1/M2 protocol declarations + M3-1 standalone files (reads resolved `svc.Protocol`).
- Reuses M0–M2 Steer-driven WS execution unchanged.
- Pairs with M3-3 (`protocol infer`), which can produce the protocol declarations M3-2 consumes.
- The deferred full-determinism path (Steps + `matchWSRules`) is a future sub-project gated on the D4 reproducibility signal.

## Open Questions (🐶 — dogfooding decides)

1. **Skeleton vs full-determinism.** Do users actually want reproducible WS runs (→ build Steps + matchWSRules), or is Steer-orchestrated-but-Scout-seeded enough? Validate before building the larger change.
2. **Coverage rule.** Is "handshake await_types + goal-named type" enough coverage, or should the planning LLM pick salient types? Start minimal.
3. **Dependency scheduling.** Does the agent head run a `Background`/`DependsOn` connect case before the decisive receive in practice? Verify the ordering holds.
4. **Prompt content.** Exactly how much WS context the planning prompt needs — tune against a real target.
