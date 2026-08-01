# Repeatable protocol-infer benchmark — Design

- Date: 2026-08-02
- Scope: a repeatable, env-gated scoring benchmark for `cerberus protocol infer`
  against the `open-agents` target, plus one real N=18 run recorded into the
  dogfood doc. Replaces subjective "looks right over 3-5 runs" judgement with a
  per-structure hit-rate table judged against thresholds.
- Lives in: `tools/protoinferbench/` (mirrors the existing `tools/slowtest/`
  pattern: a `package main` tool alongside the main module).

## Problem

Whether `protocol infer` "passes" on `open-agents` has so far been judged by
eyeballing 3-5 real runs. Run-to-run variance (false `found=false`, hard errors,
parse failures, dropped hard structures) dominates such small samples, so
conclusions keep flip-flopping. We need: enough samples (N>=15), per-structure
scoring against a fixed ground truth, and threshold-gated PASS/FAIL.

## Non-goals

- Changing `internal/protocoldiscover/` inference behaviour. This is a
  measurement tool only; the inference code is the system under test.
- A general-purpose benchmark harness for arbitrary targets. Ground truth and
  thresholds are hard-coded for `open-agents` / `apps/api/src/realtime/room.ts`.

## Architecture

`tools/protoinferbench`, `package main`. Three files:

| File | Responsibility | Network? |
|---|---|---|
| `score.go` | Pure functions: `ParseDraft`, `Score`, `Aggregate`, `Scorecard`, threshold table. | No |
| `score_test.go` | Table-driven unit tests + `testdata/*.yaml` regression anchors. | No |
| `main.go` | Orchestration: flags, health check, spawn `cerberus` N times, parse, score, print table. | Yes (only in `main()`/`runBench`) |

Dependencies: existing `gopkg.in/yaml.v3` and `internal/project` (unmarshal into
`*project.Protocol`). No new module deps.

### Test isolation guarantee (env-gate)

All network-touching code (`exec.Command`, health HTTP GET) lives exclusively in
`main()`/`runBench()`. `Score`/`ParseDraft`/`Aggregate` are pure functions with
no I/O. `go test ./tools/protoinferbench/` therefore exercises only the pure
functions — it is structurally incapable of hitting the network, so `make check`
stays EXIT 0 with no LLM calls.

As belt-and-suspenders, `runBench()` returns immediately (printing a skip line,
EXIT 0) unless `CERBERUS_BENCH=1` is set. The real N=18 run is invoked as the
built binary, never by `go test`.

## Data flow

```
main()
 |- parse flags: -n (default 18), -binary (default build/cerberus),
 |               -health-url (default http://localhost:8989/health),
 |               -workdir (default cwd; must be open-agents repo root),
 |               -name/-from/-service (default open-agents / apps/api/src/realtime / api),
 |               -per-call-timeout (default 120s)
 |    (the bench does NOT pass --samples; each run uses the binary's default,
 |     currently 3. The report header reflects whatever the binary used.)
 |- runBench()
 |    |- guard: if CERBERUS_BENCH != "1" -> print "skip (CERBERUS_BENCH unset)", return nil
 |    |- health check: GET health-url; non-200 -> fail-fast, EXIT 1
 |    |- for i in 1..N:
 |    |     exec.Command(binary, "protocol","infer","--name",name,"--from",from,
 |    |                      "--service",service,"--dry-run")
 |    |     cwd = workdir; env = os.Environ() (ANTHROPIC_AUTH_TOKEN/BASE_URL must be present)
 |    |     capture stdout+stderr+exitcode; classifyRun() -> RunResult{Outcome, Proto}
 |    |- Aggregate(results) -> []Scorecard
 |- print markdown table (per-structure rates + PASS/FAIL + outcome breakdown)
 |- EXIT 0 regardless of PASS/FAIL (non-zero only on health/env errors)
```

### run classification

`classifyRun(stdout, stderr, exitCode) -> RunResult`:

| exitCode | stdout | Outcome | Proto |
|---|---|---|---|
| 0 | has `Draft protocol` prefix + YAML parses | `draft` | parsed `*Protocol` |
| 0 | contains `no WebSocket protocol found` | `no_protocol` | nil |
| 0 | has prefix but YAML unmarshal fails | `parse_fail` | nil |
| != 0 | anything | `hard_error` | nil |

For `no_protocol`, `parse_fail`, `hard_error`: `Proto == nil`. In scoring these
count as a miss for **every** structure, but the denominator stays N (no runs are
dropped — matches the brief: pass-2 drift / failures count as "structure not
hit").

## Scoring

`Score(proto *project.Protocol) [numStructures]bool` — fixed-order structure
hits. `Score(nil)` returns all-false.

| # | Structure | Hit when |
|---|---|---|
| 1 | `framing` | `Framing == "json"` |
| 2 | `type_path` | `TypePath == "type"` |
| 3 | `auth` | `Auth != nil && Auth.Strategy == "query" && Auth.Param == "token"` (credential_ref ignored) |
| 4 | `roles` | has role `web` with `web.Params["type"] == "web"` AND role `bridge` with `bridge.Params["type"] == "bridge"` |
| 5 | `handshake` | role `web` exists AND `web.Handshake != nil && AwaitType == "devices:sync" && Optional == true` |
| 6 | `batch_keys` | `Batches["session:output-batch"]` exists AND its `ItemType == "session:output"` (flush key + item_type combined) |
| 7 | `batch_items_path` | that batch's `ItemsPath == "payload.lines"` |

Notes on alignment with the brief's ground truth:
- Handshake is required on the **web** role specifically (the guarded
  `devices:sync` send fires in the web connect path). A correct handshake
  present only on `bridge` does not count.
- `batch_keys` merges flush_key + item_type into one scored structure, matching
  the brief's "batch flush_key + item_type 正确: >=50%" threshold line.
- `credential_ref` is explicitly not scored.

### Thresholds and PASS/FAIL

| Structure | Threshold |
|---|---|
| framing | >= 95% |
| type_path | >= 95% |
| auth | >= 95% |
| roles | >= 90% |
| handshake | >= 60% |
| batch_keys | >= 50% |
| batch_items_path | >= 40% |

Overall **PASS = all 7 structures at/above threshold.** Per-structure
`Result = PASS if rate >= threshold else FAIL`. A structure's rate is
`hits / N` where N is the total run count (failed runs contribute 0 hits but
count in the denominator).

### Run-outcome breakdown

In addition to per-structure rates, report the count of each `Outcome`
(`draft` / `no_protocol` / `parse_fail` / `hard_error`) over the N runs. This
distinguishes "model omitted a structure" from "the whole run collapsed", which
is needed to judge whether the hard structures are the real problem or merely a
symptom of run-level instability.

## Output format

Markdown, directly pasteable into the dogfood doc:

```
Protocol infer benchmark — open-agents (N=18, samples=<binary default> per run)

Run outcomes:
  draft       : 15
  no_protocol :  1
  parse_fail  :  1
  hard_error  :  1

Per-structure hit rate:
| Structure        | Hits  | Rate  | Threshold | Result |
|------------------|-------|-------|-----------|--------|
| framing          | 18/18 | 100%  | >=95%     | PASS   |
| type_path        | 18/18 | 100%  | >=95%     | PASS   |
| auth             | 17/18 | 94%   | >=95%     | FAIL   |
| roles            | 16/18 | 89%   | >=90%     | FAIL   |
| handshake        |  7/18 | 39%   | >=60%     | FAIL   |
| batch_keys       |  9/18 | 50%   | >=50%     | PASS   |
| batch_items_path |  4/18 | 22%   | >=40%     | FAIL   |

Overall: FAIL (4/7 structures)
```

(Numbers above are illustrative, not real results.)

## Error handling

- `CERBERUS_BENCH != "1"` when invoked (e.g. accidentally under `go test`) ->
  print skip line, EXIT 0, no request.
- Health check non-200 / unreachable -> print message, EXIT 1, no infer calls.
- Missing `ANTHROPIC_AUTH_TOKEN`/`ANTHROPIC_BASE_URL` in env -> detect before the
  loop, print message, EXIT 1 (the first infer would otherwise fail N times).
- Single infer subprocess exceeds per-call timeout -> counted as `hard_error`,
  loop continues to the remaining runs (one stuck call must not hang the bench).
- On `hard_error`/`parse_fail`, the subprocess's first stderr line is printed as
  a one-line diagnostic.

## Testing (TDD)

Pure-function unit tests in `score_test.go`, table-driven:

- `TestParseDraft`: valid prefixed YAML parses; `no WebSocket protocol found` ->
  `no_protocol`; truncated/corrupt YAML -> `parse_fail`.
- `TestScore`: cases covering an all-hit draft, the near-perfect Run-22 shape
  (handshake + correct batch keys), a draft missing handshake+batch, and an
  empty `*Protocol`.
- `TestAggregate`: a mixed `[]RunResult` (some `draft`, some
  `no_protocol`/`hard_error`) -> asserts correct hit numerators/denominators
  (failed runs count in denominator), correct PASS/FAIL per structure, and
  overall verdict at threshold boundaries.
- `testdata/*.yaml`: the real Run-2 and Run-22 drafts from the dogfood doc are
  committed as regression anchors; `Score` on each is asserted to hit the
  expected structure set.

All tests are pure (no subprocess, no HTTP), so `make check` is network-free and
EXIT 0.

## Real run

After the tool lands and passes `make check`:

1. `make build` (builds `build/cerberus`).
2. In a separate terminal: start `open-agents` wrangler dev on :8989 and confirm
   `curl -sf http://localhost:8989/health` returns ok. The tool assumes the
   target is already up (health-check + fail-fast; it does not manage the
   process).
3. From the `open-agents` repo root, with LLM creds in env:
   `CERBERUS_BENCH=1 <cerberus>/build/cerberus ... ` via the built bench binary,
   `-n 18`, `-workdir <open-agents>`.
4. Append the resulting table to a new section
   `## Repeatable benchmark — 2026-08-02` in
   `cerberus-docs/technical/dogfood/2026-07-31-m3-3-protocol-infer-dogfood.md`.
5. Write a short Chinese conclusion: which structures pass/fail, whether
   `open-agents` protocol infer currently "passes", whether the hard structures
   (handshake / batch_items_path) are worth further investment, and the
   recommended next step.

## Open questions

None — process management (assume up) and failure-mode reporting (include it)
were resolved during brainstorming. Thresholds and ground truth are fixed by the
brief; if a real run shows a threshold is unreasonable it will be noted in the
benchmark report, not silently re-tuned here.
