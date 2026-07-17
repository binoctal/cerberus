# ReAct Per-Service Actor Header Injection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the ReAct path's `withActorHeaders` select the actor matching `tc.Service` (and inject `X-Test-User`) by delegating to `RuleEngine.authHeadersFor`, eliminating the divergence from the deterministic rule path.

**Architecture:** `withActorHeaders` replaces its hard-coded `actors[0]` lookup with a call to `r.engine.authHeadersFor(tc)`, which already encodes the three-tier actor selection (service actor → global actor → `actors[0]`) and builds `X-Test-User` from the actor's email. The returned map is merged underneath the action's own headers, preserving the existing `service < actor < action` layering. Single source of truth, no duplicated selection logic.

**Tech Stack:** Go 1.25, `testify`, `go.uber.org/zap`.

## Global Constraints

- Go 1.25, module `github.com/binoctal/cerberus`, pure Go (no CGo).
- Code comments and commit messages in English.
- Commit author `binoctal <binoctal@gmail.com>`, **no** `Co-Authored-By`.
- Follow existing comment density and naming idiom.
- Documentation only in `cerberus-docs/` (never `docs/`).
- Every task ends with `make build` green; final task runs `make check`.

## File Structure

| File | Responsibility | Action |
|---|---|---|
| `internal/head/agent/react_loop_helpers.go` | `withActorHeaders` — merge selected actor's headers under the action's | Modify |
| `internal/head/agent/actor_headers_test.go` | Unit tests for `withActorHeaders` selection / `X-Test-User` / fallbacks | Modify |

---

### Task 1: `withActorHeaders` delegates to `authHeadersFor`

**Files:**
- Modify: `internal/head/agent/react_loop_helpers.go:186-216` (the `withActorHeaders` method and its doc comment)
- Test: `internal/head/agent/actor_headers_test.go`

**Interfaces:**
- Consumes: `RuleEngine.authHeadersFor(tc TestCase) map[string]string` (already defined in `internal/head/agent/rules.go:91`; three-tier selection + `X-Test-User` from email).
- Produces: unchanged signature `(r *ReActLoop) withActorHeaders(tc TestCase, action types.TypedAction) types.TypedAction` — callers (`executeAndRecordAction`) are unaffected.

**Context for the implementer:**
- `ReActLoop` and `RuleEngine` are in the same package (`agent`); `authHeadersFor` is unexported but directly callable as `r.engine.authHeadersFor(tc)`.
- The existing tests `TestReActLoop_WithActorHeadersMerged` and `TestReActLoop_WithActorHeadersNonHTTP` (in `actor_headers_test.go`) use a single global actor (`Service == ""`) and a `TestCase` with empty `Service`; under `authHeadersFor` this resolves to that global actor, so they continue to pass unchanged. Do not modify them.
- `authHeadersFor` returns `nil` when there are no actors or the selected actor has neither email nor headers — `withActorHeaders` must treat `nil`/empty as "no actor layer, return action unchanged".

- [ ] **Step 1: Write the failing tests**

Append to `internal/head/agent/actor_headers_test.go` (the file already imports `project`, `types`, `testify/assert`; no new imports needed):

```go
// ReAct path selects the actor whose Service matches tc.Service, mirroring the
// rule engine — not actors[0].
func TestReActLoop_WithActorHeadersPerService(t *testing.T) {
	services := []project.Service{
		{Name: "a", URL: "http://a"},
		{Name: "b", URL: "http://b"},
	}
	actors := []project.Actor{
		{Service: "a", Credentials: project.CredentialRef{Headers: map[string]string{"Authorization": "Bearer sk-a"}}},
		{Service: "b", Credentials: project.CredentialRef{Headers: map[string]string{"Authorization": "Bearer sk-b"}}},
	}
	engine := NewRuleEngine(services, actors, "")
	loop := &ReActLoop{engine: engine}

	out := loop.withActorHeaders(TestCase{ID: "t1", Service: "b"}, types.HTTPAction{Method: "GET", URL: "http://b/y"})
	assert.Equal(t, "Bearer sk-b", out.(types.HTTPAction).Headers["Authorization"])

	// Service "a" does NOT leak actor b's credentials.
	outA := loop.withActorHeaders(TestCase{ID: "t2", Service: "a"}, types.HTTPAction{Method: "GET", URL: "http://a/y"})
	assert.Equal(t, "Bearer sk-a", outA.(types.HTTPAction).Headers["Authorization"])
}

// ReAct path injects X-Test-User from the selected actor's email, matching the
// rule path (previously dropped on the ReAct path).
func TestReActLoop_WithActorHeadersInjectsXTestUser(t *testing.T) {
	services := []project.Service{{Name: "a", URL: "http://a"}}
	engine := NewRuleEngine(services, []project.Actor{{
		Service:     "a",
		Credentials: project.CredentialRef{Email: "u@a"},
	}}, "")
	loop := &ReActLoop{engine: engine}

	out := loop.withActorHeaders(TestCase{Service: "a"}, types.HTTPAction{Method: "GET", URL: "http://a/y"})
	assert.Equal(t, "u@a", out.(types.HTTPAction).Headers["X-Test-User"])
}

// ReAct path falls back to a global actor (Service == "") when no actor matches
// tc.Service, then to actors[0] when no global actor exists.
func TestReActLoop_WithActorHeadersFallbacks(t *testing.T) {
	services := []project.Service{{Name: "a", URL: "http://a"}}
	specific := project.Actor{Service: "x", Credentials: project.CredentialRef{Headers: map[string]string{"Authorization": "Bearer sk-x"}}}
	global := project.Actor{Credentials: project.CredentialRef{Headers: map[string]string{"Authorization": "Bearer sk-global"}}}

	// Global-actor fallback: tc.Service matches no actor, but a global actor
	// exists (and is not actors[0]) — selection must skip the specific actor.
	engine := NewRuleEngine(services, []project.Actor{specific, global}, "")
	loop := &ReActLoop{engine: engine}
	out := loop.withActorHeaders(TestCase{Service: "missing"}, types.HTTPAction{Method: "GET", URL: "http://a/y"})
	assert.Equal(t, "Bearer sk-global", out.(types.HTTPAction).Headers["Authorization"])

	// actors[0] fallback: no matching actor and no global actor.
	engine2 := NewRuleEngine(services, []project.Actor{specific}, "")
	loop2 := &ReActLoop{engine: engine2}
	out2 := loop2.withActorHeaders(TestCase{Service: "missing"}, types.HTTPAction{Method: "GET", URL: "http://a/y"})
	assert.Equal(t, "Bearer sk-x", out2.(types.HTTPAction).Headers["Authorization"])
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/head/agent/ -run TestReActLoop_WithActorHeaders -v`
Expected: FAIL — `PerService` returns `Bearer sk-a` for `Service: "b"` (hard-coded `actors[0]`); `InjectsXTestUser` finds no `X-Test-User` key. `Fallbacks` may pass coincidentally when `actors[0]` happens to be the global/first actor, so rely on `PerService` and `InjectsXTestUser` as the failure signals.

- [ ] **Step 3: Rewrite `withActorHeaders`**

In `internal/head/agent/react_loop_helpers.go`, replace the entire doc comment and method (currently the block beginning `// withActorHeaders merges the active actor's Credentials.Headers underneath an` through the closing brace of `withActorHeaders`, including the `// TODO: per-service actor for ReAct path (currently uses actors[0]).` line) with:

```go
// withActorHeaders merges the selected actor's headers underneath an HTTP
// action's own headers (action overrides; empty removes). Actor selection and
// the X-Test-User header mirror authHeadersFor on the rule engine, so both
// paths authenticate identically. Non-HTTP actions pass through unchanged.
// Combined with withServiceHeaders, final priority is service < actor < action.
func (r *ReActLoop) withActorHeaders(tc TestCase, action types.TypedAction) types.TypedAction {
	if r.engine == nil {
		return action
	}
	actorHeaders := r.engine.authHeadersFor(tc)
	if len(actorHeaders) == 0 {
		return action
	}
	ha, ok := action.(types.HTTPAction)
	if !ok {
		return action
	}
	merged := make(map[string]string, len(actorHeaders)+len(ha.Headers))
	for k, v := range actorHeaders {
		merged[k] = v
	}
	for k, v := range ha.Headers {
		if v == "" {
			delete(merged, k)
			continue
		}
		merged[k] = v
	}
	ha.Headers = merged
	return ha
}
```

This removes the `TODO: per-service actor for ReAct path (currently uses actors[0]).` line.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/head/agent/ -run TestReActLoop_WithActorHeaders -v`
Expected: PASS — all of `Merged`, `NonHTTP`, `PerService`, `InjectsXTestUser`, `Fallbacks`.

Then run the rule-engine auth tests to confirm no regression in the shared helper's callers:

Run: `go test ./internal/head/agent/ -run TestRuleEngine_AuthHeaders -v`
Expected: PASS.

- [ ] **Step 5: Build**

Run: `make build`
Expected: compiles.

- [ ] **Step 6: Commit**

```bash
git add internal/head/agent/react_loop_helpers.go internal/head/agent/actor_headers_test.go
git commit -m "fix(agent): ReAct path selects per-service actor via authHeadersFor"
```

---

### Task 2: Full verification

**Files:** none (verification only).

- [ ] **Step 1: Format**

Run: `make fmt`
Expected: no changes (or apply them).

- [ ] **Step 2: Lint**

Run: `make lint`
Expected: no findings.

- [ ] **Step 3: Test (race)**

Run: `make test`
Expected: all packages PASS, including the new `PerService` / `InjectsXTestUser` / `Fallbacks` cases and the pre-existing `Merged` / `NonHTTP` cases.

- [ ] **Step 4: Combined check**

Run: `make check`
Expected: green.

- [ ] **Step 5: Commit any fmt/lint fixes**

```bash
git add -A
git commit -m "chore: fmt/lint after ReAct per-service actor wiring"
```

(Only if there are changes; otherwise skip.)

---

## Self-Review Notes

- **Spec coverage:** per-service selection → Task 1 `PerService` ✓; `X-Test-User` injection → Task 1 `InjectsXTestUser` ✓; global-actor and `actors[0]` fallbacks → Task 1 `Fallbacks` ✓; action override and empty-removes → pre-existing `Merged` (unchanged, still exercises the merge loop) ✓; non-HTTP passthrough → pre-existing `NonHTTP` ✓; `engine == nil` guard → retained in the replacement body ✓; TODO line removed → Task 1 Step 3 ✓.
- **Single source of truth:** the new body calls `r.engine.authHeadersFor(tc)`; no actor-selection logic is duplicated in `withActorHeaders`.
- **Layering preserved:** `executeAndRecordAction` still calls `withServiceHeaders` → `withActorHeaders` → `withBaseURL`; the merge-under-action semantics are unchanged, so `service < actor < action` holds.
- **Backward compatibility:** single-actor and global-actor projects resolve to `actors[0]` / the global actor exactly as before; the pre-existing `Merged` and `NonHTTP` tests assert this without modification.
- **No placeholders:** every code step shows complete, compilable code; commit messages and paths are exact.
