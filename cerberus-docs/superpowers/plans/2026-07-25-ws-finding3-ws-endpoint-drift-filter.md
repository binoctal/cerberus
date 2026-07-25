# Finding-3: Filter WS-Endpoint HTTP Drift Cases — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Drop LLM free-form cases that target a WS endpoint with a non-`ws_*` action (the HTTP drift that produces 426 noise); keep legitimate HTTP REST exploration, deterministic WS cases, and LLM `ws_*` attempts on WS endpoints.

**Architecture:** `augmentPlan` calls a new `filterWSEndpointDrift(plan, cfg)` at the end; it builds the set of WS endpoint paths (from services that declare a protocol) and drops cases whose target path matches one AND whose action is not a `ws_*` action. No-op when no service declares a protocol. Scout-only; no executor/Steer change.

**Tech Stack:** Go 1.25, `internal/head/scout` (`plan_phases.go`), `net/url`, testify.

## Global Constraints

- Go 1.25, pure-Go (no CGo); module `github.com/binoctal/cerberus`
- `coder/websocket` v1.8.14 only (scout-only change, no WS lib touch)
- No expression/evaluator deps
- Commit author MUST be `binoctal <binoctal@gmail.com>`, NO Co-Authored-By trailer
- Code comments + commit messages in English
- Docs only in `cerberus-docs/`
- `make check` (fmt + lint + test -race) must be EXIT 0
- Follow existing comment density/naming idiom

---

### Task 1: `filterWSEndpointDrift` mechanism — helpers + filter + wiring

**Files:**
- Modify: `internal/head/scout/plan_phases.go` — add `net/url` import; call `filterWSEndpointDrift(plan, s.config)` at the end of `augmentPlan`; add `filterWSEndpointDrift`, `urlPathOf`, `isWSAction`
- Test (new): `internal/head/scout/ws_endpoint_drift_test.go`

**Interfaces:**
- Consumes: `agent.TestPlan` (has `Cases []agent.TestCase`), `agent.TestCase` (has `Target`, `Action` string fields), `project.Config` (has `Services []project.Service`; `Service` has `URL` string and `Protocol *project.Protocol`).
- Produces: `filterWSEndpointDrift(plan *agent.TestPlan, cfg *project.Config)`, `urlPathOf(target string) string`, `isWSAction(action string) bool`.

- [ ] **Step 1: Write the failing tests (RED)**

Create `internal/head/scout/ws_endpoint_drift_test.go`:

```go
package scout

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

func wsSvc(url string) project.Service {
	return project.Service{Name: "rt", URL: url, Protocol: &project.Protocol{TypePath: "type"}}
}

func caseIDs(cases []agent.TestCase) []string {
	ids := make([]string, 0, len(cases))
	for _, c := range cases {
		ids = append(ids, c.ID)
	}
	return ids
}

// Finding-3: a non-ws_* action on a WS endpoint is HTTP drift (426) → dropped.
// HTTP REST exploration (different path) and ws_* attempts on the WS endpoint
// are kept. Deterministic WS cases (ws_* action) are kept.
func TestFilterWSEndpointDrift(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{wsSvc("http://localhost:8989/ws/user_x")}}
	plan := &agent.TestPlan{Cases: []agent.TestCase{
		{ID: "drift", Target: "/ws/user_x", Action: "api_request"},        // WS endpoint + HTTP → drop
		{ID: "rest", Target: "/api/v1/health", Action: "api_request"},     // HTTP REST → keep
		{ID: "ws-conn", Target: "/ws/user_x", Action: "ws_connect"},       // ws_* on WS → keep
		{ID: "ws-flow", Target: "/ws/user_x", Action: "ws_flow", Steps: []agent.TestStep{{}}}, // deterministic → keep
	}}
	filterWSEndpointDrift(plan, cfg)
	assert.Equal(t, []string{"rest", "ws-conn", "ws-flow"}, caseIDs(plan.Cases),
		"only the WS-endpoint HTTP-drift case is dropped")
}

// No service declares a protocol → filter is a byte-identical no-op.
func TestFilterWSEndpointDrift_NoProtocolIsNoop(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{Name: "api", URL: "http://h/api"}}}
	before := []agent.TestCase{
		{ID: "a", Target: "/api", Action: "api_request"},
		{ID: "b", Target: "/ws/x", Action: "api_request"}, // no protocol declared → not a WS endpoint
	}
	plan := &agent.TestPlan{Cases: append([]agent.TestCase{}, before...)}
	filterWSEndpointDrift(plan, cfg)
	require.Len(t, plan.Cases, len(before), "no protocol → nothing dropped")
	assert.Equal(t, []string{"a", "b"}, caseIDs(plan.Cases))
}

// An empty or unparseable target never matches a WS path → kept (no false drop).
func TestFilterWSEndpointDrift_EmptyTargetKept(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{wsSvc("http://h/ws/u")}}
	plan := &agent.TestPlan{Cases: []agent.TestCase{
		{ID: "empty", Target: "", Action: "api_request"},
	}}
	filterWSEndpointDrift(plan, cfg)
	assert.Equal(t, []string{"empty"}, caseIDs(plan.Cases))
}

func TestUrlPathOf(t *testing.T) {
	assert.Equal(t, "/ws/user_x", urlPathOf("http://localhost:8989/ws/user_x?token=t"))
	assert.Equal(t, "/ws/user_x", urlPathOf("/ws/user_x"))
	assert.Equal(t, "/ws/user_x", urlPathOf("ws://localhost:8989/ws/user_x"))
	assert.Equal(t, "", urlPathOf(""))                        // empty → no path
	assert.Equal(t, "example.com", urlPathOf("example.com"))  // bare host (no scheme/colon) → path-only parse (won't match a WS path)
	assert.Equal(t, "", urlPathOf("ht tp://x"))               // unparseable (space in URL) → ""
}

func TestIsWSAction(t *testing.T) {
	for _, a := range []string{"ws_connect", "ws_send", "ws_receive", "ws_disconnect", "ws_flow"} {
		assert.True(t, isWSAction(a), "%s is a WS action", a)
	}
	for _, a := range []string{"api_request", "process", "", "http_request"} {
		assert.False(t, isWSAction(a), "%q is not a WS action", a)
	}
}
```

- [ ] **Step 2: Run tests to verify RED**

Run: `go test ./internal/head/scout/ -run 'TestFilterWSEndpointDrift|TestUrlPathOf|TestIsWSAction' -v`
Expected: COMPILE ERROR — `undefined: filterWSEndpointDrift` / `urlPathOf` / `isWSAction`.

- [ ] **Step 3: Add the `net/url` import**

In `internal/head/scout/plan_phases.go`, add `"net/url"` to the import block. The import block becomes:

```go
import (
	"context"
	"net/url"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)
```

- [ ] **Step 4: Wire the call into `augmentPlan`**

In `augmentPlan` (around line 54-60), add the filter call as the last statement:

```go
func (s *Scout) augmentPlan(plan *agent.TestPlan, goal string) {
	covered := expandWSRelayCases(s.config, plan)
	if len(covered) > 0 {
		s.logger.Info("expanded ws_relay cases", zap.Int("covered_services", len(covered)))
	}
	s.appendExecutorCases(plan, goal, covered)
	filterWSEndpointDrift(plan, s.config) // Finding-3: drop WS-endpoint HTTP drift
}
```

- [ ] **Step 5: Add `filterWSEndpointDrift`, `urlPathOf`, `isWSAction`**

Append at the end of `internal/head/scout/plan_phases.go`:

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

// urlPathOf returns the path component of a target, which may be an absolute
// URL (http://h/ws/x), a relative path (/ws/x), or a ws:// URL. Returns "" on a
// parse failure or empty target so it never matches a WS path.
func urlPathOf(target string) string {
	if u, err := url.Parse(target); err == nil {
		return u.Path
	}
	return ""
}

// isWSAction reports whether action is one of the WS executor actions. The set
// is fixed by the coder/websocket executor.
func isWSAction(action string) bool {
	switch action {
	case "ws_connect", "ws_send", "ws_receive", "ws_disconnect", "ws_flow":
		return true
	}
	return false
}
```

- [ ] **Step 6: Run the targeted tests to verify GREEN**

Run: `go test ./internal/head/scout/ -run 'TestFilterWSEndpointDrift|TestUrlPathOf|TestIsWSAction' -v`
Expected: PASS (all four tests).

- [ ] **Step 7: Regression — full scout package**

Run: `go test ./internal/head/scout/`
Expected: PASS. Non-WS configs hit the no-op branch; WS configs keep deterministic `ws_*` cases.

- [ ] **Step 8: make check**

Run: `make check`
Expected: EXIT 0.

- [ ] **Step 9: Commit**

```bash
git add internal/head/scout/plan_phases.go internal/head/scout/ws_endpoint_drift_test.go
git commit -m "feat(ws): filter WS-endpoint HTTP drift cases

filterWSEndpointDrift (called from augmentPlan) drops LLM free-form cases that
target a WS endpoint with a non-ws_* action — the HTTP drift that hits 426
noise. Exact WS service URL path match; legitimate HTTP REST exploration,
deterministic WS cases, and LLM ws_* attempts on WS endpoints are kept. No-op
when no service declares a protocol. Scout-only."
```

---

### Task 2: Document the drift filter + optional live dogfood re-run

**Files:**
- Modify: `cerberus-docs/executors/websocket.md` — the Scout WS-cases note
- Manual (optional): a live `cerberus run` against open-agents — not part of `make check`

**Interfaces:** none.

- [ ] **Step 1: Add a one-line note to the Scout WS-cases section**

In `cerberus-docs/executors/websocket.md`, find the Scout-generated-cases / deterministic-relay note (the subsection that describes Scout emitting WS cases). Append one sentence to that note:

```
Free-form cases that target a WS endpoint with a non-ws_* action (HTTP drift, which the WS endpoint rejects with 426) are dropped; legitimate HTTP REST exploration of other paths, deterministic WS cases, and ws_* attempts on the WS endpoint are kept.
```

- [ ] **Step 2: (Optional) Live dogfood re-run — confirm tc-001 drift gone**

With open-agents on `:8989` and a built binary, re-run the dogfood `cerberus run` (same config/goal as 2026-07-24). Expected: the WS-endpoint HTTP-drift case (previously `tc-001` targeting `/ws/<userId>` with `api_request`) is no longer in the generated set; the REST-exploration cases (`/health`, `/api/v1/...`) still are. (Manual confirmation, not a `make check` step — skip if open-agents is not running.)

- [ ] **Step 3: Commit the doc**

```bash
git add cerberus-docs/executors/websocket.md
git commit -m "docs(ws): note WS-endpoint HTTP drift filtering"
```
