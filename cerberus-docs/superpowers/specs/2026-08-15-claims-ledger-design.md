# Claims Ledger Design (cerberus)

Date: 2026-08-15
Status: approved (chat review with amendments #1-#4, session 2026-08-15)
Depends on: fidelity manifest (`Actor.fidelity`, merged 2026-08-15 b286704)

## Problem

Coverage measures whether the edges WE modeled got touched — its denominator
is our own model. Nothing in cerberus answers "did the product deliver what
IT claims?". In the 2026-08-15 fidelity-ladder session, coverage 1.0 hid
three unevidenced core promises (real CLI scheduling, multi-device
orchestration, permission approval) until a human asked. The claims ledger
replaces "did our model pass" with "is the product's own claim sheet
reconciled" — mechanically, with no human in the loop.

## Decisions (user-confirmed 2026-08-15)

1. **Hard gate** — critical claims not `proven` ⇒ session incomplete, exit 3.
2. **Auto-merge extraction** — LLM-extracted claims write straight into the
   version-controlled ledger (diff reviewable); no draft-approval step.
3. **Core-first scope** — findings backflow and `replicas` cardinality
   execution are deferred; `implies_cardinality` is recorded but not enforced.

## Data model

`.cerberus/claims.yaml` (per project, version-controlled, sibling of
vocab/protocols):

```yaml
source:
  files:
    - path: ../../../open-agents/README.md
      hash: sha256:...        # change detection, same convention as vocab
claims:
  - id: schedule-real-cli     # unique, kebab-case, stable across re-extraction
    text: "调度真实 AI CLI 执行任务"
    source_ref: "README.md:3" # traceable origin
    critical: true            # only when mapped to a discovered surface (see extraction)
    implies_cardinality: 1    # recorded, NOT enforced this round
    status_annotation: ""     # manual note; "wont-test(<reason>)" is the only gate exemption; PRESERVED on re-extraction
```

`TestCase` gains `Claims []string` (claim ids this case claims to prove).
Repair-loop / replacement cases INHERIT the original case's Claims.

## Claim status semantics (amendment #1)

A passing case's **evidence tier** is:
- `real` — the case connects as a real-process role, OR its send payloads
  (after placeholder resolution) reference a real-process actor's captured
  identity (deviceId etc. — the harness-captured values are available on the
  session).
- `emulated` — otherwise.

Claim status:
- `proven` — ≥1 passing bound case with evidence tier `real`
- `emulated-only` — passing bound cases exist, all `emulated`
- `unevidenced` — no bound cases, or all failing

For projects with NO real-process actors, every claim's best tier is
`emulated` — the fidelity watermark already says why.

## Extraction (auto-merge)

`cerberus claims extract --from <file-or-dir>` (Scout-driven LLM):
- Only FALSIFIABLE claims (reject marketing language); cap 15 per extraction
  (flag-adjustable).
- **Surface triage (amendment #2)**: a claim is `critical: true` only when it
  maps to a discovered test surface (declared service route, protocol message
  type, actor, or process spec); otherwise `critical: false` with
  `status_annotation: "no surface mapping"`. This keeps the hard gate free of
  un-testable noise with no human triage.
- Merge rules mirror vocab re-extraction: preserve `status_annotation` /
  `critical` overrides on existing ids, only append new ids; deletion needs
  explicit `--prune`.
- `cerberus run` auto-extracts first when claims.yaml is absent and a
  plausible doc source exists (README* under the project dir or the SUT repo
  root), then proceeds. The written file is version-controlled so the diff is
  the audit trail.

## Reconciliation + hard gate

New session step `reconcileClaims` after the Examiner phase (also on resume):
1. Collect per-claim bound cases (by TestCase.Claims) and their final
   verdicts.
2. Compute status per the semantics above.
3. Session summary gains `ClaimsProven / ClaimsEmulatedOnly /
   ClaimsUnevidenced` counts plus the red-line list; report renders
   `Claims: 5 proven / 2 emulated-only / 1 unevidenced` and one line per red
   claim.
4. **Gate**: any `critical: true` claim not `proven` (and without a
   `wont-test(...)` annotation) ⇒ session status `incomplete`, `cerberus run`
   exits **3** (distinct from execution failure). Legacy projects turning red
   on first upgrade is the feature working, not a regression.

## CLI surface

- `cerberus claims extract --from <path> [--max 15] [--prune]`
- `cerberus claims list` (render ledger + last-known statuses)
- `cerberus claims check` (reconcile from the latest session in the store)

## Validation

- Unit: schema/validation (unique ids, annotation syntax); merge-preservation
  on re-extract; the full reconciliation matrix (proven / emulated-only /
  unevidenced / real-tier via payload-reference / wont-test exemption /
  repair-case inheritance / exit code).
- dogfood: `ws-realtime` expected to turn RED on first run (permission and
  mission claims unevidenced) — asserts the gate bites; `realtime-e2e`
  expected to prove its L1/L2 claims — asserts the gate passes truth.

## Non-goals (deferred)

Findings backflow (real-run errors auto-entering the ledger as suspected
defects), `replicas` cardinality execution in the harness, static surface
inventory generators (HTTP routes / process registries).
