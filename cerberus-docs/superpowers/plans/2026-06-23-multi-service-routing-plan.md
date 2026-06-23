# Multi-Service Routing Implementation Plan (Block ①)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make cerberus execute-layer route each test case to its target service instead of only `Services[0]`.

**Architecture:** `RuleEngine` swaps its single `baseURL` for a `services` map and selects URL + headers + actor by `tc.Service` (falling back to `Services[0]` for backward compat). Scout fills `tc.Service` via per-service `PathPrefix` plus an LLM verification pass.

**Tech Stack:** Go 1.25, `gopkg.in/yaml.v3`, `net/http/httptest`, `github.com/stretchr/testify`.

## Global Constraints

- Go 1.25, module `github.com/binoctal/cerberus`, no CGo.
- Commit author `binoctal <binoctal@gmail.com>`, no `Co-Authored-By`.
- Comments and commit messages in English.
- All docs under `cerberus-docs/`.

## File Structure

- Modify `internal/project/schema.go` — `Service.PathPrefix`, `Actor.Service`
- Modify `internal/project/model.go` — `EndpointDef.Service`
- Modify `internal/head/agent/types.go` — `TestCase.Service`
- Modify `internal/head/agent/rules.go` — `RuleEngine` services map, `NewRuleEngine` signature, per-service actor
- Modify `internal/head/agent/rules_http.go` — select base URL by `tc.Service`
- Modify `internal/head/agent/react_loop_helpers.go` — `withBaseURL` by `tc.Service`
- Modify `internal/head/agent/execute_phases_react_loop.go`, `execute_phases_recovery.go` — pass `tc` to normalization
- Modify `internal/session/run_phases_agent.go`, `resume_phases_run.go` — pass `Services` to `NewRuleEngine`
- Modify `internal/head/scout/direct_planning.go` — per-service attribution + LLM verify

---

### Task 1: Add service-attribution fields to data models

**Files:**
- Modify: `internal/project/schema.go:17-34`
- Modify: `internal/project/model.go:28-32`
- Modify: `internal/head/agent/types.go:22-35`
- Test: `internal/project/schema_test.go`, `internal/head/agent/types_test.go` (create if absent)

**Interfaces:**
- Produces: `project.Service.PathPrefix []string`, `project.Actor.Service string`, `project.EndpointDef.Service string`, `agent.TestCase.Service string`. Empty values everywhere mean "unattributed" → callers fall back to `Services[0]`.

- [ ] **Step 1: Write failing test (yaml round-trip for Service.PathPrefix + Actor.Service)**

Add to `internal/project/schema_test.go`:

```go
func TestServiceActorServiceFieldsRoundTrip(t *testing.T) {
	src := `
services:
  - name: gateway
    url: "http://localhost:8081"
    path_prefix: ["/v1", "/v1/models"]
actors:
  - name: gw-user
    service: gateway
    credentials:
      email: "u@x"
`
	var cfg project.Config
	err := yaml.Unmarshal([]byte(src), &cfg)
	require.NoError(t, err)
	require.Equal(t, []string{"/v1", "/v1/models"}, cfg.Services[0].PathPrefix)
	require.Equal(t, "gateway", cfg.Actors[0].Service)
}
```

(Add `import "gopkg.in/yaml.v3"` and `"github.com/stretchr/testify/require"` if missing.)

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./internal/project/ -run TestServiceActorServiceFieldsRoundTrip -v`
Expected: FAIL — fields do not exist / don't parse.

- [ ] **Step 3: Add fields to `Service` and `Actor`**

In `internal/project/schema.go`:

```go
type Service struct {
	Name       string            `yaml:"name"`
	URL        string            `yaml:"url"`
	Health     string            `yaml:"health,omitempty"`
	Headers    map[string]string `yaml:"headers,omitempty"`
	PathPrefix []string          `yaml:"path_prefix,omitempty"`
}

type Actor struct {
	Name        string        `yaml:"name"`
	Credentials CredentialRef `yaml:"credentials"`
	Entry       string        `yaml:"entry,omitempty"`
	Service     string        `yaml:"service,omitempty"`
}
```

Add `Service string \`json:"service,omitempty"\`` to `EndpointDef` in `internal/project/model.go` and to `TestCase` in `internal/head/agent/types.go`.

- [ ] **Step 4: Run test, verify it passes**

Run: `go test ./internal/project/ -run TestServiceActorServiceFieldsRoundTrip -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/project/schema.go internal/project/model.go internal/head/agent/types.go internal/project/schema_test.go
git commit -m "feat(project): add Service/PathPrefix attribution fields"
```

---

### Task 2: RuleEngine routes by tc.Service (base URL + per-service actor)

**Files:**
- Modify: `internal/head/agent/rules.go:12-28, 91-107`
- Modify: `internal/head/agent/rules_http.go:10-21`
- Test: `internal/head/agent/rules_test.go`

**Interfaces:**
- Consumes: `project.Service` (with `.URL`, `.Headers`), `project.Actor` (with `.Service`).
- Produces: `NewRuleEngine(services []project.Service, actors []project.Actor, workDir string) *RuleEngine`; `Match(tc)` selects URL/headers/actor by `tc.Service`.

- [ ] **Step 1: Write failing test — case routes to its own service URL**

Add to `internal/head/agent/rules_test.go`:

```go
func TestRuleEngine_RoutesByService(t *testing.T) {
	services := []project.Service{
		{Name: "gateway", URL: "http://gw:8081"},
		{Name: "admin", URL: "http://admin:8086"},
	}
	engine := NewRuleEngine(services, nil, ".")

	action, ok := engine.Match(TestCase{
		Target: "/v1/chat", Method: "POST", Service: "admin",
	})
	require.True(t, ok)
	httpAct, ok := action.(types.HTTPAction)
	require.True(t, ok)
	assert.Equal(t, "http://admin:8086/v1/chat", httpAct.URL)
}

func TestRuleEngine_FallsBackToFirstService(t *testing.T) {
	services := []project.Service{
		{Name: "gateway", URL: "http://gw:8081"},
		{Name: "admin", URL: "http://admin:8086"},
	}
	engine := NewRuleEngine(services, nil, ".")

	action, ok := engine.Match(TestCase{Target: "/v1/chat", Method: "POST"}) // Service empty
	require.True(t, ok)
	assert.Equal(t, "http://gw:8081/v1/chat", action.(types.HTTPAction).URL)
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/head/agent/ -run TestRuleEngine_RoutesByService -v`
Expected: FAIL — `NewRuleEngine` signature mismatch / always uses single baseURL.

- [ ] **Step 3: Rewrite `RuleEngine` to hold services + select per case**

In `internal/head/agent/rules.go`:

```go
type RuleEngine struct {
	services []project.Service
	byName   map[string]project.Service
	actors   []project.Actor
	workDir  string
	hits     atomic.Int64
	misses   atomic.Int64
}

func NewRuleEngine(services []project.Service, actors []project.Actor, workDir string) *RuleEngine {
	byName := make(map[string]project.Service, len(services))
	for _, s := range services {
		byName[s.Name] = s
	}
	return &RuleEngine{services: services, byName: byName, actors: actors, workDir: workDir}
}

// baseURLFor returns the URL for tc.Service, falling back to the first
// configured service (backward compatible with single-service projects).
func (r *RuleEngine) baseURLFor(tc TestCase) string {
	if tc.Service != "" {
		if s, ok := r.byName[tc.Service]; ok {
			return strings.TrimRight(s.URL, "/")
		}
	}
	if len(r.services) > 0 {
		return strings.TrimRight(r.services[0].URL, "/")
	}
	return ""
}

// serviceHeaders returns service-level headers for tc.Service (nil if none).
func (r *RuleEngine) serviceHeaders(tc TestCase) map[string]string {
	if tc.Service != "" {
		if s, ok := r.byName[tc.Service]; ok && len(s.Headers) > 0 {
			return s.Headers
		}
	}
	if len(r.services) > 0 && len(r.services[0].Headers) > 0 {
		return r.services[0].Headers
	}
	return nil
}
```

- [ ] **Step 4: Update `matchHTTPRules` + `authHeaders` to use per-case service**

In `internal/head/agent/rules_http.go`, change Rule 1 to use the per-case base + headers:

```go
func (r *RuleEngine) matchHTTPRules(tc TestCase) (types.TypedAction, bool) {
	base := r.baseURLFor(tc)
	if tc.Method != "" && strings.HasPrefix(tc.Target, "/") {
		action := types.HTTPAction{
			Method: strings.ToUpper(tc.Method),
			URL:    base + tc.Target,
		}
		if h := r.serviceHeaders(tc); h != nil {
			action.Headers = h
		}
		if auth := r.authHeadersFor(tc); auth != nil {
			if action.Headers == nil {
				action.Headers = map[string]string{}
			}
			for k, v := range auth {
				action.Headers[k] = v
			}
		}
		return action, true
	}
	// Rule 2 (navigate) + Rule 3 (full URL) — replace r.baseURL with base likewise.
	_ = base
	return nil, false
}
```

In `rules.go`, replace `authHeaders()` with:

```go
// authHeadersFor returns auth headers for tc.Service's actor, falling back to
// a global actor (Actor.Service == "") then actors[0].
func (r *RuleEngine) authHeadersFor(tc TestCase) map[string]string {
	if len(r.actors) == 0 {
		return nil
	}
	var actor project.Actor
	found := false
	if tc.Service != "" {
		for _, a := range r.actors {
			if a.Service == tc.Service {
				actor, found = a, true
				break
			}
		}
	}
	if !found {
		for _, a := range r.actors {
			if a.Service == "" {
				actor, found = a, true
				break
			}
		}
	}
	if !found {
		actor = r.actors[0]
	}
	h := map[string]string{}
	if actor.Credentials.Email != "" {
		h["X-Test-User"] = actor.Credentials.Email
	}
	for k, v := range actor.Credentials.Headers {
		h[k] = v
	}
	if len(h) == 0 {
		return nil
	}
	return h
}
```

- [ ] **Step 5: Run tests, verify they pass**

Run: `go test ./internal/head/agent/ -run "TestRuleEngine_" -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/head/agent/rules.go internal/head/agent/rules_http.go internal/head/agent/rules_test.go
git commit -m "feat(agent): route rule-engine actions by tc.Service"
```

---

### Task 3: `withBaseURL` resolves by tc.Service

**Files:**
- Modify: `internal/head/agent/react_loop_helpers.go:68-105`
- Modify: `internal/head/agent/execute_phases_react_loop.go:31-34`
- Modify: `internal/head/agent/execute_phases_recovery.go:42-43`
- Test: `internal/head/agent/url_resolve_test.go`

**Interfaces:**
- Produces: `withBaseURL(tc TestCase, action TypedAction) TypedAction`.

- [ ] **Step 1: Write failing test — relative URL resolves against tc.Service's URL**

Add to `internal/head/agent/url_resolve_test.go`:

```go
func TestWithBaseURL_ResolvesByService(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	steerJSON, _ := json.Marshal(makeSteerEnvelope("hit", "GET", "/api/data"))
	loop, s := testLoopWithServices(t, map[string]string{"default": string(steerJSON)},
		[]project.Service{{Name: "primary", URL: server.URL}}, nil)
	sessionID := createTestSession(t, s)

	plan := &TestPlan{Goal: "g", Cases: []TestCase{
		{ID: "t1", Target: "verify", Service: "primary", Expectation: "ok"},
	})}
	results, err := loop.ExecutePlan(context.Background(), plan, sessionID)
	require.NoError(t, err)
	require.Equal(t, StepPassed, results[0].Status)
}
```

(`testLoopWithServices` is added in Task 4; for Task 3, implement it locally first or add a helper here. To keep tasks decoupled, add a minimal `testLoopWithServices` in `helpers_test.go` now.)

- [ ] **Step 2: Run, verify it fails**

Run: `go test ./internal/head/agent/ -run TestWithBaseURL_ResolvesByService -v`
Expected: FAIL — `withBaseURL` ignores `tc.Service` / helper missing.

- [ ] **Step 3: Change `withBaseURL` to take `tc`**

In `react_loop_helpers.go`:

```go
func (r *ReActLoop) withBaseURL(tc TestCase, action types.TypedAction) types.TypedAction {
	base := ""
	if r.engine != nil {
		base = r.engine.baseURLFor(tc)
	}
	if base == "" {
		return action
	}
	switch a := action.(type) {
	case types.HTTPAction:
		a.URL = resolveActionURL(base, a.URL)
		return a
	case types.NavigateAction:
		a.URL = resolveActionURL(base, a.URL)
		return a
	}
	return action
}
```

Update callers — in `execute_phases_react_loop.go`:

```go
newResult := executeAndRecordAction(r, se.ctx, *se.tc, action, se.traceID)
```

In `execute_phases_recovery.go`:

```go
action := r.withBaseURL(*se.tc, recResult.Action)
```

And `executeAndRecordAction`:

```go
func executeAndRecordAction(r *ReActLoop, ctx context.Context, tc TestCase, action types.TypedAction, traceID int64) types.ExecutorResult {
	action = r.withActorHeaders(tc, action)
	action = r.withBaseURL(tc, action)
	result := r.executor.Execute(ctx, action)
	r.recordEvidence(ctx, traceID, "steer_attempt", action, result)
	return result
}
```

(Also update `withActorHeaders` to take `tc` if it needs the service; it currently keys off `r.engine.actors[0]` — leave as-is for now, per-service actor injection is handled by the rule-engine path. Add a `// TODO: per-service actor for ReAct path` note.)

- [ ] **Step 4: Run, verify it passes**

Run: `go test ./internal/head/agent/ -run "TestWithBaseURL_|TestReActLoop_RelativeURL|TestReActLoop_RecoveryRelativeURL" -v`
Expected: PASS (existing URL tests still pass via Services[0] fallback).

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/react_loop_helpers.go internal/head/agent/execute_phases_react_loop.go internal/head/agent/execute_phases_recovery.go internal/head/agent/url_resolve_test.go internal/head/agent/helpers_test.go
git commit -m "feat(agent): resolve ReAct URLs by tc.Service"
```

---

### Task 4: Session wiring passes services to RuleEngine

**Files:**
- Modify: `internal/session/run_phases_agent.go:25`
- Modify: `internal/session/resume_phases_run.go:25`
- Test: `internal/head/agent/helpers_test.go` (add `testLoopWithServices`), `internal/session/*_test.go` if any exercises this path.

**Interfaces:**
- Produces: the two call sites use `agent.NewRuleEngine(rp.session.Config.Services, rp.session.Config.Actors, projectDir)`.

- [ ] **Step 1: Write failing test — `testLoopWithServices` helper**

Add to `internal/head/agent/helpers_test.go`:

```go
func testLoopWithServices(t *testing.T, responses map[string]string, services []project.Service, actors []project.Actor) (*ReActLoop, *store.Store) {
	t.Helper()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	require.NoError(t, store.RunMigrations(context.Background(), s.DB(), "../../../migrations"))
	driver := ai.NewDriver(llm.NewMockClient(responses), ai.NewTokenBudget(200000, 10000))
	engine := NewRuleEngine(services, actors, ".")
	executor := BuildMultiExecutor(".", nil, nil, zap.NewNop())
	loop := NewReActLoopWithConfig(ReActLoopConfig{
		Driver: driver, Store: s, Engine: engine, Executor: executor,
		Config: DefaultReActConfig(), Logger: zap.NewNop(),
		Embedder: embed.NewTrigramProvider(embed.DefaultDimension),
	})
	return loop, s
}
```

- [ ] **Step 2: Update the two session call sites**

In `internal/session/run_phases_agent.go:25` and `resume_phases_run.go:25`:

```go
engine := agent.NewRuleEngine(rp.session.Config.Services, rp.session.Config.Actors, projectDir)
```

(`baseURL` local variable can be removed if now unused.)

- [ ] **Step 3: Run full build + agent tests**

Run: `go build ./... && go test ./internal/head/agent/ ./internal/session/`
Expected: build OK, all tests PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/session/run_phases_agent.go internal/session/resume_phases_run.go internal/head/agent/helpers_test.go
git commit -m "feat(session): pass Services to RuleEngine"
```

---

### Task 5: Scout attributes endpoints to services by PathPrefix

**Files:**
- Modify: `internal/head/scout/direct_planning.go:150-160`
- Test: `internal/head/scout/direct_planning_test.go` (add)

**Interfaces:**
- Produces: each generated `TestCase.Service` is set when the endpoint path matches a service's `PathPrefix`; otherwise left empty (→ `Services[0]` fallback).

- [ ] **Step 1: Write failing test — case gets Service from prefix**

```go
func TestPlan_AttributesByPathPrefix(t *testing.T) {
	services := []project.Service{
		{Name: "gateway", URL: "http://gw", PathPrefix: []string{"/v1"}},
		{Name: "admin", URL: "http://admin", PathPrefix: []string{"/api/admin"}},
	}
	cases := planWithPrefix(t, services, []string{"POST /v1/chat/completions", "GET /api/admin/users"})
	require.Equal(t, "gateway", serviceOf(cases, "/v1/chat/completions"))
	require.Equal(t, "admin", serviceOf(cases, "/api/admin/users"))
}
```

(`planWithPrefix` / `serviceOf` are small helpers in the test file — extract prefix-matching into a pure function `attributeService(path string, services []project.Service) string` so it's testable in isolation.)

- [ ] **Step 2: Run, verify it fails**

Run: `go test ./internal/head/scout/ -run TestPlan_AttributesByPathPrefix -v`
Expected: FAIL.

- [ ] **Step 3: Implement `attributeService` + wire into planning**

In `direct_planning.go`:

```go
// attributeService returns the service whose PathPrefix contains the given
// endpoint path, or "" if none match (caller falls back to Services[0]).
func attributeService(path string, services []project.Service) string {
	for _, s := range services {
		for _, p := range s.PathPrefix {
			if strings.HasPrefix(path, p) {
				return s.Name
			}
		}
	}
	return ""
}
```

When building each `TestCase` from an endpoint, set `tc.Service = attributeService(ep.Path, s.config.Services)`.

- [ ] **Step 4: Run, verify it passes**

Run: `go test ./internal/head/scout/ -run TestPlan_AttributesByPathPrefix -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/head/scout/direct_planning.go internal/head/scout/direct_planning_test.go
git commit -m "feat(scout): attribute endpoints to services by path prefix"
```

---

### Task 6: LLM verification layer for tc.Service

**Files:**
- Modify: `internal/head/scout/direct_planning.go` (after attribution)
- Test: `internal/head/scout/direct_planning_test.go` (add)

**Interfaces:**
- Produces: `verifyServiceAttribution(driver *ai.Driver, cases []agent.TestCase, services []project.Service) []agent.TestCase` — corrects misattributed `Service` using the LLM; uncertain ones keep prefix attribution + a log.

- [ ] **Step 1: Write failing test — LLM corrects a misattributed service**

```go
func TestVerifyServiceAttribution_CorrectsMisattribution(t *testing.T) {
	services := []project.Service{
		{Name: "gateway", URL: "http://gw", PathPrefix: []string{"/v1"}},
		{Name: "admin", URL: "http://admin", PathPrefix: []string{"/api/admin"}},
	}
	// prefix would give "gateway" but LLM says this /v1 path is actually admin's concern
	verifyJSON := `{"corrections":[{"path":"/v1/admin/users","service":"admin"}]}`
	driver := ai.NewDriver(llm.NewMockClient(map[string]string{"default": verifyJSON}), ai.NewTokenBudget(200000, 10000))
	cases := []agent.TestCase{{Target: "/v1/admin/users", Service: "gateway"}}
	out := verifyServiceAttribution(driver, cases, services)
	assert.Equal(t, "admin", out[0].Service)
}
```

- [ ] **Step 2: Run, verify it fails**

Run: `go test ./internal/head/scout/ -run TestVerifyServiceAttribution -v`
Expected: FAIL.

- [ ] **Step 3: Implement `verifyServiceAttribution`**

Batch all cases into one LLM call; parse corrections; apply only corrections whose target service exists in `services`; unknown corrections ignored + logged. On any LLM error, return cases unchanged + log.

- [ ] **Step 4: Wire into planning (after Step 5 of Task 5)**

After attribution, call `cases = verifyServiceAttribution(s.driver, cases, s.config.Services)`.

- [ ] **Step 5: Run, verify it passes + no regression**

Run: `go test ./internal/head/scout/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/head/scout/direct_planning.go internal/head/scout/direct_planning_test.go
git commit -m "feat(scout): LLM verification of service attribution"
```

---

### Task 7: Backward-compat integration test + full suite

**Files:**
- Test: `internal/head/agent/executor_test.go` (existing tests already cover single-service; add explicit guard)

**Interfaces:** none new.

- [ ] **Step 1: Add a guard test — single-service project behaves unchanged**

```go
func TestReActLoop_SingleServiceBackwardCompat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	// testLoop builds a single-service engine via Services[0]; Service fields empty.
	loop, s := testLoop(t, nil, server)
	sessionID := createTestSession(t, s)
	plan := &TestPlan{Goal: "g", Cases: []TestCase{
		{ID: "t1", Target: "/api/users", Method: "GET", Expectation: "ok"},
	})}
	results, err := loop.ExecutePlan(context.Background(), plan, sessionID)
	require.NoError(t, err)
	require.Equal(t, StepPassed, results[0].Status)
}
```

- [ ] **Step 2: Run full suite**

Run: `make check`
Expected: all PASS, `gofmt` clean, `golangci-lint` 0 issues.

- [ ] **Step 3: Commit**

```bash
git add internal/head/agent/executor_test.go
git commit -m "test(agent): guard single-service backward compat"
```

---

## Self-Review Notes

- **Spec coverage:** data-model fields (Task 1), rule-engine routing + per-service actor (Task 2), ReAct URL by service (Task 3), session wiring (Task 4), prefix attribution (Task 5), LLM verify (Task 6), backward compat (Task 7). Spec's "path_prefix 未配时降级" → `attributeService` returns "" → `Services[0]` fallback (Task 5 + Task 2). Spec's "domain/key 不碰" is block ②, not this plan.
- **Type consistency:** `NewRuleEngine(services, actors, workDir)` used identically in Task 2 (def) and Task 4 (call sites). `withBaseURL(tc, action)` used in Task 3 (def) and callers. `baseURLFor`/`authHeadersFor`/`serviceHeaders` consistent.
- **Known follow-up:** per-service actor injection on the ReAct path (not just rule-engine path) is left as a `// TODO` in Task 3 Step 3; full per-service auth on steered actions is a later refinement, out of block ①' core.
