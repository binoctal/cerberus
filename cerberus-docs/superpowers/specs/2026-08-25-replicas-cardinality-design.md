# Replicas Cardinality — Design Spec

Date: 2026-08-25
Status: approved in session (approach A + review amendments)
Closes: the last open item of the 2026-08-15 claims-ledger non-goals
(`replicas` cardinality execution; findings backflow and HTTP surface
generators already shipped; Examiner count/ordering dimensions already
shipped 7442490).

## Goal

Let ONE actor declaration stand for N real process instances, and make the
claims ledger's recorded-but-unenforced `implies_cardinality` an enforced
evidence bar: a claim that implies N instances is proven only when the
passing real-tier cases bound to it collectively exercise N DISTINCT real
identities.

Live validation: dogfood realtime-e2e grows from 2 to 3 real bridges
(`replicas: 3`), and its ledger gains a critical `multi-device-orchestration`
claim with `implies_cardinality: 3`.

## Evidence semantics (decided)

Cardinality proof = N distinct captured identity VALUES (deviceIds),
referenced across the claim's bound passing real-tier cases (cross-case
dedup). This extends today's real-tier bar (one real identity reaches
"real") consistently. For this iteration the claim text is scoped to
independent addressability of N real devices — cross-device mission fan-out
(one mission dispatched to all N devices) is deliberately a follow-up, and
the claim text must say so.

Counting basis: distinct deviceId values, NOT role count — two roles whose
credential_ref names the same actor must not double-count (a passing case
referencing either credits the same identity once).

## Components

### 1. Actor.replicas expansion (internal/project)

- `Actor.Replicas int` (`yaml:"replicas,omitempty"`); 0/absent = exactly one
  actor (today's behavior, back-compat).
- Expansion happens at ONE choke point: `project.Load` (both server and
  session load paths go through it). An actor with `replicas: N` becomes N
  actors named `<name>-1` … `<name>-N` (index from 1, so
  `bridge-pty` + `replicas: 3` reproduces the existing `bridge-pty-1/2`
  names bit-for-bit — wrangler D1 pairing rows stay valid, no re-pairing).
- All replica-varying string fields are authored with the existing
  `{{actor.name}}` template (setup argv, start argv, env.HOME,
  capture_file). NEW: `capture_json` dot-path values must also be templated
  (`devices.{{actor.name}}.deviceId`) — today only CaptureFile goes through
  `h.tmpl` (`harness.go:300` vs raw path at `:311`); template the dot-path
  in `capture()` with the same actor context.
- Validation (in Load, after expansion): `replicas` requires
  `fidelity: real-process` + a `process` block; `replicas >= 1`; expanded
  names must not collide with any other actor name.

### 2. Role-level claims binding (protocols yaml)

- `Role` gains `Claims []string` (`yaml:"claims,omitempty"`). SUT fact stays
  in YAML; scout stays generic.
- `realE2ECases` and `realResponderCases` union the role's claims into each
  generated case's `Claims`. HAZARD: `ws_cases.go:354` currently OVERWRITES
  `Claims` with `[]string{wsRelayClaimID}` — the union must happen at that
  assignment (dedup, order-stable), not before it, or role claims are
  silently erased.
- Existing behavior unchanged when a role declares no claims.

### 3. Cardinality enforcement (claims reconcile)

- `collectRealIdentities` additionally returns role→identity-value(s) (from
  the actor's captured PathParams, same source as today's flat
  realActorIds).
- `ReconcileClaims`: for a claim with `ImpliesCardinality: N > 0`, gather
  the distinct real identity values referenced by its passing bound cases
  (role-bound steps and `{{role.param}}` placeholder bodies both attribute
  to the role's captured identity values; raw-id body matches credit the
  matched id). Proven requires BOTH real-tier AND distinct count >= N;
  otherwise, if passing cases exist, status is emulated-only with the
  shortfall recorded in the verdict reason (e.g. "cardinality 2/3").
- Gate unchanged: critical + not proven + no wont-test ⇒ exit 3. A flaked
  third-bridge pairing therefore yields exit 3 even at 700/700 case passes
  — the gate biting honestly; this is expected and documented, not a defect.

### 4. Dogfood realtime-e2e at N=3

- project.yaml: the two hand-written bridge actors collapse to ONE
  `bridge-pty` actor with `replicas: 3`; all per-instance fields move to
  `{{actor.name}}` templates (incl. capture_json dot-path after #1).
- protocols/open-agents.yaml: add `bridge3` role block (copy of `bridge2`,
  credential_ref `bridge-pty-3`, `deviceId: "{{bridge3.deviceId}}"`), with
  `claims: [ws-relay-messaging, multi-device-orchestration]` on bridge,
  bridge2, bridge3 roles.
- claims.yaml: add `multi-device-orchestration`, critical,
  `implies_cardinality: 3`, text scoped per the evidence-semantics decision:
  "N=3 real bridge devices are each independently addressable and
  participate in real sessions (cross-device mission fan-out is a
  follow-up)". (Hand-curated addition, consistent with the ledger's
  version-controlled practice; ws-realtime's ledger keeps its copy
  documenting its own gap.)
- settings: `session_total_tokens` 700K → 800K (run 15 spent ~711K; ~15 new
  cases from bridge3's reale2e + realresp generation add judge load).
- Coverage denominator is UNAFFECTED: vocab edges use the generic `web` /
  `bridge` role namespace; `bridge3` exists only as a protocol role, and
  requiredEdges derive from vocab + HTTPTrigger, not from protocol-role
  enumeration. 100% coverage must be preserved (verify in the live run).

## Testing ladder

1. Unit: expansion (names, templates, validation errors); capture_json
   dot-path templating; role-claims union incl. the overwrite hazard;
   reconcile cardinality (proven at N, emulated-only at N-1, distinct-value
   counting with two-roles-one-actor misconfiguration).
2. `make check` (fmt + lint + test).
3. Live dogfood run: 3 real bridges pair and stay ready; bridge3 reale2e +
   realresp cases pass; `multi-device-orchestration` PROVEN in the run
   summary claims line ("N proven / 0 emulated-only / 0 unevidenced");
   coverage stays 100% (gaps:0); budget holds at 800K.

## Non-goals

Cross-device mission fan-out evidence (deviceIds: [d1,d2,d3] mission);
role-group auto-expansion in protocols yaml (bridge3 is hand-declared);
vocab-edge claims field; process-registry surface generators.
