# HTTP Endpoint Smoke Lazy Fallback — Design

**Date:** 2026-07-29
**Follow-up to:** `2026-07-28-ws-a1-runtime-fallback-design.md` (A1 Phase 2) + `2026-07-29-recovered-rendering-design.md`
**Scope decision (brainstormed):** extend the lazy-fallback mechanism to HTTP endpoints, mirroring the WS relay structure. Non-WS scope = HTTP endpoint smoke (auth-flow fallback was out of scope this round).

## Problem

A1 Phase 2's lazy fallback is **WS-specific**: only WS relay receiver roles (`wsRelayCases`) get a deterministic fallback. When an LLM-authored **HTTP** case fails at execution (the server returns an unexpected status, the body shape differs, a header is wrong), the endpoint it targeted has no fallback — it is simply a fail, even though "the endpoint exists and responds" could be proved by a trivial deterministic GET. The role is stranded for want of a smoke check.

## Goal

Mirror the WS Phase 2 structure for HTTP: when a sound LLM HTTP case targets an endpoint, Scout also emits a **lazy deterministic smoke fallback** for that endpoint — a `GET` asserting **reachable** (any HTTP response received, no transport error/timeout). The Agent activates it only when the LLM HTTP case fails at execution with a non-environmental error; a passing fallback marks the endpoint `recovered`, not `pass`.

## Why this is almost entirely Scout-side

The downstream machinery is already action-agnostic as of the recovered-rendering work:
- Agent activation (`executor_run.go`, `parallel_execute_helpers.go`) triggers on `FallbackFor != ""` + non-environmental `StepFailed`, regardless of action kind.
- `Recovered`/`FallbackFor` data model, `FromResults` pairing, `plannedCaseCount`, consolidate skip rules, and all four renderers (CLI/Markdown/HTML/JUnit) key on `Recovered`/`FallbackFor`, not on WS-ness.

So **#4 is Scout-side (coverage tracking + smoke generator + lazy emission) plus one small Agent-side judgment tweak** (see §5): a deterministic non-5xx check for smoke cases in the rule engine. No store/report/examiner changes.


## Design

### 1. HTTP coverage (mirror of WS `covered`/`coveringCase`)

WS tracks `covered[service][role]` + `coveringCase[service][role]`. HTTP tracks coverage by **endpoint identity** = `(service, path)`:

- When the `test_http_endpoint` branch in `assemblePlan` builds an LLM HTTP case (the central choke point all LLM HTTP cases flow through — not `assembleHTTP` in isolation, which deep-plan/ToT paths may bypass), record that endpoint as covered, carrying the LLM case's ID.
- Thread an `httpCovering map[string]map[string]string` (service → path → covering LLM case ID) through the planning chain to `augmentPlan`, parallel to `coveringCase`.
- **Dedup by `(service, path)`**: one smoke fallback per distinct endpoint the LLM targets, regardless of how many LLM cases hit it (mirrors WS's one-fallback-per-role).

### 2. Deterministic smoke generator (mirror of `wsRelayCases`)

A new `httpSmokeCases(svc, path, covererID)` produces one lazy fallback case:

```go
fb := agent.TestCase{
    ID:          <next smoke id>,
    Name:        fmt.Sprintf("smoke GET %s", path),
    Target:      path,
    Method:      "GET",
    Service:     svc,
    Expectation: "reachable and non-5xx (any 2xx/3xx/4xx response; no transport error/timeout)",
    FallbackFor: covererID,   // bound to the LLM HTTP case that covered this endpoint
    Priority:    -1,          // lazy: skipped unless the primary fails
}
```

`Target`/`Service` match the LLM HTTP case, so the executor resolves the same URL. `Method: "GET"` with no body is already a first-class HTTP action (`HTTPExecutor.doHTTP` defaults to GET; `assembleHTTP` produces GET when method is empty).

### 3. Emission in `augmentPlan` (mirror of `WSCasesCovered`)

A new `HTTPCasesCovered(cfg, httpCovering) []agent.TestCase` emits one smoke fallback per covered endpoint (deduped). `appendExecutorCases` appends it alongside `WSCasesCovered`:

```go
cases = append(cases, WSCasesCovered(s.config, goal, covered, coveringCase)...)
cases = append(cases, HTTPCasesCovered(s.config, httpCovering)...)
```

### 4. "Soundness" gate — intentionally absent for HTTP

WS gates fallback emission on `llmWSFlowSound` because an unsound WS case does not suppress the deterministic relay case (Phase 1 coexistence). HTTP has **no pre-existing deterministic smoke case being suppressed** — the smoke fallback is purely additive. Therefore every LLM HTTP case with a valid path is a coverage candidate; dedup-by-endpoint bounds the count. (If explosion becomes a problem in practice, cap to N most-severe endpoints — deferred.)

### 5. Reachable-and-non-5xx assertion

The smoke case passes iff the HTTP executor returns a response **with status < 500** (2xx/3xx/4xx) — without a transport error or timeout. Rationale:
- A 4xx still proves the endpoint exists and routed the request (the request was client-wrong, not server-broken) — acceptable as "role viable."
- A 5xx means the server itself errored; marking that "recovered" would **mask a real endpoint failure** as a recovery. Excluding 5xx prevents false-recovery.
- A transport error / timeout is an environmental failure; `isEnvironmental` already suppresses fallback activation, so a dead endpoint gets no bogus recovered verdict and the smoke is not run.

This lets the fallback recover a role whose LLM case failed on a wrong assertion (expected `200` + a specific body shape, got `200` + a different shape): the smoke GET confirms the endpoint is up and not erroring. It mirrors the WS fallback's "prove the role viable via a simpler deterministic check."

**Deterministic judgment (the one Agent-side tweak).** The rule engine currently marks an HTTP case `StepPassed` on ANY response (`result.Success()`), without inspecting the status code — leaving status judgment to the Examiner. For a smoke fallback we want the non-5xx check to be deterministic, not LLM-dependent. So in `tryRuleEngine` (or `matchHTTPRules`), when the case is a smoke fallback (`FallbackFor != ""`), judge pass iff the executor returned a response AND its status code < 500. The HTTPExecutor's success result already carries the status code. Non-fallback HTTP cases keep current behavior (pass on any response, Examiner judges status). `FallbackFor` is the marker that selects the stricter smoke rule.

**Execution-routing guarantee.** The smoke case MUST route through the deterministic rule engine (Phase 1), not the ReAct LLM loop. `matchHTTPRules` Rule 1 matches when `Method != "" && Target` starts with `/`. The smoke case has `Method: "GET"` + `Target: <path>`, so it matches Rule 1 and executes via `HTTPExecutor` with zero LLM tokens. The rule engine only falls through to ReAct on transport failure — and the `isEnvironmental` activation gate already prevents the smoke from running against an unreachable endpoint, so a smoke that runs always gets a response and stays off the ReAct path.

**Semantic honesty (weaker than WS).** A WS recovered verdict means the relay's deterministic behavior was verified. An HTTP recovered verdict means only **liveness + non-5xx** — the endpoint is up, routing, and not erroring. It does NOT verify the endpoint's contract (status/body shape). Reports/consumers should read HTTP "recovered" as "endpoint alive, LLM assertion likely wrong," not "endpoint fully verified."

**Limitation: GET on a non-GET endpoint.** A POST-only endpoint's smoke GET returns 405 (or 404). 4xx counts as reachable-and-non-5xx, so the smoke passes — proving only that the service routed something. Acceptable for a liveness signal; document in the report context. If this proves too weak in practice, restrict smoke to endpoints whose LLM case method was already safe/idempotent — deferred.


## Verification

- `HTTPCasesCovered`: given `httpCovering` with one covered endpoint, emits exactly one smoke case with `FallbackFor` set, `Priority<0`, `Method=GET`, matching `Target`; dedupes two coverers of the same `(service, path)` to one fallback; emits nothing for an empty map.
- `assemblePlan` threads `httpCovering` (a 4th return — see Open question) populated from `test_http_endpoint` calls.
- Rule-engine non-5xx judgment: a smoke fallback (`FallbackFor != ""`) that gets a 2xx/3xx/4xx response → `StepPassed`; a 5xx response → `StepFailed` (not recovered); a non-smoke HTTP case keeps pass-on-any-response behavior.
- Integration: an HTTP plan with one LLM case that fails non-environmentally + its smoke fallback → the smoke runs (deterministically, off the ReAct path) and `Recovered=true` (reuses the existing activation/tally/render path).
- `make check` EXIT 0.

## Open question (resolve in plan)

**Threading shape.** `assemblePlan` currently returns `(plan, covered, coveringCase)`. Adding `httpCovering` as a 4th return spreads the arity pain (already felt in Phase 2). Two options for the plan to pick:
- (a) Add a 4th return `httpCovering map[string]map[string]string` and thread it (faithful, but verbose).
- (b) Unify into a single `covering map[coverKey]string` where `coverKey` encodes kind+service+role-or-path, collapsing WS+HTTP into one side table (cleaner long-term, small refactor of the WS path).

**Recommendation: (a) for #4.** #4's goal is low-risk, additive extension. (b) refractors the just-shipped WS threading and raises regression risk for no immediate gain. Reserve (b) for when the coverage model is revisited broadly.

## Out of scope

- **Auth-flow fallback** (retry login with alternate credentials on auth failure) — needs a deterministic auth counterpart; not in this round.
- **Non-HTTP, non-WS actions** (database, exec, code) — no deterministic-counterpart structure; not addressed.
- **Cap on smoke count** (most-severe-N) — deferred unless plan explosion is observed.
- **#3 (examiner-driven re-dispatch loop)** — separate spec.

## Files

- `internal/head/scout/assembly.go` — build `httpCovering` alongside `coveringCase` in `assemblePlan`/`assembleHTTP`.
- `internal/head/scout/direct_planning.go`, `plan_phases.go` — thread `httpCovering` to `augmentPlan`.
- `internal/head/scout/http_cases.go` (new) — `HTTPCasesCovered` + `httpSmokeCases`.
- `internal/head/scout/plan_phases.go` — `appendExecutorCases` appends `HTTPCasesCovered`.
- `internal/head/agent/execute_phases_rule_engine.go` (or `rules_http.go`) — deterministic non-5xx judgment for smoke fallbacks (`FallbackFor != ""`): pass iff response received AND status < 500.
- Tests alongside each.
