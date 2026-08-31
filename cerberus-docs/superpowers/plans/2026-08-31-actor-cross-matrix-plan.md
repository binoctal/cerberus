# Actor Cross-Matrix v1 (Read-Only IDOR Tier) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Emit a `-crossuser` test-case tier that proves a second web principal (rival) is rejected (4xx) when reading the owner's resource ids — horizontal isolation / IDOR detection.

**Architecture:** Everything declarative + deterministic, per the zero-LLM generation discipline. A `web-rival` protocol role (`http_only: true`, backed by a second dev user seeded via the admin-actor auth shape) supplies the rival JWT through the existing `AuthRole` → `CredentialRef` mechanism — executor, claims gate, and coverage attribution are untouched. The generator learns the rival's name from a new vocab judgment field (`http_cross_role`), never from a path/role literal (isAdminPath lesson). Per-route opt-out rides a `cross_exempt` mark preserved across vocab regen like `min_query`.

**Tech Stack:** Go 1.25, `modernc.org/sqlite` (no CGo), plain `testing`.

**Spec:** `cerberus-docs/superpowers/specs/2026-08-31-actor-cross-matrix-design.md` (read it first — the plan argues from it; the spec's three load-bearing review findings are non-negotiable: `http_only: true`, min_query appended to the crossuser target, admin-actor auth shape for the rival actor).

## Global Constraints

- Commit author `binoctal <binoctal@gmail.com>`, commit messages and code comments in English, no `Co-Authored-By`.
- No CGo; module `github.com/binoctal/cerberus`.
- All docs in `cerberus-docs/`, never `docs/`.
- **run39 may be running the dogfood environment while you work.** Never start/kill wrangler, never run `cerberus run`, never touch `dogfood/realtime-e2e/.cerberus/runtime/` until it has finished (check `dogfood/realtime-e2e/.cerberus/runtime/logs/run39-launcher.log` for `cerberus run exited rc=`). Editing `.cerberus/project.yaml`, `.cerberus/protocols/`, and `.cerberus/vocab/` files is safe — the running session loaded its config at start.
- Test commands: `go test ./internal/project/ -run <Name>` (fast, targeted); `make check` (fmt + lint + full suite) only at the final gate.

---

### Task 1: Vocab schema fields — `cross_exempt` (route) and `http_cross_role` (vocabulary)

**Files:**
- Modify: `internal/project/vocabulary.go` (~line 172 area for the route field; ~line 38 area for the vocabulary field)
- Test: `internal/project/vocabulary_test.go`

**Interfaces:**
- Consumes: existing `VocabHTTPRoute` / `Vocabulary` structs.
- Produces: `VocabHTTPRoute.CrossExempt bool` (yaml `cross_exempt`), `Vocabulary.HTTPCrossRole string` (yaml `http_cross_role`). Later tasks rely on these exact names.

- [ ] **Step 1: Write the failing round-trip test**

Append to `internal/project/vocabulary_test.go` (match the file's existing style; if it already has a YAML round-trip helper, extend that instead of duplicating):

```go
func TestVocabCrossFieldsRoundTrip(t *testing.T) {
	in := &Vocabulary{
		HTTPCrossRole: "web-rival",
		HTTPRoutes: []VocabHTTPRoute{
			{Method: "GET", Path: "/api/teams/:id", CrossExempt: true,
				ParamSources: map[string]VocabParamSource{":id": {Route: "GET /api/teams", Pick: "0.id"}}},
		},
	}
	block, err := yaml.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Vocabulary
	if err := yaml.Unmarshal(block, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.HTTPCrossRole != "web-rival" {
		t.Fatalf("http_cross_role = %q, want web-rival", out.HTTPCrossRole)
	}
	if !out.HTTPRoutes[0].CrossExempt {
		t.Fatal("cross_exempt lost in round-trip")
	}
}
```

(If `yaml` is not already imported in that test file, add `yaml "gopkg.in/yaml.v3"` matching the package's existing import style — check how other tests in the package import it.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/project/ -run TestVocabCrossFieldsRoundTrip -v`
Expected: FAIL — `CrossExempt` / `HTTPCrossRole` undefined (compile error).

- [ ] **Step 3: Add the fields**

In `internal/project/vocabulary.go`, inside `VocabHTTPRoute` after the `MinQuery` field (keep the comment style — one fact per field):

```go
	// CrossExempt vetoes the -crossuser tier for this route: the resource is
	// genuinely shared cross-principal (e.g. team-scoped membership), so a
	// rival's 200 is legitimate access, not an isolation failure. Judgment
	// layer — live-probe knowledge, preserved across re-extraction like
	// min_query.
	CrossExempt bool `yaml:"cross_exempt,omitempty" json:"cross_exempt,omitempty"`
```

Inside `Vocabulary` after `HTTPDefaultRole`:

```go
	// HTTPCrossRole names the protocol role used as the second principal in
	// the -crossuser isolation tier. It must resolve to a credentialed,
	// http_only protocol role (a principal that exists for AuthRole injection
	// and nothing else). Judgment layer — setting it opts a vocabulary into
	// the cross tier; unset means no crossuser cases (generic repos are
	// unaffected).
	HTTPCrossRole string `yaml:"http_cross_role,omitempty" json:"http_cross_role,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/project/ -run TestVocabCrossFieldsRoundTrip -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/project/vocabulary.go internal/project/vocabulary_test.go
git commit -m "feat(vocab): cross_exempt route mark + http_cross_role vocabulary field"
```

---

### Task 2: Regen preserves the new judgment marks

The regen merge currently lives inline in `cmd/cerberus/main_protocol.go` (`runProtocolVocabulary`, ~lines 329-393), untestable in isolation. Extract it into a pure function in `internal/project` and preserve the two new fields there.

**Files:**
- Create: `internal/project/vocabulary_merge.go`
- Create test: `internal/project/vocabulary_merge_test.go`
- Modify: `cmd/cerberus/main_protocol.go:329-393` (replace inline merge body with a call)

**Interfaces:**
- Consumes: `Vocabulary` from Task 1.
- Produces: `func MergeVocabJudgment(prev, fresh *Vocabulary)` — mutates `fresh` in place, copying every judgment-layer value from `prev` (edges' Partial/Unsupported, HTTPAuthMiddlewares, UI, HTTPRoleRoutes, HTTPDefaultRole, per-route marks keyed `method|path` including the new `CrossExempt`, and `HTTPCrossRole`). Called by `runProtocolVocabulary` only when `prev != nil`.

- [ ] **Step 1: Write the failing test**

`internal/project/vocabulary_merge_test.go`:

```go
package project

import "testing"

func TestMergeVocabJudgmentPreservesCrossMarks(t *testing.T) {
	prev := &Vocabulary{
		HTTPCrossRole: "web-rival",
		HTTPRoleRoutes: []VocabRoleRoute{{Prefix: "/api/admin", Role: "admin"}},
		HTTPRoutes: []VocabHTTPRoute{
			{Method: "GET", Path: "/api/teams/:id", CrossExempt: true, MinQuery: map[string]string{"x": "1"}},
		},
	}
	fresh := &Vocabulary{
		HTTPRoutes: []VocabHTTPRoute{
			{Method: "GET", Path: "/api/teams/:id"}, // re-derived, no marks
			{Method: "GET", Path: "/api/sessions/:id"},
		},
	}
	MergeVocabJudgment(prev, fresh)
	if fresh.HTTPCrossRole != "web-rival" {
		t.Fatalf("http_cross_role = %q, want preserved web-rival", fresh.HTTPCrossRole)
	}
	if fresh.HTTPRoleRoutes == nil || len(fresh.HTTPRoleRoutes) != 1 {
		t.Fatalf("role routes not preserved: %+v", fresh.HTTPRoleRoutes)
	}
	if !fresh.HTTPRoutes[0].CrossExempt {
		t.Fatal("cross_exempt not preserved on re-derived route")
	}
	if fresh.HTTPRoutes[1].CrossExempt {
		t.Fatal("cross_exempt must not leak onto routes the prev vocab never marked")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/project/ -run TestMergeVocabJudgment -v`
Expected: FAIL — `MergeVocabJudgment` undefined.

- [ ] **Step 3: Extract the merge into `internal/project/vocabulary_merge.go`**

Move the ENTIRE `if prev != nil { ... }` body from `runProtocolVocabulary` (the edges marks, auth middlewares, UI, role routes, default role, and route-marks blocks — verbatim, comments included) into:

```go
package project

// MergeVocabJudgment copies every judgment-layer value from prev onto fresh,
// in place. Fresh is the re-extracted vocabulary (its fact layer — edges,
// routes, middlewares — was derived from source and always wins); prev is the
// previous vocab file whose hand-curated marks (partial/unsupported, role
// map, min_query, param chains, cross marks) encode live-probe knowledge a
// re-extraction cannot know and must never silently drop.
func MergeVocabJudgment(prev, fresh *Vocabulary) {
	// ... body moved verbatim from cmd/cerberus/main_protocol.go, with two
	// additions:
	// (1) beside the HTTPDefaultRole preserve:
	if prev.HTTPCrossRole != "" {
		fresh.HTTPCrossRole = prev.HTTPCrossRole
	}
	// (2) inside the routeMarks loop, beside MinQuery:
	if old.CrossExempt {
		fresh.HTTPRoutes[i].CrossExempt = old.CrossExempt
	}
}
```

In `cmd/cerberus/main_protocol.go`, replace the inline `if prev != nil { ... }` block with:

```go
	if prev != nil {
		project.MergeVocabJudgment(prev, vocab)
	}
```

(Keep the surrounding comment lines that explain WHY re-extraction must not drop marks; they move with the body.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/project/ -run TestMergeVocabJudgment -v && go build ./cmd/...`
Expected: PASS + clean build.

- [ ] **Step 5: Commit**

```bash
git add internal/project/vocabulary_merge.go internal/project/vocabulary_merge_test.go cmd/cerberus/main_protocol.go
git commit -m "refactor(vocab): extract regen judgment merge; preserve cross marks"
```

---

### Task 3: The `-crossuser` generator tier

**Files:**
- Modify: `internal/head/scout/http_route_cases.go`
- Test: `internal/head/scout/http_route_cases_test.go`

**Interfaces:**
- Consumes: `Vocabulary.HTTPCrossRole`, `VocabHTTPRoute.CrossExempt` (Task 1); `roleForRoute`, `paramResolvable`, `routeParams`, `fillRouteParamsFromSources`, `minQueryString`, `routeBaseID` (existing in this file).
- Produces: `func crossRoleFor(svc project.Service) string` and `func crossUserRouteCase(svc project.Service, host string, r project.VocabHTTPRoute, method, ownerRole, rivalRole string) agent.TestCase`; case IDs `routeBaseID(...) + "-crossuser"`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/head/scout/http_route_cases_test.go`. Build the fixture exactly like `TestHTTPRouteCases_TiersAndOrdering` does (same imports already in the file):

```go
// crossFixture: web role (credentialed, connects), web-rival (credentialed,
// http_only), admin (credentialed, http_only); vocab opts into the cross
// tier via http_cross_role and maps everything to web by default.
func crossFixture(crossRole string, exemptTeams bool) project.Service {
	sessions := project.VocabHTTPRoute{Method: "GET", Path: "/api/sessions/:id", Auth: "required",
		ParamSources: map[string]project.VocabParamSource{":id": {Route: "GET /api/sessions", Pick: "0.id"}}}
	teams := project.VocabHTTPRoute{Method: "GET", Path: "/api/teams/:id", Auth: "required", CrossExempt: exemptTeams,
		ParamSources: map[string]project.VocabParamSource{":id": {Route: "GET /api/teams", Pick: "0.id"}}}
	post := project.VocabHTTPRoute{Method: "POST", Path: "/api/sessions/:id", Auth: "required",
		ParamSources: map[string]project.VocabParamSource{":id": {Route: "GET /api/sessions", Pick: "0.id"}}}
	adminSrc := project.VocabHTTPRoute{Method: "GET", Path: "/api/admin/audit/:id", Auth: "required",
		ParamSources: map[string]project.VocabParamSource{":id": {Route: "GET /api/admin/audit", Pick: "0.id"}}}
	return project.Service{
		Name: "realtime", URL: "http://localhost:8989/ws/{userId}",
		Protocol: &project.Protocol{Roles: map[string]*project.ProtocolRole{
			"web":       {CredentialRef: "web-actor"},
			"web-rival": {CredentialRef: "web-rival-actor", HTTPOnly: true},
			"admin":     {CredentialRef: "admin-actor", HTTPOnly: true},
		}},
		Vocabulary: &project.Vocabulary{
			HTTPCrossRole:   crossRole,
			HTTPDefaultRole: "web",
			HTTPRoleRoutes:  []project.VocabRoleRoute{{Prefix: "/api/admin", Role: "admin"}},
			HTTPRoutes:      []project.VocabHTTPRoute{sessions, teams, post, adminSrc},
		},
	}
}

func findCase(cases []agent.TestCase, id string) *agent.TestCase {
	for i := range cases {
		if cases[i].ID == id {
			return &cases[i]
		}
	}
	return nil
}

func TestHTTPRouteCases_CrossUserTier(t *testing.T) {
	svc := crossFixture("web-rival", false)
	cases := httpRouteCases(svc)

	c := findCase(cases, "http-route-realtime-get-api-sessions-1-crossuser")
	if c == nil {
		t.Fatal("sessions detail route must emit a -crossuser case")
	}
	if len(c.Steps) != 2 {
		t.Fatalf("crossuser = capture + target, got %d steps", len(c.Steps))
	}
	if c.Steps[0].AuthRole != "web" || c.Steps[0].ExpectStatusClass != "2xx" {
		t.Fatalf("capture step must run as owner web asserting 2xx: %+v", c.Steps[0])
	}
	tgt := c.Steps[1]
	if tgt.AuthRole != "web-rival" {
		t.Fatalf("target step AuthRole = %q, want web-rival", tgt.AuthRole)
	}
	if tgt.ExpectStatusClass != "4xx" {
		t.Fatalf("target must assert 4xx (isolation), got %q", tgt.ExpectStatusClass)
	}
	if !strings.Contains(tgt.URL, "{{case.p_id}}") {
		t.Fatalf("target must use the captured owner id: %q", tgt.URL)
	}
	if !strings.Contains(c.Expectation, "cross-user isolation") {
		t.Fatalf("expectation must name cross-user isolation: %q", c.Expectation)
	}

	// POST routes never get the tier (v1 is read-only).
	if findCase(cases, "http-route-realtime-post-api-sessions-1-crossuser") != nil {
		t.Fatal("POST route must not emit a crossuser case")
	}
	// Admin-sourced ids are not owner data.
	if findCase(cases, "http-route-realtime-get-api-admin-audit-1-crossuser") != nil {
		t.Fatal("admin-sourced route must not emit a crossuser case")
	}
}

func TestHTTPRouteCases_CrossUserExemptAndGates(t *testing.T) {
	// cross_exempt drops only the crossuser tier (authed stays).
	svc := crossFixture("web-rival", true)
	cases := httpRouteCases(svc)
	if findCase(cases, "http-route-realtime-get-api-teams-1-crossuser") != nil {
		t.Fatal("cross_exempt route must not emit a crossuser case")
	}
	if findCase(cases, "http-route-realtime-get-api-teams-1-authed") == nil {
		t.Fatal("cross_exempt must NOT drop the authed tier")
	}

	// No cross role declared -> no tier at all (generic repos unaffected).
	svc = crossFixture("", false)
	if got := countSuffix(httpRouteCases(svc), "-crossuser"); got != 0 {
		t.Fatalf("http_cross_role unset: %d crossuser cases, want 0", got)
	}

	// The cross role must be http_only: declaring a connecting role as the
	// rival would be the client-leg hijack the spec forbids.
	svc = crossFixture("web", false)
	if got := countSuffix(httpRouteCases(svc), "-crossuser"); got != 0 {
		t.Fatalf("non-http_only cross role: %d crossuser cases, want 0", got)
	}
}

func countSuffix(cases []agent.TestCase, suffix string) int {
	n := 0
	for _, c := range cases {
		if strings.HasSuffix(c.ID, suffix) {
			n++
		}
	}
	return n
}
```

Note: `VocabHTTPRoute` is referenced unqualified inside package `scout` tests only via `project.VocabHTTPRoute` — keep the `project.` prefix in the fixture helper (match the existing test file's style).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/head/scout/ -run TestHTTPRouteCases_CrossUser -v`
Expected: FAIL — no `-crossuser` cases emitted.

- [ ] **Step 3: Implement the tier**

In `internal/head/scout/http_route_cases.go`:

(a) In `httpRouteCases`, extend the `auth == "required"` branch (currently lines 57-67):

```go
		if r.Auth == "required" {
			cases = append(cases,
				unauthRouteCase(svc, host, r, method),
				reachabilityRouteCase(svc, host, r, method))
			if role := roleForRoute(svc, r.Path); role != "" && paramResolvable(r) {
				c := authedRouteCase(svc, host, r, method, role)
				cases = append(cases, c)
				// Crossuser tier: same shape as authed, but the target step
				// runs as the rival principal and asserts the 4xx rejection.
				// v1 is read-only (GET only): a genuinely vulnerable SUT must
				// not be proven vulnerable by destroying the owner's data.
				if rival := crossRoleFor(svc); rival != "" && method == "GET" &&
					!r.CrossExempt && crossOwnerEligible(svc, role) &&
					ownerScopedSources(svc, r, role) {
					cases = append(cases, crossUserRouteCase(svc, host, r, method, role, rival))
				}
			}
			continue
		}
```

(b) New helpers at the bottom of the file:

```go
// crossOwnerEligible reports whether an owner role may carry the crossuser
// tier: it must be an INTERACTIVE principal — credentialed, connects over WS
// (not http_only), not owned by a real process (not process_bound). This is
// the spec's "web role" expressed as mechanical role properties instead of a
// hardcoded name (isAdminPath lesson): injection-only roles like admin are
// vertical boundaries, and a second principal of the same role reading admin
// data would be an escalation probe, not horizontal isolation.
func crossOwnerEligible(svc project.Service, role string) bool {
	if svc.Protocol == nil {
		return false
	}
	r := svc.Protocol.Roles[role]
	return r != nil && r.CredentialRef != "" && !r.HTTPOnly && !r.ProcessBound
}

// crossRoleFor resolves the vocabulary's cross-tier principal: it must be a
// declared protocol role that carries a credential AND is http_only — a
// connecting role as the rival would leak into client-role selection
// (real_responder_cases / acp_cases), the hijack the spec's http_only finding
// guards against. "" disables the tier entirely.
func crossRoleFor(svc project.Service) string {
	name := svc.Vocabulary.HTTPCrossRole
	if name == "" || svc.Protocol == nil {
		return ""
	}
	r := svc.Protocol.Roles[name]
	if r == nil || r.CredentialRef == "" || !r.HTTPOnly {
		return ""
	}
	return name
}

// ownerScopedSources reports whether every :param of the route chains from a
// list route resolved to the SAME role as the target's owner — i.e. the
// captured id is the owner principal's data, not admin-carried or unmapped.
func ownerScopedSources(svc project.Service, r project.VocabHTTPRoute, ownerRole string) bool {
	for _, p := range routeParams(r.Path) {
		ps, ok := r.ParamSources[p]
		if !ok {
			return false
		}
		_, listPath, _ := strings.Cut(ps.Route, " ")
		if roleForRoute(svc, listPath) != ownerRole {
			return false
		}
	}
	return true
}

// crossUserRouteCase is the authed tier with the target step re-roted: the
// capture steps run as the owner (asserting the real id exists), the target
// GET runs as the rival principal and must be REJECTED — a 200 is a real
// IDOR finding and fails the case. min_query is appended to the target like
// the authed tier: without it a missing-query 400 would satisfy the 4xx
// assertion and mask exactly the IDOR being hunted.
func crossUserRouteCase(svc project.Service, host string, r project.VocabHTTPRoute, method, ownerRole, rivalRole string) agent.TestCase {
	steps := make([]agent.TestStep, 0, len(r.ParamSources)+1)
	for _, p := range routeParams(r.Path) {
		ps := r.ParamSources[p]
		_, listPath, _ := strings.Cut(ps.Route, " ")
		listRole := roleForRoute(svc, listPath)
		if listRole == "" {
			listRole = ownerRole
		}
		steps = append(steps, agent.TestStep{
			Action:            "http_request",
			URL:               host + fillRouteParams(listPath),
			Method:            "GET",
			AuthRole:          listRole,
			ExpectStatusClass: "2xx",
			Capture:           map[string]string{ps.Pick: "p_" + strings.TrimPrefix(p, ":")},
		})
	}
	targetURL := host + fillRouteParamsFromSources(r.Path, r.ParamSources) + minQueryString(r.MinQuery)
	steps = append(steps, agent.TestStep{
		Action:            "http_request",
		URL:               targetURL,
		Method:            method,
		AuthRole:          rivalRole,
		ExpectStatusClass: "4xx",
	})
	return agent.TestCase{
		ID:          routeBaseID(svc, method, r.Path) + "-crossuser",
		Name:        fmt.Sprintf("%s %s cross-user isolation", method, fillRouteParams(r.Path)),
		Service:     svc.Name,
		Target:      host,
		Action:      "ws_flow",
		Expectation: "cross-user isolation: rival principal reading another user's resource is rejected (4xx) — horizontal access control holds",
		Priority:    0.5,
		Steps:       steps,
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/head/scout/ -run TestHTTPRouteCases -v`
Expected: PASS (new and all existing tiers — the existing tests must not change, since none of them set `http_cross_role`).

- [ ] **Step 5: Commit**

```bash
git add internal/head/scout/http_route_cases.go internal/head/scout/http_route_cases_test.go
git commit -m "feat(scout): -crossuser read-only IDOR tier from http_cross_role"
```

---

### Task 4: Dogfood declarations — rival principal, role, and vocab opt-in

**Files:**
- Modify: `dogfood/realtime-e2e/.cerberus/project.yaml` (add actor after `admin-actor`)
- Modify: `dogfood/realtime-e2e/.cerberus/protocols/open-agents.yaml` (add role after `admin`)
- Modify: `dogfood/realtime-e2e/.cerberus/vocab/open-agents.vocab.yaml` (add one line)

**Interfaces:**
- Consumes: Task 1 fields, Task 3 generator, the provisioning-only+http_login validation relaxation already on main (076fa60).
- Produces: a dogfood config that loads cleanly and emits 7 crossuser cases on the next run.

- [ ] **Step 1: Add the rival actor to project.yaml**

After the `admin-actor` block (keep the distinct-email comment discipline — see the spec's load-bearing finding #3):

```yaml
  # Second web principal for the -crossuser isolation tier (spec:
  # cerberus-docs/superpowers/specs/2026-08-31-actor-cross-matrix-design.md).
  # The ADMIN auth shape, not web-actor's: {email}/{password} templated into
  # BOTH bodies — web-actor's setup body (plan only) provisions the DEFAULT
  # dev user, which would make the rival the same principal as the owner and
  # every crossuser case a false 200. plan: pro isolates this tier's variable
  # (a free-plan rival would conflate plan-gate 403 with isolation 403).
  - name: web-rival-actor
    credentials:
      email: rival@openagents.local
      password: rival123456
    auth:
      login:
        method: POST
        path: /api/dev/setup
        body:
          email: "{email}"
          password: "{password}"
          plan: pro
        headers:
          Origin: http://localhost:8989
      # token_from omitted on purpose: provisioning-only flow (mirrors admin-actor)
      inject_as: "Authorization: Bearer {token}"
      http_login:
        method: POST
        path: /api/dev/login
        body:
          email: "{email}"
          password: "{password}"
        headers:
          Origin: http://localhost:8989
      http_token_from: token
```

- [ ] **Step 2: Add the rival role to protocols/open-agents.yaml**

After the `admin` role (line ~75), mirroring its shape exactly:

```yaml
  # Second web principal for the -crossuser tier. http_only is load-bearing:
  # client-role selection (real_responder_cases / acp_cases) takes the first
  # credentialed non-real non-http_only role — without the flag this role is
  # one rename away from hijacking the emulated-client leg.
  web-rival:
    credential_ref: web-rival-actor
    http_only: true
```

- [ ] **Step 3: Opt the vocabulary into the cross tier**

In `dogfood/realtime-e2e/.cerberus/vocab/open-agents.vocab.yaml`, beside the existing `http_role_routes` / `http_default_role` keys (top level), add:

```yaml
http_cross_role: web-rival
```

No `cross_exempt` marks yet — the first run's evidence decides (expected candidate: `GET /api/teams/:id`, team-scoped membership).

- [ ] **Step 4: Verify the config loads and the tier emits**

Run: `go test ./internal/project/ -run Dogfood -v`
Expected: PASS (dogfood config validation — the rival actor's provisioning-only+http_login shape rides the 076fa60 relaxation).

Then, a dry count of what the generator will emit (pure Go, no dogfood environment touched):

```bash
cat > internal/head/scout/crosscount_dogfood_test.go <<'EOF'
package scout

import (
	"testing"

	"github.com/binoctal/cerberus/internal/project"
)

// The loader attaches .cerberus/vocab/<protocol_ref>.vocab.yaml to each
// service (loader.go:146), so LoadFromFile yields the fully assembled
// services the generator will see on the next run.
func TestDogfoodCrossUserCount(t *testing.T) {
	cfg, err := project.LoadFromFile("../../dogfood/realtime-e2e/.cerberus/project.yaml")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, svc := range cfg.Services {
		for _, c := range httpRouteCases(svc) {
			if len(c.ID) > 10 && c.ID[len(c.ID)-10:] == "-crossuser" {
				n++
			}
		}
	}
	if n != 7 {
		t.Fatalf("crossuser cases = %d, want 7 (spec's candidate count)", n)
	}
}
EOF
go test ./internal/head/scout/ -run TestDogfoodCrossUserCount -v
```

If the count differs from 7, diff the emitted IDs against the spec's candidate list (sessions, agents, skills, permissions, missions, teams, external-agents) and reconcile — either the spec's count was stale (update the spec with a one-line errata) or the predicate is wrong (fix the predicate; do NOT tune the predicate to hit 7).

Keep `crosscount_dogfood_test.go` as a permanent dogfood-shape guard if it passes; delete it if it proves brittle (e.g. vocab churn makes 7 a moving target — then keep only the generator unit tests from Task 3).

- [ ] **Step 5: Commit**

```bash
git add dogfood/realtime-e2e/.cerberus/project.yaml dogfood/realtime-e2e/.cerberus/protocols/open-agents.yaml dogfood/realtime-e2e/.cerberus/vocab/open-agents.vocab.yaml internal/head/scout/crosscount_dogfood_test.go
git commit -m "feat(dogfood): web-rival principal + cross-tier opt-in"
```

---

### Task 5: Full gate

**Files:** none new.

- [ ] **Step 1: Run the full check**

Run: `make check`
Expected: fmt clean, lint clean, all tests pass.

- [ ] **Step 2: Commit anything fmt touched, push**

```bash
git add -A && git commit -m "chore: fmt after cross-matrix v1"  # only if fmt changed files
git push origin main
```

- [ ] **Step 3: Report**

Report: commit list, the crossuser case count emitted for the dogfood vocab, and the run40 acceptance line from the spec (all 4xx green = isolation holds; any 200 = real IDOR finding to file against open-agents). Do NOT launch run40 — that is the operator's call after run39's harvest.

---

## Self-Review Notes

- Spec coverage: §1 case shape → Task 3; §2 rival declarations → Task 4 (+ http_only finding baked into `crossRoleFor` and the role YAML); §3 exemption + regen preservation → Tasks 1-2; §4 coverage/judging → no code needed (rides existing machinery; verified by not touching it); §5 testing → Tasks 1-4 test steps + Task 5 gate.
- The 7-case count check is reconciled against the spec, not forced to match.
- Dogfood files are edited but nothing restarts the environment (global constraint).
