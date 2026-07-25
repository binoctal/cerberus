# WS Finding-3 — Filter WS-Endpoint HTTP Drift Cases — Design — 2026-07-25

## Background

The 2026-07-24 live execution dogfood
(`cerberus-docs/technical/dogfood/2026-07-24-ws-relay-live-execution-dogfood.md`)
showed Scout's LLM free-form cases (tc-*) drift. Scout's `Plan` pipeline: Phase
2 LLM planning emits tc-* cases; `augmentPlan` then appends deterministic WS
cases (`WSCasesCovered`: 3C relay, `wsStepsCase` exchange, connect). The
deterministic WS cases run reliably (runSteps, PASS attempts:1), but the LLM
tc-* cases drift.

Closer inspection of the dogfood tc-* targets reveals the drift is narrower
than "all WS-service tc-*": the LLM explores the service's whole API surface —
tc-001 targets the WS endpoint (`/ws/user_...`) with an HTTP action
(`api_request` → 426, the WS endpoint refusing HTTP upgrade), while tc-002..009
target HTTP REST endpoints (`/health`, `/api/v1/...`) as legitimate HTTP
exploration. Filtering ALL WS-service tc-* would wrongly drop that legitimate
REST exploration. The actual drift is: **HTTP/free-form action on a WS
endpoint**.

The deterministic WS cases already cover the protocol-derivable WS scenarios.
The LLM tc-* HTTP drift on a WS endpoint is pure 426 noise. This design filters
it — without touching the executor, Steer, or the legitimate HTTP exploration.

## Goal

Drop LLM free-form cases that target a WS endpoint with a non-`ws_*` action
(the HTTP drift that produces 426 noise). Keep legitimate HTTP REST
exploration, deterministic WS cases, and LLM `ws_*` attempts on WS endpoints.
No executor/Steer change, no new deps.

## Design

Scope: `internal/head/scout/plan_phases.go` (+ two small helpers) only.

### Mechanism

`augmentPlan`, after `expandWSRelayCases` + `appendExecutorCases`, filters
`plan.Cases`: drop any case whose target path matches a WS service's URL path
AND whose action is not a `ws_*` action.

```go
func (s *Scout) augmentPlan(plan *agent.TestPlan, goal string) {
    covered := expandWSRelayCases(s.config, plan)
    if len(covered) > 0 {
        s.logger.Info("expanded ws_relay cases", zap.Int("covered_services", len(covered)))
    }
    s.appendExecutorCases(plan, goal, covered)
    filterWSEndpointDrift(plan, s.config) // Finding-3
}
```

`filterWSEndpointDrift` builds the set of WS endpoint paths from services that
declare a protocol, then drops drift cases into a fresh slice:

```go
// filterWSEndpointDrift drops LLM free-form cases that target a WS endpoint
// with a non-ws_* action — the HTTP drift that produces 426 noise. Legitimate
// HTTP REST exploration (a different path), deterministic WS cases (a ws_*
// action), and LLM ws_* attempts on a WS endpoint are all kept. No-op when no
// service declares a protocol (byte-identical for non-WS projects).
func filterWSEndpointDrift(plan *agent.TestPlan, cfg *project.Config) {
    wsPaths := map[string]bool{}
    for _, svc := range cfg.Services {
        if svc.Protocol == nil {
            continue
        }
        if u, err := url.Parse(svc.URL); err == nil && u.Path != "" {
            wsPaths[u.Path] = true
        }
    }
    if len(wsPaths) == 0 {
        return
    }
    kept := make([]agent.TestCase, 0, len(plan.Cases))
    for _, c := range plan.Cases {
        if wsPaths[urlPathOf(c.Target)] && !isWSAction(c.Action) {
            continue
        }
        kept = append(kept, c)
    }
    plan.Cases = kept
}
```

### Helpers

- `urlPathOf(target string) string`: `url.Parse(target).Path` — the path of an
  absolute URL, a relative path, or a bare path; `""` on parse failure. A
  relative path like `/ws/user_x` parses to Path `/ws/user_x`.
- `isWSAction(action string) bool`: `action` ∈
  `{ws_connect, ws_send, ws_receive, ws_disconnect, ws_flow}` — the WS executor
  actions. Hardcoded set; the WS action vocabulary is fixed by the
  coder/websocket executor (deriving it from the executor registry is YAGNI).

## Assumptions & Edge Cases

- **Path match is EXACT** (`casePath == wsServicePath`). The dogfood tc-001
  target equals the WS service URL path exactly. LLM variants (e.g. `/ws`
  without the userId, or a different userId path) would NOT match and would
  slip through — a narrower LLM drift, future precision work, out of scope.
  Exact match avoids the false-positive risk of a `/ws/` prefix match catching
  unrelated paths.
- **No WS service** → `wsPaths` empty → filter is a no-op → byte-identical to
  today (all non-WS projects).
- **Deterministic WS cases** (`WSCasesCovered`: relay/exchange/connect) have
  target=svc.URL (a WS path) but action ∈ `ws_*` → KEPT.
- **Legitimate HTTP exploration** (tc-002..009: `/health`, `/api/...`) → path ≠
  WS path → KEPT.
- **LLM `ws_*` cases on a WS endpoint** → action ∈ `ws_*` → KEPT (these route
  through Steer; their reliability is Finding-2's single-conn `ws_*` concern,
  NOT this filter's scope).
- **case.Target empty or unparseable** → path `""` → never matches a WS path →
  KEPT (no false drop).
- **Multiple WS services** → `wsPaths` accumulates each path; a case matching
  any is filtered by action.

## Out of Scope

- Finding-2's single-conn `ws_*` Steer reliability (separate).
- LLM `ws_*` case quality on WS endpoints (Steer dispatch, separate).
- Path-variant drift (LLM emitting a non-identical WS endpoint path) — future
  precision.
- type-transform relays (`session:start`↔`session:created`) — A1/capability-
  model gap, separate.

## Testing

- Unit `filterWSEndpointDrift`: (a) WS-endpoint target + `api_request` action →
  dropped; (b) WS-endpoint + `ws_connect` → kept; (c) HTTP path + `api_request`
  → kept; (d) no-protocol config → byte-identical (no drop); (e) deterministic
  `ws_flow`/`ws_connect` case on a WS path → kept; (f) empty target → kept.
- Unit `urlPathOf`: absolute URL, relative path, bare host, unparseable.
- Regression: existing scout tests unchanged (no-WS and WS configs both);
  `WSCasesCovered` cases survive.
- (Optional, live) re-run the dogfood: tc-001 (WS-endpoint drift) absent;
  tc-002..009 (REST exploration) still present.

## Constraints

Go 1.25 pure-Go; `coder/websocket` v1.8.14 only (scout-only change, no WS lib
touch); no expression/evaluator deps; author `binoctal <binoctal@gmail.com>`,
no Co-Authored-By; English comments/commits; docs only in `cerberus-docs/`;
`make check` green.
