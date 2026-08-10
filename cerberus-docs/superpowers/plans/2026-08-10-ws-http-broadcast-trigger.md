# Mixed HTTP+WS Broadcast-Trigger Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a deterministic `http_request` step that interleaves an authenticated HTTP request into a WS Steps sequence to trigger a server push received on an open WS connection, closing the open-agents `/broadcast` HTTP→WS coverage gap.

**Architecture:** The web actor obtains a real JWT via a second authflow login (`http_login` → `/api/dev/login`), stored in a new index slot `ActorHTTPTokens`. The Steps runner resolves a new `http_request` step (URL/body placeholders + Bearer auth from that slot) into an `HTTPAction` dispatched to the existing `HTTPExecutor`. A new Protocol-level `http_triggers` declaration drives a deterministic generator emitting connect → http_request → receive.

**Tech Stack:** Go 1.25, `modernc.org/sqlite` (no CGo), module `github.com/binoctal/cerberus`.

## Global Constraints

- Commit author: `binoctal <binoctal@gmail.com>`, no `Co-Authored-By`.
- No CGo dependency.
- Code comments and commit messages in English; replies to the user in Simplified Chinese.
- Zero regression: configs without `http_login` / `http_triggers` produce byte-identical behavior.
- Tests: `make test` (or `go test -v -race ./...`). Live tests: `make integration-openagents`.
- Documentation only in `cerberus-docs/`, never `docs/`.

**Spec:** `cerberus-docs/superpowers/specs/2026-08-10-ws-http-broadcast-trigger-design.md`

---

## File Structure

- `internal/project/authflow_schema.go` — `AuthFlow.HTTPLogin`, `AuthFlow.HTTPTokenFrom` (Task 1).
- `internal/project/schema.go` — `CredentialRef.RawHTTPToken` (Task 1).
- `internal/project/validate_auth.go` — `http_login`/`http_token_from` consistency (Task 1).
- `internal/head/agent/authflow.go` — `AuthResult.HTTPToken`; `ResolveAuthHeader` runs `HTTPLogin` (Task 2).
- `internal/session/auth_setup.go` — write `RawHTTPToken` (Task 2).
- `internal/head/agent/ws_protocol.go` — `WSProtocolIndex.ActorHTTPTokens` (Task 3).
- `internal/head/agent/websocket.go` — extract `resolvePlaceholders` (Task 4).
- `internal/head/agent/types.go` — `TestStep` new fields (Task 5).
- `internal/head/agent/executor_types.go` + `executor_config.go` — `ReActLoop.wsIdx` (Task 5).
- `internal/session/run_phases_agent.go`, `internal/session/resume_phases_run.go` — pass `wsIdx` to loop config (Task 5).
- `internal/head/agent/execute_phases_steps.go` — `http_request` branch, `resolveHTTPStep`, status check, evidence (Task 5).
- `internal/project/protocol_schema.go` — `Protocol.HTTPTriggers`, `HTTPTrigger` (Task 6).
- `internal/project/validate_protocol.go` — `validateProtocolHTTPTriggers` (Task 6).
- `internal/head/scout/ws_cases.go` — `wsHTTPTriggerCases` (Task 7).
- `dogfood/ws-realtime/.cerberus/project.yaml`, `.../protocols/open-agents.yaml` (Task 8).
- `internal/head/agent/httptrigger_live_integration_test.go` (Task 9).
- `cerberus-docs/technical/validation/2026-08-07-openagents-integration-test-report.md` (Task 10).

---

### Task 1: Auth declaration — `http_login` + `http_token_from`

**Files:**
- Modify: `internal/project/authflow_schema.go` (add fields to `AuthFlow`)
- Modify: `internal/project/schema.go:52` (`CredentialRef` struct)
- Modify: `internal/project/validate_auth.go` (consistency check)
- Test: `internal/project/validate_auth_test.go`

**Interfaces:**
- Produces: `AuthFlow.HTTPLogin *AuthLogin`, `AuthFlow.HTTPTokenFrom string`, `CredentialRef.RawHTTPToken string`. Task 2 consumes `HTTPLogin`/`HTTPTokenFrom`; Task 3 consumes `RawHTTPToken`.

- [ ] **Step 1: Write the failing test**

Append to `internal/project/validate_auth_test.go`:

```go
func TestValidateAuthFlow_HTTPLogin(t *testing.T) {
	t.Run("valid http_login", func(t *testing.T) {
		af := &AuthFlow{
			Login:        AuthLogin{Method: "POST", Path: "/api/dev/setup"},
			TokenFrom:    "config.deviceToken",
			InjectAs:     "Authorization: Bearer {token}",
			HTTPLogin:    &AuthLogin{Method: "POST", Path: "/api/dev/login", Body: map[string]string{}},
			HTTPTokenFrom: "token",
		}
		if err := ValidateAuthFlow(af); err != nil {
			t.Fatalf("expected valid, got %v", err)
		}
	})
	t.Run("http_login without http_token_from fails", func(t *testing.T) {
		af := &AuthFlow{
			Login:     AuthLogin{Method: "POST", Path: "/api/dev/setup"},
			TokenFrom: "config.deviceToken",
			InjectAs:  "Authorization: Bearer {token}",
			HTTPLogin: &AuthLogin{Method: "POST", Path: "/api/dev/login"},
		}
		if err := ValidateAuthFlow(af); err == nil {
			t.Fatal("expected error for http_login without http_token_from")
		}
	})
	t.Run("http_token_from without http_login fails", func(t *testing.T) {
		af := &AuthFlow{
			Login:        AuthLogin{Method: "POST", Path: "/api/dev/setup"},
			TokenFrom:    "config.deviceToken",
			InjectAs:     "Authorization: Bearer {token}",
			HTTPTokenFrom: "token",
		}
		if err := ValidateAuthFlow(af); err == nil {
			t.Fatal("expected error for http_token_from without http_login")
		}
	})
	t.Run("http_login empty path fails", func(t *testing.T) {
		af := &AuthFlow{
			Login:        AuthLogin{Method: "POST", Path: "/api/dev/setup"},
			TokenFrom:    "config.deviceToken",
			InjectAs:     "Authorization: Bearer {token}",
			HTTPLogin:    &AuthLogin{Method: "POST", Path: ""},
			HTTPTokenFrom: "token",
		}
		if err := ValidateAuthFlow(af); err == nil {
			t.Fatal("expected error for http_login with empty path")
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestValidateAuthFlow_HTTPLogin ./internal/project/`
Expected: FAIL — the fields do not exist (compile error).

- [ ] **Step 3: Add the schema fields**

In `internal/project/authflow_schema.go`, add two fields to `AuthFlow` (after `PathParams`):

```go
	// HTTPLogin is an optional SECOND login request run after the primary login,
	// used to obtain a credential for HTTP routes that the primary login's token
	// cannot satisfy (e.g. a JWT for protected REST routes when the primary token
	// is a WS-only backdoor). Its captured token is stored separately from the WS
	// token and injected by http_request steps as Authorization: Bearer <token>.
	// Empty ⇒ no second login (backwards-compatible).
	HTTPLogin *AuthLogin `yaml:"http_login,omitempty"`
	// HTTPTokenFrom is the dot-path into the http_login response JSON that yields
	// the HTTP credential (e.g. "token"). Required iff HTTPLogin is set.
	HTTPTokenFrom string `yaml:"http_token_from,omitempty"`
```

In `internal/project/schema.go`, add to the `CredentialRef` struct (after `RawToken`):

```go
	// RawHTTPToken is the HTTP credential captured by the optional http_login
	// (distinct from RawToken, which is the WS credential). Populated at session
	// setup; read by the Steps runner to inject http_request Authorization headers.
	RawHTTPToken string `yaml:"-" json:"-"`
```

- [ ] **Step 4: Add the validation**

In `internal/project/validate_auth.go`, append to `ValidateAuthFlow` (before `return nil`):

```go
	// http_login / http_token_from must both be set or both be unset; an
	// http_login needs a path (method defaults to POST at runtime, like the
	// primary login).
	if (af.HTTPLogin != nil) != (af.HTTPTokenFrom != "") {
		return errors.New("http_login and http_token_from must both be set or both be unset")
	}
	if af.HTTPLogin != nil && af.HTTPLogin.Path == "" {
		return errors.New("http_login.path is required when http_login is set")
	}
```

- [ ] **Step 5: Run tests to verify pass**

Run: `go test ./internal/project/`
Expected: PASS (all existing + new).

- [ ] **Step 6: Commit**

```bash
git add internal/project/authflow_schema.go internal/project/schema.go internal/project/validate_auth.go internal/project/validate_auth_test.go
git commit -m "feat(auth): add optional http_login to AuthFlow for HTTP JWT capture"
```

---

### Task 2: authflow execution — run `http_login`, capture `HTTPToken`

**Files:**
- Modify: `internal/head/agent/authflow.go` (`AuthResult`, `ResolveAuthHeader`)
- Modify: `internal/session/auth_setup.go:57` (write `RawHTTPToken`)
- Test: `internal/head/agent/authflow_test.go`

**Interfaces:**
- Consumes: `AuthFlow.HTTPLogin`/`HTTPTokenFrom` (Task 1).
- Produces: `AuthResult.HTTPToken` (string); `auth_setup.go` writes it to `Credentials.RawHTTPToken`. Task 3 reads `RawHTTPToken`.

- [ ] **Step 1: Write the failing test**

Append to `internal/head/agent/authflow_test.go`. The test stands up an `httptest.Server` whose `/primary` returns a setup-style body and whose `/login` returns `{token: <jwt>}`:

```go
func TestResolveAuthHeader_HTTPLogin(t *testing.T) {
	var primaryHits, loginHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/primary":
			primaryHits++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"config": map[string]any{"userId": "user_1", "deviceToken": "dt"},
			})
		case "/login":
			loginHits++
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "JWT-123"})
		}
	}))
	defer srv.Close()

	actor := project.Actor{
		Name: "web-actor",
		Auth: &project.AuthFlow{
			Login:     project.AuthLogin{Method: "POST", Path: "/primary", Body: map[string]string{}},
			TokenFrom: "config.deviceToken",
			InjectAs:  "Authorization: Bearer {token}",
			PathParams: map[string]string{"userId": "config.userId"},
			HTTPLogin:    &project.AuthLogin{Method: "POST", Path: "/login", Body: map[string]string{}},
			HTTPTokenFrom: "token",
		},
	}

	res, err := ResolveAuthHeader(context.Background(), srv.URL, actor)
	if err != nil {
		t.Fatalf("ResolveAuthHeader: %v", err)
	}
	if res.HTTPToken != "JWT-123" {
		t.Fatalf("HTTPToken = %q, want JWT-123", res.HTTPToken)
	}
	if loginHits != 1 {
		t.Fatalf("http_login hit %d times, want 1", loginHits)
	}
	// WS token still resolved from the primary login (token_from), unchanged.
	if res.RawToken != "dt" {
		t.Fatalf("RawToken = %q, want dt", res.RawToken)
	}
}

func TestResolveAuthHeader_NoHTTPLogin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "dt"})
	}))
	defer srv.Close()
	actor := project.Actor{
		Name: "bridge-actor",
		Auth: &project.AuthFlow{
			Login:     project.AuthLogin{Method: "POST", Path: "/", Body: map[string]string{}},
			TokenFrom: "token", InjectAs: "Authorization: Bearer {token}",
		},
	}
	res, err := ResolveAuthHeader(context.Background(), srv.URL, actor)
	if err != nil {
		t.Fatalf("ResolveAuthHeader: %v", err)
	}
	if res.HTTPToken != "" {
		t.Fatalf("HTTPToken = %q, want empty (no http_login)", res.HTTPToken)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestResolveAuthHeader_HTTPLogin ./internal/head/agent/`
Expected: FAIL — `AuthResult.HTTPToken` does not exist (compile error).

- [ ] **Step 3: Add `HTTPToken` to `AuthResult`**

In `internal/head/agent/authflow.go`, add to `AuthResult` (after `PathParams`):

```go
	// HTTPToken is the credential captured from the optional http_login (empty
	// when no http_login is declared). It is an HTTP-route credential distinct
	// from RawToken (the WS credential); never logged.
	HTTPToken string
```

- [ ] **Step 4: Refactor the login send into a helper and run `http_login`**

Extract the primary login's request/response decode into a reusable helper, then call it again for `HTTPLogin`. Add above `ResolveAuthHeader`:

```go
// sendLogin performs one login HTTP request and returns the decoded JSON
// response. It is shared by the primary login and the optional http_login so
// both build the URL, body, and headers identically. bodyVars interpolates
// {email}/{password}/{token} into the declared body field values.
func sendLogin(ctx context.Context, svcURL string, login AuthLogin, bodyVars map[string]string) (map[string]any, error) {
	bodyFields := make(map[string]string, len(login.Body))
	for k, v := range login.Body {
		bodyFields[k] = interpolate(v, bodyVars)
	}
	loginURL := login.Path
	if !isAbsoluteURL(loginURL) {
		var base string
		if u, err := url.Parse(svcURL); err == nil && u.IsAbs() {
			base = u.Scheme + "://" + u.Host
		} else {
			base = strings.TrimRight(svcURL, "/")
		}
		loginURL = base + "/" + strings.TrimLeft(loginURL, "/")
	}
	var bodyReader io.Reader
	if len(bodyFields) > 0 {
		encoded, err := json.Marshal(bodyFields)
		if err != nil {
			return nil, fmt.Errorf("auth flow: encode login body: %w", err)
		}
		bodyReader = strings.NewReader(string(encoded))
	}
	method := login.Method
	if method == "" {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, loginURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("auth flow: build request: %w", err)
	}
	if len(bodyFields) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range login.Headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("auth flow: login request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("auth flow: login returned status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("auth flow: read response: %w", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("auth flow: response is not a JSON object")
	}
	return decoded, nil
}
```

In `ResolveAuthHeader`, replace steps 1–3 (the inline request build/send/decode) with a call to the helper, then add the `http_login` capture before the final `return`. The result construction becomes:

```go
	// 1-3. Send the primary login.
	vars := map[string]string{
		"{email}":    actor.Credentials.Email,
		"{password}": actor.Credentials.Password,
	}
	decoded, err := sendLogin(ctx, svcURL, af.Login, vars)
	if err != nil {
		return nil, err
	}

	// 4. Token resolution (unchanged logic).
	token := actor.Credentials.Token
	if af.TokenFrom != "" {
		t, err := extractByDotPath(decoded, af.TokenFrom)
		if err != nil {
			return nil, err
		}
		token = t
	}

	// 5. PathParams capture (unchanged logic).
	var pathParams map[string]string
	for name, dotPath := range af.PathParams {
		if pathParams == nil {
			pathParams = make(map[string]string)
		}
		if v, pErr := extractByDotPath(decoded, dotPath); pErr == nil {
			pathParams[name] = v
		} else {
			pathParams[name] = ""
		}
	}

	// 6. Optional http_login: run AFTER the primary login so its prerequisites
	// (e.g. a user created by setup) exist, then capture the HTTP credential.
	var httpToken string
	if af.HTTPLogin != nil {
		httpDecoded, err := sendLogin(ctx, svcURL, *af.HTTPLogin, vars)
		if err != nil {
			return nil, err
		}
		httpToken, err = extractByDotPath(httpDecoded, af.HTTPTokenFrom)
		if err != nil {
			return nil, err
		}
	}

	// 7. Interpolate {token} into inject_as (unchanged).
	header := interpolate(af.InjectAs, map[string]string{"{token}": token})
	hName, hValue, ok := splitHeader(header)
	if !ok {
		return nil, fmt.Errorf("auth flow: inject_as %q is not a 'Name: Value' header", af.InjectAs)
	}
	return &AuthResult{HeaderName: hName, HeaderValue: hValue, RawToken: token, PathParams: pathParams, HTTPToken: httpToken}, nil
```

Remove now-unused imports/locals if the compiler flags them (`bodyFields`, the old inline `req` build). Keep `isAbsoluteURL`, `extractByDotPath`, `interpolate`, `splitHeader` as-is.

- [ ] **Step 5: Write `RawHTTPToken` in session setup**

In `internal/session/auth_setup.go`, after the line `a.Credentials.RawToken = res.RawToken` (≈ line 57), add:

```go
		a.Credentials.RawHTTPToken = res.HTTPToken
```

- [ ] **Step 6: Run tests to verify pass**

Run: `go test ./internal/head/agent/ ./internal/session/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/head/agent/authflow.go internal/session/auth_setup.go internal/head/agent/authflow_test.go
git commit -m "feat(authflow): run optional http_login and capture HTTP token"
```

---

### Task 3: Index slot — `WSProtocolIndex.ActorHTTPTokens`

**Files:**
- Modify: `internal/head/agent/ws_protocol.go` (`WSProtocolIndex`, `BuildWSProtocolIndex`)
- Test: `internal/head/agent/ws_protocol_test.go`

**Interfaces:**
- Consumes: `Credentials.RawHTTPToken` (Task 1).
- Produces: `WSProtocolIndex.ActorHTTPTokens map[string]string`. Task 5 reads it in `resolveHTTPStep`.

- [ ] **Step 1: Write the failing test**

Append to `internal/head/agent/ws_protocol_test.go` (if the file builds an index from a hand-constructed `Config`, mirror its existing pattern; otherwise construct one inline):

```go
func TestBuildWSProtocolIndex_ActorHTTPTokens(t *testing.T) {
	cfg := &project.Config{
		Services: []project.Service{{
			Name:     "realtime",
			URL:      "http://h/ws/{userId}",
			Protocol: &project.Protocol{Roles: map[string]*project.ProtocolRole{"web": {}}},
		}},
		Actors: []project.Actor{{
			Name: "web-actor",
			Credentials: project.CredentialRef{RawHTTPToken: "JWT-9", RawToken: "demo"},
		}},
	}
	idx := BuildWSProtocolIndex(cfg)
	if idx == nil {
		t.Fatal("expected non-nil index")
	}
	if idx.ActorHTTPTokens["web-actor"] != "JWT-9" {
		t.Fatalf("ActorHTTPTokens[web-actor] = %q, want JWT-9", idx.ActorHTTPTokens["web-actor"])
	}
	if idx.ActorTokens["web-actor"] != "demo" {
		t.Fatalf("ActorTokens[web-actor] = %q, want demo (unchanged)", idx.ActorTokens["web-actor"])
	}
}
```

(Adjust the `Config`/`Service`/`Actor` field literals to match the real struct shapes in `internal/project/schema.go` — `Service.URL`, `Service.Protocol`, `Protocol.Roles`, `Actor.Name`, `Actor.Credentials` are the fields used.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestBuildWSProtocolIndex_ActorHTTPTokens ./internal/head/agent/`
Expected: FAIL — `ActorHTTPTokens` does not exist (compile error).

- [ ] **Step 3: Add the field and populate it**

In `internal/head/agent/ws_protocol.go`, add to `WSProtocolIndex` (after `ActorPathParams`):

```go
	// ActorHTTPTokens maps actor name -> the HTTP credential captured by an
	// optional http_login (distinct from ActorTokens, the WS credential). Read
	// by the Steps runner to inject http_request Authorization headers.
	ActorHTTPTokens map[string]string
```

Initialize it in `BuildWSProtocolIndex` where the index is constructed:

```go
			idx = &WSProtocolIndex{
				ByHost:          make(map[string]*project.Protocol),
				ActorTokens:     make(map[string]string),
				ActorPathParams: make(map[string]map[string]string),
				ActorHTTPTokens: make(map[string]string),
			}
```

In the actor loop (after the `ActorPathParams` population block), add:

```go
		// HTTP credential from the optional http_login; only stashed when
		// non-empty so a legacy config (no http_login) leaves the slot absent.
		if a.Credentials.RawHTTPToken != "" {
			idx.ActorHTTPTokens[a.Name] = a.Credentials.RawHTTPToken
		}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./internal/head/agent/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/ws_protocol.go internal/head/agent/ws_protocol_test.go
git commit -m "feat(ws-idx): index captured HTTP tokens per actor"
```

---

### Task 4: Shared placeholder resolver — `resolvePlaceholders`

**Files:**
- Modify: `internal/head/agent/websocket.go` (`resolveMessageBody` delegates)
- Test: `internal/head/agent/websocket_test.go` (existing resolver tests must still pass)

**Interfaces:**
- Produces: `resolvePlaceholders(idx *WSProtocolIndex, proto *project.Protocol, owningActor, text string) (string, error)`. Task 5 calls it for URL/body resolution. `resolveMessageBody` delegates with `owningActor = entry.credentialRef`.

- [ ] **Step 1: Write the failing test**

Append to `internal/head/agent/websocket_test.go`:

```go
func TestResolvePlaceholders_CrossActorURL(t *testing.T) {
	idx := &WSProtocolIndex{
		ActorPathParams: map[string]map[string]string{
			"bridge-actor": {"deviceId": "device_xyz"},
		},
	}
	proto := &project.Protocol{
		Roles: map[string]*project.ProtocolRole{
			"bridge": {CredentialRef: "bridge-actor"},
		},
	}
	out, err := resolvePlaceholders(idx, proto, "", "/api/devices/{{bridge.deviceId}}/restart")
	if err != nil {
		t.Fatalf("resolvePlaceholders: %v", err)
	}
	if out != "/api/devices/device_xyz/restart" {
		t.Fatalf("got %q", out)
	}
}

func TestResolvePlaceholders_UnresolvedFails(t *testing.T) {
	idx := &WSProtocolIndex{ActorPathParams: map[string]map[string]string{}}
	proto := &project.Protocol{Roles: map[string]*project.ProtocolRole{
		"bridge": {CredentialRef: "bridge-actor"},
	}}
	_, err := resolvePlaceholders(idx, proto, "", "/api/devices/{{bridge.deviceId}}/restart")
	if err == nil {
		t.Fatal("expected unresolved placeholder error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestResolvePlaceholders ./internal/head/agent/`
Expected: FAIL — `resolvePlaceholders` undefined.

- [ ] **Step 3: Extract the free function**

In `internal/head/agent/websocket.go`, extract the body of `resolveMessageBody` (lines ≈ 764–809) into a free function that takes `owningActor` instead of reading `entry.credentialRef`:

```go
// resolvePlaceholders substitutes {{param}} / {{role.param}} placeholders in
// text against provisioned actor state: {{param}} reads the owning actor's
// captured path params; {{role.param}} reads the named declared role's actor
// params (cross-actor). A declared-role or owning-actor placeholder with no
// captured value is a hard error; a dot placeholder whose role is NOT declared
// is left literal. Text with no {{ is returned verbatim. This is the shared
// resolver for ws_send bodies and http_request URL/body templates.
func resolvePlaceholders(idx *WSProtocolIndex, proto *project.Protocol, owningActor, text string) (string, error) {
	if !strings.Contains(text, "{{") {
		return text, nil
	}
	var own map[string]string
	if idx != nil && owningActor != "" {
		own = idx.ActorPathParams[owningActor]
	}
	var unresolved string
	out := wsBodyPlaceholderRe.ReplaceAllStringFunc(text, func(match string) string {
		token := match[2 : len(match)-2]
		if i := strings.IndexByte(token, '.'); i > 0 {
			role, param := token[:i], token[i+1:]
			if proto != nil {
				if r, ok := proto.Roles[role]; ok && r != nil {
					if r.CredentialRef != "" && idx != nil {
						if v, ok := idx.ActorPathParams[r.CredentialRef][param]; ok {
							return v
						}
					}
					if unresolved == "" {
						unresolved = match
					}
					return match
				}
			}
			return match
		}
		if own != nil {
			if v, ok := own[token]; ok {
				return v
			}
		}
		if unresolved == "" {
			unresolved = match
		}
		return match
	})
	if unresolved != "" {
		return "", fmt.Errorf("unresolved placeholder %s", unresolved)
	}
	return out, nil
}
```

Rewrite `resolveMessageBody` to delegate:

```go
func (e *WebSocketExecutor) resolveMessageBody(entry *wsEntry, msg string) (string, error) {
	return resolvePlaceholders(e.idx, entry.protocol, entry.credentialRef, msg)
}
```

(Keep the `wsBodyPlaceholderRe` definition and the doc comment on `resolveMessageBody` describing the ws_send contract.)

- [ ] **Step 4: Run ALL resolver tests**

Run: `go test -run "TestResolveMessageBody|TestResolvePlaceholders" ./internal/head/agent/`
Expected: PASS — the existing `resolveMessageBody` unit tests pass unchanged (behavior preserved), and the new `resolvePlaceholders` tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/websocket.go internal/head/agent/websocket_test.go
git commit -m "refactor(ws): extract resolvePlaceholders shared by ws_send and http_request"
```

---

### Task 5: The `http_request` step (runner)

**Files:**
- Modify: `internal/head/agent/types.go:76` (`TestStep`)
- Modify: `internal/head/agent/executor_types.go:28` (`ReActLoop`), `internal/head/agent/executor_config.go` (`ReActLoopConfig`)
- Modify: `internal/session/run_phases_agent.go:20`, `internal/session/resume_phases_run.go:26`
- Modify: `internal/head/agent/execute_phases_steps.go`
- Test: `internal/head/agent/execute_phases_steps_test.go`

**Interfaces:**
- Consumes: `WSProtocolIndex.ActorHTTPTokens` (Task 3), `resolvePlaceholders` (Task 4), `ReActLoop.wsIdx`.
- Produces: a runnable `http_request` TestStep action; `TestStep.Method/Headers/Body/ExpectStatus/AuthRole`.

- [ ] **Step 1: Add `TestStep` fields**

In `internal/head/agent/types.go`, append to `TestStep` (after `ExpectAbsent`):

```go
	// http_request: HTTP method (GET/POST/...). Defaults to GET when empty.
	Method string `json:"method,omitempty"`
	// http_request: explicit request headers (e.g. an injected Authorization).
	// When AuthRole is also set, explicit Headers override the auth header.
	Headers map[string]string `json:"headers,omitempty"`
	// http_request: request body (raw string, typically JSON).
	Body string `json:"body,omitempty"`
	// http_request: expected response status; 0 ⇒ do not assert (rely on the
	// executor's own success/ok gate).
	ExpectStatus int `json:"expect_status,omitempty"`
	// http_request: a declared role whose actor's HTTP token (http_login) is
	// injected as Authorization: Bearer <token>. Empty ⇒ no auth injection
	// (Headers must supply auth, if needed).
	AuthRole string `json:"auth_role,omitempty"`
```

- [ ] **Step 2: Add `ReActLoop.wsIdx` and wire it**

In `internal/head/agent/executor_types.go`, add to the `ReActLoop` struct (after `executor`):

```go
	wsIdx *WSProtocolIndex // index for http_request step resolution; nil ⇒ no http triggers
```

In `internal/head/agent/executor_config.go`, add to `ReActLoopConfig` (after `Executor`):

```go
	WSIdx *WSProtocolIndex
```

and in `NewReActLoopWithGateWithConfig`, add to the `&ReActLoop{...}` literal:

```go
		wsIdx:        cfg.WSIdx,
```

In `internal/session/run_phases_agent.go:20`, extract the index into a var and pass it to both calls. Replace:

```go
	multiExec := agent.BuildMultiExecutor(projectDir, agent.ServiceHeadersMap(rp.session.Config.Services), agent.BuildWSProtocolIndex(rp.session.Config), rp.session.Gate, rp.session.Logger)
```

with:

```go
	wsIdx := agent.BuildWSProtocolIndex(rp.session.Config)
	multiExec := agent.BuildMultiExecutor(projectDir, agent.ServiceHeadersMap(rp.session.Config.Services), wsIdx, rp.session.Gate, rp.session.Logger)
```

and add `WSIdx: wsIdx,` to the `agent.ReActLoopConfig{...}` literal built in that file (locate the `ReActLoopConfig` literal; set the field). Apply the identical change in `internal/session/resume_phases_run.go:26`.

- [ ] **Step 3: Write the failing test for `resolveHTTPStep`**

Append to `internal/head/agent/execute_phases_steps_test.go`:

```go
func TestResolveHTTPStep(t *testing.T) {
	idx := &WSProtocolIndex{
		ByHost:          map[string]*project.Protocol{"localhost:8989": nil},
		ActorPathParams: map[string]map[string]string{"bridge-actor": {"deviceId": "device_xyz"}},
		ActorHTTPTokens: map[string]string{"web-actor": "JWT-1"},
	}
	proto := &project.Protocol{Roles: map[string]*project.ProtocolRole{
		"web":    {CredentialRef: "web-actor"},
		"bridge": {CredentialRef: "bridge-actor"},
	}}
	idx.ByHost["localhost:8989"] = proto

	t.Run("auth + url placeholder", func(t *testing.T) {
		s := TestStep{
			Action: "http_request", Method: "POST",
			URL: "http://localhost:8989/api/devices/{{bridge.deviceId}}/restart",
			AuthRole: "web", ExpectStatus: 200,
		}
		a, err := resolveHTTPStep(idx, &TestCase{}, s)
		if err != nil {
			t.Fatalf("resolveHTTPStep: %v", err)
		}
		ha, ok := a.(types.HTTPAction)
		if !ok {
			t.Fatalf("got %T, want HTTPAction", a)
		}
		if ha.URL != "http://localhost:8989/api/devices/device_xyz/restart" {
			t.Fatalf("URL = %q", ha.URL)
		}
		if ha.Headers["Authorization"] != "Bearer JWT-1" {
			t.Fatalf("Authorization = %q", ha.Headers["Authorization"])
		}
		if ha.Method != "POST" {
			t.Fatalf("Method = %q", ha.Method)
		}
	})
	t.Run("explicit header overrides auth", func(t *testing.T) {
		s := TestStep{Action: "http_request", URL: "http://localhost:8989/x",
			AuthRole: "web", Headers: map[string]string{"Authorization": "Bearer OVERRIDE"}}
		a, err := resolveHTTPStep(idx, &TestCase{}, s)
		if err != nil {
			t.Fatalf("resolveHTTPStep: %v", err)
		}
		if a.(types.HTTPAction).Headers["Authorization"] != "Bearer OVERRIDE" {
			t.Fatalf("expected explicit override")
		}
	})
	t.Run("missing http token fails", func(t *testing.T) {
		s := TestStep{Action: "http_request", URL: "http://localhost:8989/x", AuthRole: "bridge"}
		_, err := resolveHTTPStep(idx, &TestCase{}, s)
		if err == nil {
			t.Fatal("expected error: bridge has no http token")
		}
	})
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `go test -run TestResolveHTTPStep ./internal/head/agent/`
Expected: FAIL — `resolveHTTPStep` undefined.

- [ ] **Step 5: Implement `resolveHTTPStep`**

In `internal/head/agent/execute_phases_steps.go`, add the function (and a URL-host helper):

```go
// resolveHTTPStep turns an http_request TestStep into a dispatchable HTTPAction.
// URL and Body {{param}}/{{role.param}} placeholders resolve from provisioned
// actor state (resolvePlaceholders); AuthRole's actor HTTP token is injected as
// "Authorization: Bearer <token>" unless an explicit Authorization header is
// present (explicit headers win). The protocol is looked up by the URL host.
func resolveHTTPStep(idx *WSProtocolIndex, tc *TestCase, s TestStep) (types.TypedAction, error) {
	method := s.Method
	if method == "" {
		method = "GET"
	}
	proto := protocolForURL(idx, s.URL)
	owningActor := ""
	if proto != nil && s.AuthRole != "" {
		if r := proto.Roles[s.AuthRole]; r != nil {
			owningActor = r.CredentialRef
		}
	}
	url, err := resolvePlaceholders(idx, proto, owningActor, s.URL)
	if err != nil {
		return nil, fmt.Errorf("http_request: %w", err)
	}
	body := s.Body
	if body != "" {
		if b, berr := resolvePlaceholders(idx, proto, owningActor, body); berr == nil {
			body = b
		} else {
			return nil, fmt.Errorf("http_request: %w", berr)
		}
	}
	headers := map[string]string{}
	for k, v := range s.Headers {
		headers[k] = v
	}
	if s.AuthRole != "" && owningActor != "" && idx != nil {
		if tok := idx.ActorHTTPTokens[owningActor]; tok != "" {
			if _, set := headers["Authorization"]; !set {
				headers["Authorization"] = "Bearer " + tok
			}
		} else {
			return nil, fmt.Errorf("http_request: no http token for actor %q", owningActor)
		}
	}
	return types.HTTPAction{Method: method, URL: url, Headers: headers, Body: body}, nil
}

// protocolForURL returns the declared protocol for a URL's host, or nil.
func protocolForURL(idx *WSProtocolIndex, rawURL string) *project.Protocol {
	if idx == nil {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	return idx.ByHost[u.Host]
}
```

Add the needed imports to the file: `"net/url"`, `"github.com/binoctal/cerberus/internal/project"`, `"github.com/binoctal/cerberus/internal/types"` (types likely already imported).

- [ ] **Step 6: Wire `http_request` into `runSteps` + status check + evidence**

In `execute_phases_steps.go` `runSteps`, change the per-step action resolution and post-execute check. Replace the block:

```go
		action, err := stepToAction(se.tc, s)
		if err != nil {
			return se.failureResult(err, 1)
		}
```

with:

```go
		var action types.TypedAction
		var err error
		if s.Action == "http_request" {
			action, err = resolveHTTPStep(r.wsIdx, se.tc, s)
		} else {
			action, err = stepToAction(se.tc, s)
		}
		if err != nil {
			return se.failureResult(err, 1)
		}
```

Then after `result := r.executor.Execute(...)` and `evidence = append(...)`, before the `!result.Success()` check, add the explicit status assertion:

```go
		// http_request explicit status assertion: when expect_status is set, a
		// non-matching status fails the step regardless of the executor's own
		// success/ok gate.
		if s.Action == "http_request" && s.ExpectStatus != 0 {
			if hr, ok := result.(types.HTTPResult); ok && hr.StatusCode != s.ExpectStatus {
				return StepResult{TestCase: se.tc, Status: StepFailed, TraceID: se.traceID,
					Attempts: 1, Duration: time.Since(se.start), Action: action, Result: result, Evidence: evidence}
			}
		}
```

Add an `http_request` branch to `stepEvidence` (after the `ws_send` branch):

```go
	if s.Action == "http_request" {
		if hr, ok := result.(types.HTTPResult); ok {
			ev.Content = fmt.Sprintf("http_request: %s %d", hr.URL, hr.StatusCode)
		}
	}
```

- [ ] **Step 7: Run tests to verify pass**

Run: `go test ./internal/head/agent/ ./internal/session/`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/head/agent/types.go internal/head/agent/executor_types.go internal/head/agent/executor_config.go internal/session/run_phases_agent.go internal/session/resume_phases_run.go internal/head/agent/execute_phases_steps.go internal/head/agent/execute_phases_steps_test.go
git commit -m "feat(steps): add http_request step with placeholder + auth resolution"
```

---

### Task 6: Protocol declaration — `http_triggers`

**Files:**
- Modify: `internal/project/protocol_schema.go` (`Protocol`, new `HTTPTrigger` type)
- Modify: `internal/project/validate_protocol.go` (`validateProtocolHTTPTriggers`, wire into `ValidateProtocol`)
- Test: `internal/project/validate_protocol_test.go`

**Interfaces:**
- Consumes: `Protocol.Roles` (declared role set).
- Produces: `Protocol.HTTPTriggers []*HTTPTrigger`. Task 7 reads it.

- [ ] **Step 1: Write the failing test**

Append to `internal/project/validate_protocol_test.go`:

```go
func TestValidateProtocol_HTTPTriggers(t *testing.T) {
	roles := map[string]*ProtocolRole{"web": {}, "bridge": {}}
	t.Run("valid", func(t *testing.T) {
		p := &Protocol{Roles: roles, HTTPTriggers: []*HTTPTrigger{{
			ID: "device-restart",
			Request:  HTTPTriggerRequest{Method: "POST", Path: "/api/devices/{{bridge.deviceId}}/restart", AuthRole: "web", ExpectStatus: 200},
			Effect:   HTTPTriggerEffect{MessageType: "device:restart", ToRole: "web"},
		}}}
		if err := ValidateProtocol(p, nil); err != nil {
			t.Fatalf("expected valid, got %v", err)
		}
	})
	t.Run("undeclared auth_role fails", func(t *testing.T) {
		p := &Protocol{Roles: roles, HTTPTriggers: []*HTTPTrigger{{
			ID: "x", Request: HTTPTriggerRequest{Method: "POST", Path: "/p", AuthRole: "ghost"},
			Effect: HTTPTriggerEffect{MessageType: "t", ToRole: "web"},
		}}}
		if err := ValidateProtocol(p, nil); err == nil {
			t.Fatal("expected error for undeclared auth_role")
		}
	})
	t.Run("undeclared to_role fails", func(t *testing.T) {
		p := &Protocol{Roles: roles, HTTPTriggers: []*HTTPTrigger{{
			ID: "x", Request: HTTPTriggerRequest{Method: "POST", Path: "/p", AuthRole: "web"},
			Effect: HTTPTriggerEffect{MessageType: "t", ToRole: "ghost"},
		}}}
		if err := ValidateProtocol(p, nil); err == nil {
			t.Fatal("expected error for undeclared to_role")
		}
	})
	t.Run("empty id/method/path fails", func(t *testing.T) {
		p := &Protocol{Roles: roles, HTTPTriggers: []*HTTPTrigger{{
			Request: HTTPTriggerRequest{AuthRole: "web"}, Effect: HTTPTriggerEffect{MessageType: "t", ToRole: "web"},
		}}}
		if err := ValidateProtocol(p, nil); err == nil {
			t.Fatal("expected error for empty id/method/path")
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestValidateProtocol_HTTPTriggers ./internal/project/`
Expected: FAIL — `HTTPTriggers`/`HTTPTrigger` undefined (compile error).

- [ ] **Step 3: Add the schema**

In `internal/project/protocol_schema.go`, add a field to `Protocol` (after `Batches`):

```go
	// HTTPTriggers declares HTTP routes that trigger a WS message push when hit
	// (a public HTTP route whose handler fans a message out to WS clients via the
	// DO /broadcast). Each trigger drives one deterministic Steps case
	// (connect the recipient → http_request → receive the pushed type). Empty ⇒
	// no trigger cases (backwards-compatible).
	HTTPTriggers []*HTTPTrigger `yaml:"http_triggers,omitempty"`
```

Add the trigger types (after the `ProtocolBatch` struct):

```go
// HTTPTrigger declares one HTTP→WS push trigger for the deterministic generator.
type HTTPTrigger struct {
	ID      string             `yaml:"id"`
	Request HTTPTriggerRequest `yaml:"request"`
	Effect  HTTPTriggerEffect  `yaml:"effect"`
}

// HTTPTriggerRequest describes the HTTP request that triggers the push.
type HTTPTriggerRequest struct {
	Method       string `yaml:"method"`        // HTTP method; defaults to POST at generation
	Path         string `yaml:"path"`          // host-relative (e.g. /api/devices/{{bridge.deviceId}}/restart)
	AuthRole     string `yaml:"auth_role"`     // declared role whose actor's http_login token authorizes the request
	ExpectStatus int    `yaml:"expect_status"` // expected response status; 0 ⇒ no assertion
}

// HTTPTriggerEffect describes the WS message the push delivers.
type HTTPTriggerEffect struct {
	MessageType string `yaml:"message_type"` // routing type received on the WS connection
	ToRole      string `yaml:"to_role"`      // declared role whose connection receives it
}
```

- [ ] **Step 4: Add validation and wire it**

In `internal/project/validate_protocol.go`, add:

```go
// validateProtocolHTTPTriggers checks each http_trigger: id/method/path and the
// effect message_type are non-empty, and request.auth_role + effect.to_role
// name declared roles. Placeholder resolvability is NOT checked (runtime).
func validateProtocolHTTPTriggers(p *Protocol) error {
	for i, tr := range p.HTTPTriggers {
		prefix := fmt.Sprintf("http_triggers[%d]", i)
		if tr.ID == "" {
			return fmt.Errorf("%s.id is required", prefix)
		}
		if tr.Request.Method == "" {
			return fmt.Errorf("%s.request.method is required", prefix)
		}
		if tr.Request.Path == "" {
			return fmt.Errorf("%s.request.path is required", prefix)
		}
		if tr.Request.AuthRole == "" || p.Roles[tr.Request.AuthRole] == nil {
			return fmt.Errorf("%s.request.auth_role %q does not match a declared role", prefix, tr.Request.AuthRole)
		}
		if tr.Effect.MessageType == "" {
			return fmt.Errorf("%s.effect.message_type is required", prefix)
		}
		if tr.Effect.ToRole == "" || p.Roles[tr.Effect.ToRole] == nil {
			return fmt.Errorf("%s.effect.to_role %q does not match a declared role", prefix, tr.Effect.ToRole)
		}
	}
	return nil
}
```

In `ValidateProtocol`, before `return validateProtocolBatches(...)`, add:

```go
	if err := validateProtocolHTTPTriggers(p); err != nil {
		return err
	}
```

- [ ] **Step 5: Run tests to verify pass**

Run: `go test ./internal/project/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/project/protocol_schema.go internal/project/validate_protocol.go internal/project/validate_protocol_test.go
git commit -m "feat(protocol): add http_triggers declaration + validation"
```

---

### Task 7: Deterministic generator — `wsHTTPTriggerCases`

**Files:**
- Modify: `internal/head/scout/ws_cases.go` (`wsHTTPTriggerCases`, `serviceHost`; wire into `wsCasesForService`)
- Test: `internal/head/scout/ws_cases_test.go`

**Interfaces:**
- Consumes: `Protocol.HTTPTriggers` (Task 6), `TestStep` fields (Task 5), `wsCaseID`/`sanitizeTypeID`.
- Produces: deterministic `http_request` Steps cases appended to the service's case set.

- [ ] **Step 1: Write the failing test**

Append to `internal/head/scout/ws_cases_test.go`:

```go
func TestWSHTTPTriggerCases(t *testing.T) {
	svc := project.Service{
		Name: "realtime",
		URL:  "http://localhost:8989/ws/{userId}",
		Protocol: &project.Protocol{
			Roles: map[string]*project.ProtocolRole{
				"web":    {CredentialRef: "web-actor"},
				"bridge": {CredentialRef: "bridge-actor"},
			},
			HTTPTriggers: []*project.HTTPTrigger{{
				ID:     "device-restart",
				Request: project.HTTPTriggerRequest{Method: "POST", Path: "/api/devices/{{bridge.deviceId}}/restart", AuthRole: "web", ExpectStatus: 200},
				Effect:  project.HTTPTriggerEffect{MessageType: "device:restart", ToRole: "web"},
			}},
		},
	}

	t.Run("emits connect http receive", func(t *testing.T) {
		cases := wsHTTPTriggerCases(svc)
		if len(cases) != 1 {
			t.Fatalf("got %d cases, want 1", len(cases))
		}
		steps := cases[0].Steps
		if len(steps) != 3 || steps[0].Action != "ws_connect" || steps[1].Action != "http_request" || steps[2].Action != "ws_receive" {
			t.Fatalf("unexpected steps: %+v", steps)
		}
		hr := steps[1]
		if hr.URL != "http://localhost:8989/api/devices/{{bridge.deviceId}}/restart" {
			t.Fatalf("URL = %q", hr.URL)
		}
		if hr.AuthRole != "web" || hr.Method != "POST" || hr.ExpectStatus != 200 {
			t.Fatalf("http step = %+v", hr)
		}
		if steps[2].Type != "device:restart" || steps[2].ConnectionID != "web" {
			t.Fatalf("receive step = %+v", steps[2])
		}
	})

	t.Run("no triggers → no cases", func(t *testing.T) {
		noTrig := svc
		noTrig.Protocol = &project.Protocol{Roles: svc.Protocol.Roles}
		if got := wsHTTPTriggerCases(noTrig); len(got) != 0 {
			t.Fatalf("got %d cases, want 0", len(got))
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestWSHTTPTriggerCases ./internal/head/scout/`
Expected: FAIL — `wsHTTPTriggerCases` undefined.

- [ ] **Step 3: Implement the generator**

In `internal/head/scout/ws_cases.go`, add:

```go
// serviceHost returns the scheme://host of a service URL (stripping any WS path
// template such as "/ws/{userId}"). Used to prefix host-relative http_trigger
// paths. Falls back to the raw URL on parse error.
func serviceHost(svcURL string) string {
	u, err := url.Parse(svcURL)
	if err != nil || !u.IsAbs() {
		return svcURL
	}
	return u.Scheme + "://" + u.Host
}

// wsHTTPTriggerCases emits one deterministic Steps case per declared http_trigger:
// connect the effect.to_role connection → http_request the trigger route →
// ws_receive the pushed type on that connection (the decisive assertion). The
// request URL is the service host + the trigger's host-relative path; the
// {{role.param}} placeholder is carried verbatim and resolved at run time by the
// Steps runner. Pure; no LLM. No cases when the protocol declares none.
func wsHTTPTriggerCases(svc project.Service) []agent.TestCase {
	if svc.Protocol == nil || len(svc.Protocol.HTTPTriggers) == 0 {
		return nil
	}
	host := serviceHost(svc.URL)
	var cases []agent.TestCase
	for _, tr := range svc.Protocol.HTTPTriggers {
		toRole := tr.Effect.ToRole
		cases = append(cases, agent.TestCase{
			ID:          "ws-" + svc.Name + "-http-" + sanitizeTypeID(tr.ID),
			Name:        fmt.Sprintf("%s %s triggers %s", svc.Name, tr.ID, tr.Effect.MessageType),
			Service:     svc.Name,
			Target:      svc.URL,
			Action:      "ws_flow",
			Expectation: fmt.Sprintf("%s: POST %s delivers %s to %s", svc.Name, tr.Request.Path, tr.Effect.MessageType, toRole),
			Priority:    0.7,
			Steps: []agent.TestStep{
				{Action: "ws_connect", ConnectionID: toRole, Role: toRole},
				{Action: "http_request", Method: tr.Request.Method, URL: host + tr.Request.Path,
					AuthRole: tr.Request.AuthRole, ExpectStatus: tr.Request.ExpectStatus},
				{Action: "ws_receive", ConnectionID: toRole, Type: tr.Effect.MessageType, Timeout: 5},
			},
		})
	}
	return cases
}
```

Add `"net/url"` to the import block.

Wire it into `wsCasesForService`: after the `rrCases` append (≈ line 67), add:

```go
	cases = append(cases, wsHTTPTriggerCases(svc)...)
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./internal/head/scout/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/head/scout/ws_cases.go internal/head/scout/ws_cases_test.go
git commit -m "feat(scout): generate http_trigger Steps cases"
```

---

### Task 8: Dogfood config

**Files:**
- Modify: `dogfood/ws-realtime/.cerberus/project.yaml` (web-actor `http_login`)
- Modify: `dogfood/ws-realtime/.cerberus/protocols/open-agents.yaml` (`http_triggers`)

- [ ] **Step 1: Add the web-actor `http_login`**

In `dogfood/ws-realtime/.cerberus/project.yaml`, under the `web-actor.auth` block, add after `path_params:`:

```yaml
      http_login:
        method: POST
        path: /api/dev/login
        body: {}
        headers:
          Origin: http://localhost:8989
      http_token_from: token
```

(The `bridge-actor` is unchanged — it has no `http_login`; its HTTP routes, if any, are out of scope.)

- [ ] **Step 2: Declare the device-restart trigger**

In `dogfood/ws-realtime/.cerberus/protocols/open-agents.yaml`, append at top level:

```yaml
http_triggers:
  - id: device-restart
    request:
      method: POST
      path: /api/devices/{{bridge.deviceId}}/restart
      auth_role: web
      expect_status: 200
    effect:
      message_type: device:restart
      to_role: web
```

- [ ] **Step 3: Verify the config loads and the case generates**

Run: `make build && ./build/cerberus protocol vocabulary --config dogfood/ws-realtime/.cerberus/project.yaml --dir dogfood/ws-realtime --dry-run 2>&1 | head`
(If the vocabulary subcommand is not the right validation entry, instead run a quick `make test` to confirm config validation is exercised; the dogfood config is loaded by the integration suite in Task 9.) Expected: no validation error.

- [ ] **Step 4: Commit**

```bash
git add dogfood/ws-realtime/.cerberus/project.yaml dogfood/ws-realtime/.cerberus/protocols/open-agents.yaml
git commit -m "feat(dogfood): web-actor http_login + device-restart http_trigger"
```

---

### Task 9: Live integration test

**Files:**
- Create: `internal/head/agent/httptrigger_live_integration_test.go` (`//go:build integration`)

**Interfaces:**
- Consumes: the existing open-agents provisioning helpers (follow the pattern in `internal/head/agent/pathcoverage_live_integration_test.go` and `openagents_setup_test.go` — same `Origin` header, same `/api/dev/setup` provisioning, same live-server base URL).

- [ ] **Step 1: Write the live test**

Create `internal/head/agent/httptrigger_live_integration_test.go`. Follow the existing live tests' structure for bring-up/provisioning (reuse their helpers/vars verbatim). The case: provision a web actor + a bridge device via `/api/dev/setup`; call `/api/dev/login` for a JWT; open a web WS connection; POST restart; receive `device:restart`.

```go
//go:build integration

package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/binoctal/cerberus/internal/types"
)

// TestHTTPTrigger_LiveDeviceRestart proves the http_request step end-to-end
// against a live open-agents dev server: a web WS connection receives the
// device:restart push triggered by an authenticated POST /api/devices/:id/restart.
// Provisioning/auth follow the same pattern as pathcoverage_live_integration_test.go.
func TestHTTPTrigger_LiveDeviceRestart(t *testing.T) {
	base := liveOpenAgentsBase(t) // helper from the existing live suite; skips if server down
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Provision (reuse the existing suite's provisioning helper/shape).
	userID, bridgeDeviceID, webToken, deviceToken := provisionOpenAgents(t, ctx, base)

	// Obtain a JWT via /api/dev/login (demo defaults; setup created the user).
	jwt := devLogin(t, ctx, base)

	// Build a web WS connection via the executor + a protocol index (mirror the
	// existing live tests' executor/index construction).
	idx, executor := liveWSIndexAndExecutor(t, base, userID, webToken, deviceToken, bridgeDeviceID)

	// 1. Connect web.
	connect := types.WSConnectAction{URL: base + "/ws/" + userID, Role: "web", ConnectionID: "web"}
	if r := executor.Execute(ctx, connect); !r.Success() {
		t.Fatalf("web connect: %v", r)
	}

	// 2. http_request restart (explicit Authorization from the JWT; URL templated
	//    by hand here since this test does not run the scout generator).
	req := types.HTTPAction{
		Method:  "POST",
		URL:     base + "/api/devices/" + bridgeDeviceID + "/restart",
		Headers: map[string]string{"Authorization": "Bearer " + jwt},
	}
	if r := executor.Execute(ctx, req); !r.Success() {
		t.Fatalf("restart POST: %v", r)
	}

	// 3. Receive the pushed device:restart on the web connection.
	recv := types.WSReceiveAction{ConnectionID: "web", Type: "device:restart", Timeout: 5}
	r := executor.Execute(ctx, recv)
	if !r.Success() {
		t.Fatalf("expected device:restart on web, got: %v", r)
	}
	_ = idx // index retained by executor; referenced to keep the helper in scope
}
```

The helper functions `liveOpenAgentsBase`, `provisionOpenAgents`, `liveWSIndexAndExecutor` should MATCH the names/patterns already present in the live suite; if they differ, copy the exact provisioning sequence from `pathcoverage_live_integration_test.go` (it already does `/api/dev/setup` with the `Origin` header and builds the WS executor/index). `devLogin` is new:

```go
func devLogin(t *testing.T, ctx context.Context, base string) string {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, "POST", base+"/api/dev/login", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", base)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("dev login: %v", err)
	}
	defer resp.Body.Close()
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.Token == "" {
		t.Fatalf("dev login: no token (status %d)", resp.StatusCode)
	}
	return out.Token
}
```

Adjust helper names/structs to the real ones in the existing live test file when implementing (do not invent provisioning that contradicts it).

- [ ] **Step 2: Run the live test against a running server**

Start open-agents (or reuse `:8989`):

```bash
bash scripts/integration-openagents.sh
```

In another shell, or via the make target:

```bash
make integration-openagents TEST=TestHTTPTrigger_LiveDeviceRestart
```

Expected: PASS — web receives `device:restart`. If it fails on auth, confirm the JWT is set (not `demo_token`) and the device exists for the user.

- [ ] **Step 3: Commit**

```bash
git add internal/head/agent/httptrigger_live_integration_test.go
git commit -m "test(agent): live http_request device-restart trigger proof"
```

---

### Task 10: Autonomous live verification + report

**Files:**
- Modify: `cerberus-docs/technical/validation/2026-08-07-openagents-integration-test-report.md` (append a section)

- [ ] **Step 1: Run an autonomous `cerberus run`**

With open-agents up on `:8989` and `make build` at the branch tip:

```bash
./build/cerberus run --config dogfood/ws-realtime/.cerberus/project.yaml \
  --dir dogfood/ws-realtime \
  --goal "Trigger a device restart over HTTP and observe the push over the realtime WS service"
```

Capture verbatim: the `auth flow resolved` lines for `web-actor` (note a second login ran) and `bridge-actor`, the `coverage assessment` line (`reached`/`gaps`/`coverage_pct`), and the `device-restart` case's `test case completed` + Examiner `verdict` lines.

- [ ] **Step 2: Append the honest verification section**

Append to `cerberus-docs/technical/validation/2026-08-07-openagents-integration-test-report.md` a new dated section recording: the run setup, the verbatim log lines, whether the `device-restart` case passed, and an honest note on whether `coverage_pct` changed and why (per the spec's coverage caveat: `device:restart` is HTTP-emitted and may not be a declared WS vocab edge, so coverage may not rise even though the case passes). Report the two outcomes separately.

- [ ] **Step 3: Commit**

```bash
git add cerberus-docs/technical/validation/2026-08-07-openagents-integration-test-report.md
git commit -m "docs(validation): autonomous http-trigger device-restart verification"
```

---

## Self-Review Notes

- **Spec coverage:** D1 (runner resolution) → Task 5; D2 (shared resolver) → Task 4; D3 (http_login + slot) → Tasks 1–3; D4 (CSRF) → no code needed, documented in spec; D5 (http_triggers) → Task 6; D6 (generator) → Task 7; D7 (evidence/verdict) → Task 5. Success criteria 1–3 → Tasks 1–5, 9; criterion 4 → Tasks 8, 10; criterion 5 → all tasks preserve absent-field behavior.
- **Type consistency:** `AuthFlow.HTTPLogin`/`HTTPTokenFrom` (Task 1) → consumed in Task 2; `AuthResult.HTTPToken` (Task 2) → `Credentials.RawHTTPToken` (Task 2) → `ActorHTTPTokens` (Task 3) → read in `resolveHTTPStep` (Task 5). `Protocol.HTTPTriggers`/`HTTPTrigger` (Task 6) → `wsHTTPTriggerCases` (Task 7). `TestStep.Method/Headers/Body/ExpectStatus/AuthRole` (Task 5) used by both the generator (Task 7) and resolver (Task 5).
- **No placeholders:** helper-name caveats in Task 9 are explicitly tied to mirroring the existing `pathcoverage_live_integration_test.go`; all other steps carry real code.
