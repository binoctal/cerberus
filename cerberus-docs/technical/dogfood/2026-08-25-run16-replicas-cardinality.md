# Run 16 — replicas cardinality live-validated: 3 real bridges, claim proven at N=3

Date: 2026-08-25
Run 16 session: (runtime/logs, cerberus run exit 0) —
**699 pass / 0 fail / 0 uncertain / 0 recovered, coverage 100% (reached:true,
gaps:0)**, 25m18s, ~694K tokens (800K budget).

Implements: `cerberus-docs/superpowers/specs/2026-08-25-replicas-cardinality-design.md`
(spec) and the 2026-08-25 plan. Closes the last open non-goal of the
2026-08-15 claims-ledger spec: `replicas` cardinality execution.

## What shipped

- `Actor.replicas: N` expands at `project.LoadFromYAML` into `base-1..base-N`
  (index from 1 — existing paired-device D1 rows stay valid; live-verified:
  all three bridges paired and reached ready without re-pairing).
- `capture_json` dot-paths now support the same `{{actor.name}}` template as
  capture_file (the one field the expansion could not previously derive).
- `ProtocolRole.Claims` — SUT fact in YAML; `realE2ECases`/`realResponderCases`
  union role-declared claims onto generated cases at their struct-literal
  sites (`:191`/`:69`; the only Claims overwrite in scout, `:354`, belongs to
  the relay family and is untouched).
- `implies_cardinality: N` is ENFORCED: proven requires N distinct real
  ACTORS across the claim's passing real-tier cases (attribution: role-bound
  steps, `{{role.param}}` placeholders, raw-id body matches → owning actor).
  Shortfall demotes to emulated-only with `ClaimVerdict.Reason`
  ("cardinality k/N") surfaced in the gate's red lines. Unit-locked incl.
  the same-actor-two-roles misconfiguration guard.
- dogfood realtime-e2e: one `replicas: 3` actor replaces the two hand-written
  bridges; `bridge3` protocol role (bridge2's shape + cardinality claims on
  all three bridge roles); ledger gains `multi-device-orchestration`
  (critical, implies_cardinality: 3, text scoped to independent
  addressability — cross-device mission fan-out is the recorded follow-up);
  token budget 700K → 800K.

## Expected vs observed (every plan gate)

| Gate | Expected | Observed |
|---|---|---|
| exit code | 0 (not 3) | 0 |
| Real actors | all three bridges | bridge-pty-1, bridge-pty-2, bridge-pty-3 |
| Claims line | 2 proven / 0 emulated-only / 0 unevidenced / 1 wont-test | exactly that — `multi-device-orchestration` PROVEN at N=3 |
| Coverage | 100%, gaps:0 (vocab role namespace separate from protocol roles) | reached:true, gaps:0 |
| bridge3 cases | reale2e + realresp auto-generated per role | `ws-realtime-bridge3-reale2e-session` PASS; bonus `ws-realtime-bridge3-restart-pair` PASS (restart generator iterates real actors); no realresp for bridge3 — bridge2 never declared `responses` either, so per-replica realresp was never part of the shape |
| Budget | ≤800K | ~694K |

## Verification ladder

1. Unit TDD per plan task (all red-then-green); `make check` green.
2. `make integration-openagents` exit 0.
3. This live run.

## Residuals / observations

- Verdict count stayed ~700 (no case-count explosion): the third replica adds
  one reale2e case plus the restart pair — cardinality evidence, not volume.
- The claims gate now bites on replica pairing flakes by design: if the third
  bridge fails to pair, all 699 cases can pass and the run still exits 3 with
  "cardinality 2/3" — honest incompleteness, not a defect.
- Follow-up recorded (spec Non-goals): cross-device mission fan-out evidence
  (a single mission addressed to deviceIds: [d1,d2,d3]).
