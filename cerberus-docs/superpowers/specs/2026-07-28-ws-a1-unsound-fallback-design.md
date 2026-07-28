# A1 unsound-WS-flow fallback (Phase 1: plan-time structural gate) — 2026-07-28

## Context

When the LLM authors a `ws_flow` case, `assemblePlan` (`internal/head/scout/assembly.go`,
`flush`) marks every role the case connects (`ws_connect` with `Role`) as
**covered** in the per-service map. `WSCasesCovered` (`internal/head/scout/ws_cases.go`)
then suppresses ALL deterministic cases for a covered role — the relay case
(`if !svcCovered[rc.Steps[0].Role]`) and the per-role loop
(`if covered[svc.Name][roleName] → continue`).

This "A1 coexistence" avoids redundant sockets, but it makes the LLM case a
**single point of failure** for the role: if the LLM `ws_flow` is broken at
runtime, the role has no fallback and is stranded.

## Root cause

`covered` is built structurally — any `ws_connect` with a `Role` marks the role
covered — with no check that the case's receives can actually match a real
server frame. The observed failure mode (Finding-2 probe, 2026-07-27): a
connect-only goal led the LLM to emit an **unrealistic `ws_receive`** of an
invented type ("message") that the server never sends to a lone client → the
receive times out → the case fails, and with the deterministic fallback
suppressed the role is uncovered.

Plan-time signals can detect this specific failure: a receive whose type is
**not grounded** in anything the protocol or goal declares is very likely
invented and will not match.

## Design (Phase 1): gate `covered` on case soundness

Tighten the construction of `covered` so a role is treated as covered only when
a **sound** LLM `ws_flow` covers it. The unsound LLM case itself stays in the
plan (it may still be right); the deterministic fallback is simply no longer
suppressed for that role.

### Soundness rule (pure, unit-testable)

An LLM `ws_flow` case is **sound** iff **every** `ws_receive` step in it has a
**grounded** type. A type is grounded for the case's service iff it equals
(compared by `sanitizeTypeID`, consistent with `wsDecisiveTypes` dedup) any of:

1. any role's `Handshake.AwaitType` in that service's protocol (this includes
   relay signals — optional-handshake await types); OR
2. a type named in the goal (`wsTypesNamedInGoal(goal)`); OR
3. any entry in that receive step's `Aliases`.

A case with no `ws_receive` (connect-only, send-only) is trivially sound. A
service with no declared protocol grounds only via the goal; a receive not in
the goal is therefore ungrounded → unsound. Asserts are intentionally not part
of soundness — malformed asserts are tolerated at execution by the D4 defense
(commit cf638a0), so they cannot strand a role on their own.

### Mechanism

In `assembly.go`:

- Precompute a `map[string]*project.Protocol` (service name → protocol) once
  from the existing `services` argument; the `flush` closure captures it.
- In `flush`, before iterating `open.Steps` to mark `covered`, compute
  `sound := llmWSFlowSound(open, protoOf(open.Service), goal)`. Only when `sound`
  are a case's `ws_connect`-with-`Role` steps recorded into `covered`. `*open` is
  appended to `cases` unconditionally (policy: keep the LLM case).

New scout-local pure helpers:

- `wsTypeGrounded(typ string, aliases []string, proto *project.Protocol, goal string) bool`
- `llmWSFlowSound(tc *agent.TestCase, proto *project.Protocol, goal string) bool`
  — `true` when `tc` has no `ws_receive`, or every `ws_receive` is grounded.

### What changes

- `internal/head/scout/assembly.go` — `flush` gates `covered`-marking on
  soundness; service→protocol index. `assemblePlan` signature unchanged.
- `internal/head/scout/ws_cases.go` / `WSCasesCovered` — **unchanged**. The
  `covered` map contract is unchanged (still `map[svc]map[role]bool`); only its
  construction is more precise.
- **Scope**: the residual risk (and this gate) is direct-planning-only.
  `assemblePlan` is the sole builder of the LLM `covered` map (single call site,
  `direct_planning.go:87`). ToT planning passes an empty `covered`, so
  `WSCasesCovered` never suppresses there and the gate is not on that path.

### Why this is safe

`covered[svc][role]` becomes true if **any** sound case covers the role (a
sound case marks it; an unsound case does not). So:

- sound LLM coverage → deterministic suppressed (today's behavior, no redundant
  socket);
- only-unsound LLM coverage → deterministic fallback emitted alongside the LLM
  case (the residual-risk fix; redundant sockets — a relay fallback re-connects
  the receiver AND every peer — the price of not stranding the role).

## Test impact

- New unit tests (table-driven) for `wsTypeGrounded` / `llmWSFlowSound`:
  handshake type, goal type, alias → grounded; invented type ("message") →
  ungrounded; connect-only case → sound; mixed grounded + ungrounded receives →
  unsound.
- New assembly-level test: an LLM `ws_flow` covering a role with an invented
  receive → the role is **not** in `covered` → `WSCasesCovered` emits the
  deterministic fallback (the residual-risk proof). A sound relay/exchange →
  role covered (unchanged).
- Existing tests that hand-build `covered` and call `WSCasesCovered` directly
  (e.g. `TestWSCasesCovered_RelayDroppedWhenLLMCoversReceiver`) are unaffected —
  they bypass `assemblePlan`, and the `covered` contract is unchanged.
- Existing `ws_relay` assembly tests: verify their relay cases (receive type =
  a declared handshake await type) remain sound → covered. Adjust only if a test
  uses an ungrounded receive.

## Verification

- `make check` EXIT 0.
- The residual-risk scenario is proven by the assembly-level test (unsound
  receive → fallback emitted); a live dogfood is not required for Phase 1 (and
  the path only bites when the LLM emits an unsound case, which is itself rare).

## Out of scope (Phase 2 — separate spec)

- **Runtime fallback**: run the deterministic case for a role whose LLM case
  fails at execution (catches "right type but server silent / auth wrong", which
  plan-time cannot see). Requires agent/examiner orchestration — a cross-cutting
  change, deferred to its own design.
- **Self-handshake re-await**: an LLM `ws_receive` whose type equals the connect
  step's own (already-consumed) handshake await type is grounded by this rule
  and therefore not flagged. Narrow; revisit if observed.
- Dropping unsound LLM cases (policy alternative (b)) — rejected for Phase 1 in
  favor of keeping the LLM case + adding the fallback (simpler, and preserves a
  possibly-correct LLM case).
