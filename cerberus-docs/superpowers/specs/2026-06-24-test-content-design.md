# Test-Content Improvements — Design

- Date: 2026-06-24
- Status: Implemented (2026-06-24) — commits d6a49cb, b2fb111, 1e7e029, 53d248f, 63bb2e8
- Author: binoctal

## Goal

Fix two test-content gaps surfaced by running cerberus against modelsite:
- **(A)** rule-engine POST sends no body → gateway returns 400 (`missing model`).
- **(B)** ReAct LLM steer emits absolute URLs with the wrong host:port (8080 instead of 8081) → requests hit the wrong service.

## Background

- `HTTPAction` already has a `Body` field, but `TestCase`/`EndpointDef` carry no body and Scout emits none → the rule engine constructs POSTs without a body.
- `withBaseURL` only resolves relative URLs; an absolute URL from the LLM (wrong host) is used as-is.

## A. POST body via layered sources

- `project.Service` gains optional `body_template` — a human-configured default body (e.g. `{"model":"...","messages":[...]}`) holding values the LLM can't know (real model name, provider-specific fields).
- `TestCase` gains optional `Body`. Scout fills it per case:
  - if the case's service has a `body_template`, generate **case-specific variations on it** (mutate messages, fields for boundary/negative cases);
  - otherwise LLM infers a minimal body (best-effort).
  - Only for POST/PUT; GET/DELETE get no body.
- rule engine `matchHTTPRules`: `HTTPAction.Body = tc.Body`.

So body comes from the most reliable source available: human template (accurate) → Scout LLM variation (automatic). Cerberus provides the mechanism; the project fills `body_template`.

## B. withBaseURL forces tc.Service host + ReAct prompt hint

- `withBaseURL`: regardless of relative or absolute, normalize the action URL's `host:port` to **tc.Service's base**, preserving path/query. (LLM gives `http://localhost:8080/v1/chat/completions`, tc.Service=gateway → `http://localhost:8081/v1/chat/completions`.)
- ReAct steer prompt: include the current service's base URL so the LLM tends to give the right host in the first place — the force is the backstop, not the only layer.

## Boundaries

- A applies only to POST/PUT.
- B applies only when `tc.Service` has a base (empty → unchanged; backward compatible with single-service projects).
- `body_template` value is human-configured per project; cerberus reads it.

## Verification standard

After A+B, a POST carries a body and reaches the gateway's business layer (passes the 400 "missing model"). Full chat success additionally depends on modelsite's model/provider routing — that is modelsite business logic, **out of scope for cerberus**. The bar is "request reaches the business layer with a well-formed body", not "the whole chat returns 200".

## Out of scope

- Discovering body schemas from OpenAPI/Swagger (future enhancement; `body_template` covers it manually for now).
- Per-case body mutation strategies beyond what Scout LLM generates.
- modelsite code changes.

## Decisions

- **Body source**: layered — human `body_template` (accurate, esp. for unknown-to-LLM fields like model name) > Scout LLM variation. Not pure-LLM (LLM can't know provider model names).
- **YAGNI**: only `TestCase.Body`; `EndpointDef.Body` not added (body is case content, not endpoint structure).
- **B**: force host (reliable) + prompt hint (so LLM is less likely to err). Two layers.
