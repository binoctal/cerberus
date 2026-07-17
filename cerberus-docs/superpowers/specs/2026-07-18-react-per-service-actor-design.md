# ReAct Per-Service Actor Header Injection

**Date:** 2026-07-18
**Status:** Design
**Resolves:** `internal/head/agent/react_loop_helpers.go:189` — `TODO: per-service actor for ReAct path (currently uses actors[0])`

## Problem

When a project configures multiple services each backed by its own actor, the deterministic rule path (`RuleEngine.authHeadersFor`) selects the actor matching `tc.Service` (falling back to a global actor, then `actors[0]`). The ReAct path — taken when a test case has no matching rule and AI steer produces the action — does not: `ReActLoop.withActorHeaders` hard-codes `r.engine.actors[0]` regardless of `tc.Service`.

Two behavioral divergences result:

1. **Wrong actor.** A ReAct-steered request against service B is authenticated with service A's credentials (those of `actors[0]`). In multi-service routing this is a correctness bug: the request either fails auth or exercises the wrong tenant.
2. **Missing `X-Test-User`.** `authHeadersFor` injects `X-Test-User: <actor.Credentials.Email>` when set; `withActorHeaders` only merges `actor.Credentials.Headers` and omits this header. ReAct-steered requests thus lack the test-user identity the rule path carries.

## Goal

Make the ReAct path inject actor headers identically to the rule path, by sourcing both actor selection and header construction from a single implementation (`RuleEngine.authHeadersFor`).

## Non-Goals

- No change to the rule path.
- No change to service-level header injection (`withServiceHeaders`) or base-URL resolution (`withBaseURL`); the `service < actor < action` layering is preserved.
- No new config schema or persistence.

## Design

### Single source of truth

`withActorHeaders` calls `r.engine.authHeadersFor(tc)` and treats its return value as the actor header layer. `authHeadersFor` already encodes the three-tier selection (service actor → global actor `Service==""` → `actors[0]`) and builds `X-Test-User` from email. The ReAct path therefore inherits both fixes with no duplicated logic.

`authHeadersFor` is an unexported method on `RuleEngine`; `withActorHeaders` is a method on `ReActLoop` in the same package (`agent`), so no visibility change is required.

### Replacement body

`internal/head/agent/react_loop_helpers.go`, `withActorHeaders`:

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

The `TODO: per-service actor for ReAct path (currently uses actors[0]).` line is removed from the preceding doc comment.

### Layering invariant

`executeAndRecordAction` continues to call `withServiceHeaders` → `withActorHeaders` → `withBaseURL` in order. Because each step merges "underneath" the action's own headers, the final precedence remains `service < actor < action`, unchanged from today.

### Edge cases

- `engine == nil`: returns action unchanged (guard retained).
- No actors / selected actor has no headers and no email: `authHeadersFor` returns `nil`; `withActorHeaders` returns action unchanged.
- Non-HTTP action: passed through unchanged (existing behavior).
- Action header with empty value: removes the corresponding actor/service header (existing "empty removes" semantics preserved).

## Testing

New unit tests in `internal/head/agent/react_loop_helpers_test.go` (extend if present, else create), exercising `withActorHeaders` against a `ReActLoop` whose `engine` is a `RuleEngine` built from explicit services/actors:

1. **Per-service selection** — two actors (`Service: "a"`, `Service: "b"`) with distinct `Authorization` headers; a test case with `Service: "b"` yields `b`'s header, not `a`'s.
2. **`X-Test-User` injection** — selected actor has `Credentials.Email`; resulting action headers contain `X-Test-User: <email>`.
3. **Global-actor fallback** — no actor matches `tc.Service`, but an actor with `Service: ""` exists; its headers are applied.
4. **`actors[0]` fallback** — no service match, no global actor; `actors[0]`'s headers are applied.
5. **Action override** — action carries a header also present on the actor; action's value wins; an action header set to `""` removes the actor header.
6. **Non-HTTP passthrough** — a `ProcessAction` or `WaitAction` returns unchanged.
7. **No engine** — `r.engine == nil` returns the action unchanged.

Existing `authHeadersFor` behavior is already covered by rule-engine tests; the new tests assert the ReAct path delegates to it rather than re-deriving selection.

## Risk

- **Behavior change for existing ReAct runs:** any project relying on the old "always `actors[0]`" behavior for multi-service ReAct will now correctly select per service. This is the intended fix; single-actor projects are unaffected (selection resolves to `actors[0]`).
- **`X-Test-User` newly present on ReAct requests:** downstream services that key on this header will now see it on ReAct-steered requests where they previously did not. This aligns ReAct with the rule path; no production service is known to break, but it is an observable change.

## File Impact

| File | Change |
|---|---|
| `internal/head/agent/react_loop_helpers.go` | Rewrite `withActorHeaders` body; drop TODO line |
| `internal/head/agent/react_loop_helpers_test.go` | Add cases 1–7 above |
