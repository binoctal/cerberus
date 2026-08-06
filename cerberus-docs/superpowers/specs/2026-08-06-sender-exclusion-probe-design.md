# Sender-Exclusion Probe — Design

> Status: design approved for planning. Date: 2026-08-06.
> Context: follow-up to the four-category drift split. The recurring `routing`
> drift lands as `honest-uncertain` because the `membership` dimension leaves
> `Excluded` nil (`sender exclusion not probed`). This spec closes that gap.

## Problem

Two validation runs (2026-08-06-examiner-vocab-validation,
2026-08-06-structured-evidence-validation) converge on the same finding: the
Examiner judge never mislabels a pass-case (`incorrect = 0`), and the only
recurring drift is the `routing` case. Its root cause is **evidence
insufficiency, not type vocabulary**: a single matched message cannot prove
multi-peer fan-out or sender-exclusion, so a careful judge is correctly
uncertain regardless of vocabulary.

The infrastructure is already in place except the active probe:

- `types.Dimension.Excluded *bool` — "only set when actively probed."
- `examiner.deriveDimensions` derives `membership` (recipients + sender) but
  leaves `Excluded` nil, rendering `"sender exclusion not probed"`.
- `WebSocketExecutor.doReceive` already returns a `"timeout"` status when no
  frame matches within the window — i.e. the executor can already observe
  "message did not arrive."

The missing piece is an **active negative-receive step** on the sender's own
connection that asserts the broadcast type does NOT arrive, plus the plumbing
to turn that outcome into `Excluded = *bool`.

## Goal

Convert the `routing` case's `honest-uncertain` verdict into a confident,
evidence-grounded verdict (`clean` when the sender is correctly excluded,
`incorrect` when the server wrongly echoes to the sender). `Excluded` becomes
a measured value wherever a probe runs, and stays nil (unchanged prompt) where
no probe runs — so non-relay and non-WS cases are byte-identical (zero
regression).

## Non-Goals (out of scope)

- Full observed-vs-expected peer-set membership (the "Option B" broad design).
  Only sender self-exclusion is probed. Broader fan-out membership is a
  follow-up that depends on a reliable expected-peer-set source.
- Changes to the Scout protocol-vocabulary pipeline, the LLM judge prompt
  template, the drift metric, or the confidence threshold.
- Auto-injection of probes by the Agent at execution time. The probe is planned
  by Scout (see Decision 1).

## Decisions

### Decision 1 — Scout plans the probe (not the Agent)

The negative-receive step is a first-class step in the relay test case,
authored by Scout, not auto-injected by the Agent executor. Rationale: the
probe is test intent, Scout has the fullest context (sender connection,
broadcast type, relay structure), and it keeps the Scout-plans / Agent-executes
separation clean. The Agent executor only needs to honor the new flag.

### Decision 2 — `ExpectAbsent` flag, inverted success semantics

Add an `ExpectAbsent bool` to `WSReceiveAction` (and `TestStep`). When true,
`doReceive` inverts success:

- No match before timeout (`"timeout"` status) ⇒ `WSResult{OK: true}` — the
  sender did NOT receive its own broadcast, the desired outcome.
- A matching frame arrives ⇒ `WSResult{OK: false, Err: ...}` — the server
  echoed to the sender, a real relay bug.

This reuses the existing timeout machinery; no new timing path. `Matched` on
the resulting Evidence still reflects whether a frame matched (true when the
server wrongly echoed), which is exactly what `deriveDimensions` needs to set
`Excluded`.

### Decision 3 — Probe marker carried on Evidence

Add `ExpectAbsent bool` to the agent `Evidence` struct, populated by
`stepEvidence` from the step. This lets `deriveDimensions` distinguish a
genuine negative probe (ExpectAbsent=true, on the sender connection, for the
broadcast type) from an ordinary receive that merely failed to match — without
which every failed receive would be misread as an exclusion signal.

### Decision 4 — `Excluded` derived from probe outcome

In `deriveDimensions`, after computing sender + recipients per type, if a
matching probe Evidence exists (ExpectAbsent=true, ConnectionID == sender,
MatchedType == the broadcast type), set `Excluded` from the probe outcome:

- Probe `Matched == false` (timed out, OK) ⇒ `Excluded = *true` (sender excluded).
- Probe `Matched == true` (server echoed) ⇒ `Excluded = *false`.

When no probe ran for a type, `Excluded` stays nil — the existing
`"sender exclusion not probed"` render is unchanged.

### Decision 5 — Scout gates the probe on the relay fan-out pattern

Scout appends the negative-receive step only for relay/broadcast fan-out cases
where it already models the sender connection and broadcast type — the same
cases that emit the `ws_flow` membership dimension. Non-relay cases are
untouched. The probe reuses the broadcast type and sender connection Scout
already binds.

## Architecture

Data flow (relay case):

```
Scout relay case
  └─ steps: [connect web, connect bridge, connect sender, ws_send sender T,
             ws_receive web T, ws_receive bridge T,
             ws_receive sender T (ExpectAbsent=true)]   ← NEW probe step
        │
        ▼
Agent stepToAction → WSReceiveAction{ExpectAbsent:true}
  └─ doReceive: inverts success on ExpectAbsent
        │
        ▼
WSResult (OK = sender did NOT receive) → stepEvidence
  └─ Evidence{Action:"ws_receive", ConnectionID:"sender",
              MatchedType:"T", Matched:false, ExpectAbsent:true}
        │
        ▼
Examiner deriveDimensions
  └─ membership dim: recipients=[web,bridge], sender=sender,
                    Excluded = *true   (was nil)
  └─ renderDimensions → "sender excluded"   (was "not probed")
        │
        ▼
Judge: confident verdict (clean / incorrect) instead of honest-uncertain
```

## Components & Changes

### `internal/types/actions_http.go` — `WSReceiveAction.ExpectAbsent`
- Add `ExpectAbsent bool \`json:"expect_absent,omitempty"\``.
- `Validate()`: no new constraint (type + connection_id already required).

### `internal/head/agent/execute_phases_steps.go` — `TestStep` + `stepToAction`
- Add `ExpectAbsent bool` to `TestStep`.
- `stepToAction` ws_receive branch: thread `ExpectAbsent` onto the action.

### `internal/head/agent/websocket.go` — `doReceive`
- When `a.ExpectAbsent`, invert the outcome: timeout ⇒ OK; match ⇒ fail with
  an `Err` describing the unwanted echo. Keep evidence (MatchedMessage etc.)
  populated so a real echo is visible.

### `internal/head/agent/types.go` — `Evidence.ExpectAbsent`
- Add `ExpectAbsent bool \`json:"expect_absent,omitempty"\``.

### `internal/head/agent/execute_phases_steps.go` — `stepEvidence`
- Populate `ev.ExpectAbsent = s.ExpectAbsent` for ws_receive.

### `internal/head/examiner/dimensions.go` — `deriveDimensions`
- Detect the sender probe and set `Excluded` per Decision 4.
- Source-1 (result-carried) dimensions still win on (Kind, Label) conflict.

### `internal/head/scout/ws_cases.go` — relay fan-out case
- Append the negative-receive probe step for the sender connection + broadcast
  type when generating a relay fan-out case.

## Testing

1. **Unit (default build):**
   - `deriveDimensions`: with probe Evidence (Matched=false) ⇒ `Excluded=*true`;
     with probe Evidence (Matched=true) ⇒ `Excluded=*false`; with no probe ⇒
     `Excluded=nil` (regression). Table-driven.
   - `stepEvidence`: ExpectAbsent threaded onto Evidence.
   - `doReceive` / executor-level: ExpectAbsent inverts success (timeout=pass,
     echo=fail). Reuse the existing WS executor test harness.
   - `stepToAction`: ExpectAbsent round-trips onto the action.
2. **Scout:** relay fan-out case includes the sender probe step; non-relay cases
   do not (assembly test, alongside `TestAssemblePlan_DropsEmptyWSFlowCase`).
3. **Manual validation (`//go:build manual`):** extend `buildValidationCases`
   with a `routing` variant whose Evidence carries the probe. The judge should
   leave `honest-uncertain` — landing `clean` (Excluded=true) or `incorrect`
   (Excluded=false). Re-report under the four-category metric.

## Success Criteria

- `Excluded` is a measured `*bool` whenever a relay case carries the probe;
  `nil` otherwise (zero-regression, byte-identical prompt for non-probe cases).
- The `routing` case in the manual validation harness no longer reports
  `honest-uncertain` as its best outcome — it resolves to `clean` or
  `incorrect` based on the probe.
- `make check` (fmt + lint + `go test -race ./...`) passes; the manual build
  (`go vet -tags=manual`) compiles.

## Risks

- **Probe timing.** The negative receive waits out its full timeout on every
  relay case (no frame will arrive). Scout must use a short timeout (a few
  seconds) so this does not dominate case wall-clock.
- **Echo semantics vary by system.** Some legitimate designs echo to the
  sender. Scout only adds the probe for relay fan-out cases whose contract is
  sender-excluded; the probe is a faithful assertion of that contract, not a
  blanket assumption.
- **Judge prompt.** No prompt template change; `renderDimensions` already
  renders `Excluded` for all three states (`nil`/`*true`/`*false`). The judge
  sees a concrete fact where it previously saw an absence.
