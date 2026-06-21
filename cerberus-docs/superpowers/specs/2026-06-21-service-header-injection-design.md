# Service-Level Header Injection — Design Spec

> Date: 2026-06-21
> Status: Draft (awaiting review)
> Origin: modelsite E2E evaluation — L2 auth-boundary run failed because the
> ReAct agent never set the `Host` / `Authorization` headers required by the
> relay gateway (domain-routed + bearer auth).

## 1. Problem

cerberus targets SaaS apps: direct URL + `email`/`password` actors → the rule
engine injects a single `X-Test-User` header. This breaks for **API gateways
that route by `Host` and authenticate by `Authorization: Bearer`**, of which
modelsite's relay gateway is the concrete case.

Measured failure (session `dab03ec5`, L2 auth boundary, 7 cases, 6 fail):

- Rule-engine fast-path emits a bare request → gateway returns `404 unknown
  domain` (tenant resolution fails with no `Host`).
- ReAct `steer()` regenerates the request but **still omits `Host`**; the
  recovery LLM mis-diagnoses the 404 as "wrong port / route not registered /
  service down" across all 3 attempts. No request ever reaches key validation.

Root cause is structural, not a prompt bug: there is **no path to inject
arbitrary per-service or per-actor headers**, and relying on the LLM to honor
header instructions in the goal is unreliable.

## 2. Goals / Non-Goals

**Goals**

- Let `project.yaml` declare headers that are sent on **every** request to a
  service (e.g. `Host`, `X-Internal-Auth`) and per-actor (e.g.
  `Authorization: Bearer ...`).
- Inject at the **execution layer** so both the rule engine and the ReAct loop
  emit the headers deterministically — independent of LLM compliance.
- Keep a per-action override so negative tests (e.g. "wrong Host → expect 404")
  can still mutate headers.

**Non-Goals**

- SSE/streaming consumption (separate limitation: 30s timeout + 1MB cap).
- Generic auth flows (OAuth, session cookies). Bearer + arbitrary headers only.
- Changing the existing `X-Test-User` SaaS behavior (additive only).

## 3. Schema Changes (`internal/project/schema.go`)

```go
type Service struct {
    Name    string            `yaml:"name"`
    URL     string            `yaml:"url"`
    Health  string            `yaml:"health,omitempty"`
    Headers map[string]string `yaml:"headers,omitempty"` // NEW: per-service
}

type CredentialRef struct {
    Email    string            `yaml:"email"`
    Password string            `yaml:"password"`
    Headers  map[string]string `yaml:"headers,omitempty"` // NEW: per-actor
}
```

Rationale: a single `Headers map[string]string` on each is more general than
dedicated `BearerToken`/`APIKey` fields, covers `X-Internal-Auth`, custom
tracing headers, etc., and stays additive (old configs parse unchanged).

`project.yaml` for modelsite becomes:

```yaml
services:
  - name: gateway
    url: "http://localhost:8081"
    health: "/health"
    headers:
      Host: api.opendune.com          # tenant routing
actors:
  - name: valid_user
    credentials:
      headers:
        Authorization: "Bearer sk-relay-..."   # per-actor bearer
  - name: anonymous
    credentials: {}                   # no headers → exercises 401 path
```

## 4. Injection Point (the core decision)

Inject in the **executor**, not at action-generation. Concretely:

- `HTTPExecutor` (http.go) gains a `serviceHeaders map[string]string` keyed by
  the service URL's host (e.g. `localhost:8081`), plus the **current actor's**
  headers resolved at execution time.
- `prepareHTTPRequest` / `doHTTP` merges headers in this priority (later wins):

  1. service headers (matched by request URL host)
  2. actor headers (the actor selected for this case)
  3. action headers (from rule engine or ReAct `steer()`) — **override**, so a
     negative test can set `Host: localhost` or omit `Authorization`.

- Wire `serviceHeaders` through `NewHTTPExecutor` → the agent builder that
  already has the `project.Config`. Actor headers flow via the existing actor
  selection path used by `authHeaders()` (rules.go).

Why execution layer: L2 proved the LLM in `steer()` won't reliably set headers.
Merging at `prepareHTTPRequest` guarantees the request is correct regardless of
who built the `HTTPAction`, while still letting an explicit action header win
for negative cases.

## 5. Affected Files

| File | Change |
|---|---|
| `internal/project/schema.go` | add `Headers` to `Service` + `CredentialRef` |
| `internal/head/agent/http.go` | `HTTPExecutor` holds `serviceHeaders`; `doHTTP` merges |
| `internal/head/agent/http_helpers.go` | `prepareHTTPRequest` accepts merged headers (or a resolver) |
| `internal/head/agent/rules.go` | `authHeaders()` also emits actor `Headers` (not just `X-Test-User`) |
| `internal/head/agent/executor_config.go` + builder | thread `serviceHeaders` from `project.Config` into `HTTPExecutor` |
| `cerberus-docs/configuration/project.md` | document `services[].headers` + `actors[].credentials.headers` |

## 6. Test Strategy (TDD)

1. **Schema parse**: `Service.Headers` / `CredentialRef.Headers` round-trip
   from YAML (table-driven, `schema_test.go`).
2. **Merge unit**: given service+actor+action headers, assert priority and that
   a missing service host yields no injection (`prepareHTTPRequest` test).
3. **Rule engine**: a case against a service with `Host` produces an
   `HTTPAction` whose executed request carries it (httptest server asserts the
   received `Host`).
4. **ReAct path**: seed a `steer()` that returns a header-less `HTTPAction`;
   assert the executor still injects `Host` + `Authorization`.
5. **Override**: action `Host: localhost` overrides service `Host`.

## 7. Risks / Open Questions

- **Host match key**: match service by `host:port` of the request URL. If two
  services share a host (different paths), the first declared wins — document
  this. OPEN: do we need explicit `service:` selection on actions instead?
- **Reflexion bias**: the failed L2 session stored 3 reflections ("404 = port
  problem"). After implementing, clear `cerberus.db` episodic memory before the
  re-validation run, or the agent may re-apply the stale misdiagnosis.
- **Actor selection for ReAct**: confirm the active actor is available at
  `executeAndRecordAction`; if not, carry it on `stepExecution`.
- **Secret leakage**: actor `Authorization` headers must not appear in evidence
  logs / report. Verify redaction in `recordEvidence`.

## 8. Validation Plan

After implementation + `make build`:

1. Reset modelsite reflexion memory.
2. Recreate the test key (`sk-relay-cerberus-test-*`, agent `1829a0f4`) — same
   HMAC construction as the evaluation.
3. Re-run the **L2 goal verbatim**. Success criteria: requests carry
   `Host: api.opendune.com` + `Authorization`, invalid/missing tokens yield
   401, the valid token reaches the model layer (400 `unknown model`), wrong
   `Host` yields 404. Examiner verdicts should reflect real auth behavior.
4. Optionally re-run L3 (billing `/internal/v1/billing/*` with
   `X-Internal-Auth` as a service header).

## 9. Decision Needed

- Approve `Headers map` on both `Service` and `CredentialRef` (vs. dedicated
  `bearer_token` field)? — recommend `Headers map` (general, additive).
- Approve execution-layer injection (vs. ReAct-prompt-only)? — recommend
  execution-layer (L2 proved prompt-only unreliable).
