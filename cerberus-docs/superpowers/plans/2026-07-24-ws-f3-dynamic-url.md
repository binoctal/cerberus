# WS Dynamic URL Path Params (F3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Checkbox tracking.

**Goal:** A WS service URL may contain `{param}` placeholders resolved at connect from fields the role's auth flow captured from its login response. Replaces literal baking of runtime path ids (e.g. `/ws/{userId}`).

**Architecture:** `AuthFlow.PathParams` (url-param→response dot-path) → `ResolveAuthHeader` captures them (reusing `extractByDotPath`) into an `AuthResult` → `auth_setup` stores on `Credentials.PathParams` → `WSProtocolIndex.ActorPathParams` carries them → `doConnect` substitutes `{param}` in the dial URL from the role's actor params.

**Tech Stack:** Go 1.25; existing `extractByDotPath`, `ResolveAuthHeader`, `WSProtocolIndex`, `doConnect`.

## Global Constraints
- Go 1.25, pure-Go; `coder/websocket v1.8.14` ONLY; no new deps; no expression evaluator.
- Secret hygiene: path params ride runtime-only Credentials fields (`yaml:"-"`); the token stays the only URL-scrubbed value; path params never in the prompt.
- Author `binoctal <binoctal@gmail.com>`; NO Co-Authored-By; English; docs only in `cerberus-docs/`; `make check` green.
- Backwards-compat: no `path_params` / no `{param}` ⇒ byte-identical behavior.

---

### Task 1: schema + auth-flow capture

**Files:**
- Modify: `internal/project/authflow_schema.go` (`AuthFlow` += `PathParams`)
- Modify: `internal/project/schema.go` (`CredentialRef` += `PathParams`, runtime-only)
- Modify: `internal/project/validate_auth.go` (validate `path_params` param names)
- Modify: `internal/head/agent/authflow.go` (`AuthResult` struct; `ResolveAuthHeader` returns it; capture path params)
- Modify: `internal/session/auth_setup.go` (store `Credentials.PathParams`)
- Tests: `internal/project/authflow_schema_test.go` (+ `validate_auth_test.go`), `internal/head/agent/authflow_test.go`, `internal/session/auth_setup_test.go` (if exists)

**Interfaces:**
- Produces: `AuthFlow.PathParams map[string]string`; `AuthResult{Name, Value, RawToken, PathParams}`; `ResolveAuthHeader(ctx, svcURL, actor) (*AuthResult, error)`; `CredentialRef.PathParams`.

**Reviewer note (controller):** sonnet. This is auth/secret-hygiene territory — verify: token never in errors/logs (unchanged), path params ride `yaml:"-"` fields, the signature change's caller/test ripple is complete, backwards-compat (no path_params ⇒ old behavior).

- [ ] **Step 1: Schema + validation**

`internal/project/authflow_schema.go` — add to `AuthFlow`:
```go
	// PathParams captures additional fields from the login response for URL
	// templating: url-param name -> response JSON dot-path (same syntax as
	// token_from). At WS connect, {name} placeholders in the service URL are
	// substituted from these. Empty ⇒ no path params (backwards-compatible).
	PathParams map[string]string `yaml:"path_params,omitempty"`
```

`internal/project/schema.go` — add to `CredentialRef` (runtime-only, after RawToken):
```go
	// PathParams holds url-param -> value captured by the auth flow (F3).
	// Runtime-only; never loaded from YAML.
	PathParams map[string]string `yaml:"-" json:"-"`
```

`internal/project/validate_auth.go` — validate each `path_params` KEY is a param name (`^[A-Za-z_][A-Za-z0-9_]*$`; reject otherwise). Dot-path VALUES are unconstrained (resolved at runtime). No change when `path_params` absent.

- [ ] **Step 2: AuthResult + capture**

`internal/head/agent/authflow.go` — change the signature + add the struct + capture:
```go
// AuthResult is the outcome of an actor's auth flow: the header to inject, the
// raw token (for WS query/header/subprotocol), and any captured URL path params.
type AuthResult struct {
	HeaderName  string
	HeaderValue string
	RawToken    string
	PathParams  map[string]string // url-param -> captured value (F3)
}

func ResolveAuthHeader(ctx context.Context, svcURL string, actor project.Actor) (*AuthResult, error) {
	af := actor.Auth
	if af == nil {
		return nil, fmt.Errorf("actor has no auth flow")
	}
	// ... (steps 1-4 unchanged through token extraction) ...
	token, err := extractByDotPath(decoded, af.TokenFrom)
	if err != nil {
		return nil, err
	}
	// F3: capture declared path params from the same response. An absent dot-path
	// yields "" (non-fatal; connect-time {param} resolution fails clearly).
	var pathParams map[string]string
	for name, dotPath := range af.PathParams {
		if pathParams == nil {
			pathParams = make(map[string]string)
		}
		if v, err := extractByDotPath(decoded, dotPath); err == nil {
			pathParams[name] = v
		} else {
			pathParams[name] = ""
		}
	}
	header := interpolate(af.InjectAs, map[string]string{"{token}": token})
	hName, hValue, ok := splitHeader(header)
	if !ok {
		return nil, fmt.Errorf("auth flow: inject_as %q is not a 'Name: Value' header", af.InjectAs)
	}
	return &AuthResult{HeaderName: hName, HeaderValue: hValue, RawToken: token, PathParams: pathParams}, nil
}
```
(Convert every `return "", '', '', err` in the function to `return nil, err`. Token-error messages stay token-free, unchanged.)

- [ ] **Step 3: auth_setup storage**

`internal/session/auth_setup.go:44` — the one production caller:
```go
	res, err := agent.ResolveAuthHeader(ctx, svcURL, *a)
	if err != nil { /* unchanged warn + continue */ }
	// ...
	a.Credentials.Headers[res.HeaderName] = res.HeaderValue
	a.Credentials.RawToken = res.RawToken
	a.Credentials.PathParams = res.PathParams // F3
```

- [ ] **Step 4: Tests**

- `authflow_test.go`: update the ~6 call sites to the struct (`res.HeaderName`, etc.). Add: a flow with `path_params: {userId: "config.userId"}` against a fake response `{config:{userId:"user_1", ...}}` ⇒ `res.PathParams["userId"] == "user_1"`; absent dot-path ⇒ `""`; no `path_params` ⇒ `res.PathParams == nil`.
- `authflow_schema_test.go` / `validate_auth_test.go`: `PathParams` YAML round-trip; bad param-name key rejected; absent ⇒ unchanged.
- (auth_setup storage is exercised by an existing or new session test if present; otherwise the authflow test covers capture.)

- [ ] **Step 5: Run + commit**

`go test -race -count=1 ./internal/project/ ./internal/head/agent/ ./internal/session/` then `make check`. Commit:
```bash
git commit -m "feat(ws): capture auth-flow path params (F3 schema + capture)"
```

---

### Task 2: index + connect-time URL templating

**Files:**
- Modify: `internal/head/agent/ws_protocol.go` (`WSProtocolIndex` += `ActorPathParams`; `BuildWSProtocolIndex` populates)
- Modify: `internal/head/agent/websocket.go` (`doConnect` substitutes `{param}`)
- Tests: `internal/head/agent/ws_protocol_test.go`, `internal/head/agent/websocket_test.go`

**Interfaces:**
- Produces: `WSProtocolIndex.ActorPathParams map[string]map[string]string`; `doConnect` resolves `{param}` in the dial URL from the role's actor params.

**Reviewer note (controller):** sonnet. Verify: templating applies AFTER role-param injection + BEFORE dial; the role's actor is the `credential_ref` resolved actor; a `{param}` with no captured value ⇒ dedicated error (not a silent wrong dial); `preInjectionURL` still strips only the token (userId may show — it's the endpoint).

- [ ] **Step 1: Index**

`internal/head/agent/ws_protocol.go` — add to `WSProtocolIndex` + populate in `BuildWSProtocolIndex`:
```go
type WSProtocolIndex struct {
	ByHost          map[string]*project.Protocol
	ActorTokens     map[string]string            // existing
	ActorPathParams map[string]map[string]string // actor -> {url-param: value} (F3)
}
```
In `BuildWSProtocolIndex`, where `ActorTokens[a.Name] = a.Credentials.RawToken`, add:
```go
	if len(a.Credentials.PathParams) > 0 {
		if idx.ActorPathParams == nil {
			idx.ActorPathParams = map[string]map[string]string{}
		}
		idx.ActorPathParams[a.Name] = a.Credentials.PathParams
	}
```
(Ensure `ActorPathParams` is initialized in the index constructor.)

- [ ] **Step 2: doConnect templating**

`internal/head/agent/websocket.go` — after role-param injection (and the `preInjectionURL` recompute) and before `websocket.Dial`, resolve `{param}` in `dialURL` from the resolved actor's path params. The actor is the `credentialRef` already resolved earlier in `doConnect`. Add:
```go
// resolveURLParams substitutes {param} placeholders in rawURL from params.
// A placeholder with no value returns an error naming it (clear failure over a
// silent wrong dial). No placeholders ⇒ rawURL unchanged.
func resolveURLParams(rawURL string, params map[string]string) (string, error) {
	out := rawURL
	for name, val := range params {
		out = strings.ReplaceAll(out, "{"+name+"}", val)
	}
	if i := strings.Index(out, "{"); i >= 0 {
		j := strings.Index(out[i:], "}")
		if j > 0 {
			return "", fmt.Errorf("ws connect: unresolved URL param %s", out[i:i+j+1])
		}
	}
	return out, nil
}
```
In `doConnect`, after role params + before dial (and recompute `preInjectionURL` from the templated `dialURL` so the echoed URL reflects the real path):
```go
	actorParams := e.pathParamsFor(credentialRef) // helper: idx.ActorPathParams[credentialRef]
	dialURL, perr := resolveURLParams(dialURL, actorParams)
	if perr != nil {
		return types.WSResult{OK: false, URL: preInjectionURL, Err: perr.Error(), Latency: time.Since(start)}
	}
	// recompute preInjectionURL from the templated dialURL (path id now present)
	if ap := maybeAuthParam(proto); ap != "" {
		preInjectionURL = stripQuery(dialURL, ap)
	} else {
		preInjectionURL = dialURL
	}
```
(`pathParamsFor` returns `e.idx.ActorPathParams[actor]` or nil. Place the templating after the existing role-param `preInjectionURL` recompute block, before `websocket.Dial`.)

- [ ] **Step 3: Tests**

- `ws_protocol_test.go`: `BuildWSProtocolIndex` populates `ActorPathParams` from an actor with `Credentials.PathParams`.
- `websocket_test.go`: a connect against a `{userId}` URL with an actor that has `userId` captured ⇒ dials the substituted URL (server-side assertion of the path) + `WSResult.URL` shows the substituted path (token still stripped); a `{param}` with no captured value ⇒ `OK:false` with "unresolved URL param".

- [ ] **Step 4: Run + commit**

`go test -race -count=1 ./internal/head/agent/` then `make check`. Commit:
```bash
git commit -m "feat(ws): connect-time URL path-param templating (F3)"
```

---

### Task 3: docs + prompt

**Files:**
- Modify: `cerberus-docs/executors/websocket.md` (URL templating note)
- Modify: `cerberus-docs/configuration/project.md` (AuthFlow `path_params`)

- [ ] **Step 1:** `websocket.md` under the protocol/URL material: a service URL may use `{param}` placeholders resolved at connect from the role's auth-flow `path_params` captures (e.g. `ws://h/ws/{userId}`). `project.md` AuthFlow section: document `path_params: {url-param: response-dot-path}`.
- [ ] **Step 2:** `make check` + commit `docs(ws): F3 dynamic URL path params`.

---

## Post-implementation (controller)
- [ ] **Whole-branch review (opus):** `25d70c4..HEAD`. Verify: secret hygiene (token only scrubbed; path params on `yaml:-` fields, never in prompt); backwards-compat (no path_params ⇒ identical); signature ripple complete; templating after role-params/before dial; unresolved `{param}` ⇒ dedicated error; constraints; `make check` green.
- [ ] **Finish:** ff-merge main + delete branch (NO push).
- [ ] **Memory + ledger:** F3 done — WS arc F1+F2+F3+F4 + Scout relay all complete.
