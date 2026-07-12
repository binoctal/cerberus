# Declarative Auth Flow Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let cerberus obtain a dynamic auth credential (e.g. a REST login JWT) once per session via a declarative `auth:` block, inject it as a static header so existing header-injection code carries it unchanged.

**Architecture:** A single-purpose executor (`internal/head/agent/authflow.go`) runs one login HTTP request, extracts a token from the response JSON via a dot-path, and interpolates it into a header template. Session setup calls this once per actor that has an `auth:` block and writes the resulting header into `actor.Credentials.Headers`. Downstream `authHeadersFor` / `withActorHeaders` are unchanged — the dynamic token becomes a static header before any test case runs. On any failure the actor degrades to unauthenticated (warning log), never aborting the session.

**Tech Stack:** Go 1.25, `net/http`, `encoding/json`, `strings`, `net/http/httptest` (tests), existing `internal/project` schema/validation, `internal/session` lifecycle.

**Reference spec:** `cerberus-docs/superpowers/specs/2026-07-12-auth-flow-design.md`

## Scope

This plan covers spec Components 1 (schema), 2 (executor), and 4 (error handling, security, testing), plus session integration. It delivers the complete core loop: a configured `auth:` block produces an injected Bearer header at run time.

Spec Component 3 (LLM-assisted discovery: `cerberus auth discover` command + Scout runtime fallback) is a separate authoring-aid subsystem and will be a **follow-up plan**. It depends on this plan's schema and executor but is not required for the core loop.

## Global Constraints

- Go 1.25, module `github.com/binoctal/cerberus`, pure-Go (no CGo).
- Commit author: `binoctal <binoctal@gmail.com>`, no `Co-Authored-By`.
- Code comments and commit messages in English.
- Token values MUST NEVER be logged or printed (record header name, HTTP status, length only).
- `make check` (fmt + lint + race test) green after every task.
- Follow existing comment density and naming idiom in the touched packages.
- Degrade, never abort: any auth-flow failure logs a warning and leaves the actor unauthenticated; it must not fail the session.

---

## File Structure

- **Create** `internal/project/authflow_schema.go` — `AuthFlow` / `AuthLogin` Go types (kept out of `schema.go` to keep that file focused; same package, same YAML decode).
- **Create** `internal/project/validate_auth.go` — `validateAuthFlow` extension to actor validation.
- **Modify** `internal/project/schema.go` — add `Auth *AuthFlow` field to `Actor`.
- **Modify** `internal/project/validate_actors.go` — call `validateAuthFlow` per actor.
- **Create** `internal/head/agent/authflow.go` — `ResolveAuthHeader` executor: interpolate body, send one HTTP login, extract token via dot-path, build header. Plus `extractByDotPath` and `interpolate` helpers.
- **Create** `internal/head/agent/authflow_test.go` — httptest-based tests covering extraction, dot-path, degrade cases, interpolation.
- **Create** `internal/project/authflow_schema_test.go` — YAML round-trip + validation tests.
- **Create** `internal/session/auth_setup.go` — `(s *Session) resolveActorAuth(ctx)`; resolves every actor's `Auth` block and writes headers into `Credentials.Headers`.
- **Modify** `internal/session/lifecycle_run.go` — call `resolveActorAuth` after `initialize()`.
- **Modify** `internal/session/lifecycle_resume.go` — call `resolveActorAuth` after its initialize step.

---

## Task 1: Config schema — `AuthFlow` / `AuthLogin` types

**Files:**
- Create: `internal/project/authflow_schema.go`
- Modify: `internal/project/schema.go:26-31` (the `Actor` struct)
- Test: `internal/project/authflow_schema_test.go`

**Interfaces:**
- Consumes: nothing (leaf types).
- Produces: `project.AuthFlow{Login AuthLogin; TokenFrom string; InjectAs string}`, `project.AuthLogin{Method, Path string; Body, Headers map[string]string}`, and `Actor.Auth *AuthFlow`. Later tasks reference these exact field names.

- [ ] **Step 1: Write the failing test**

Create `internal/project/authflow_schema_test.go`:

```go
package project

import (
	"bytes"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestActorAuthYAMLRoundTrip(t *testing.T) {
	in := []byte(`
actors:
  - name: test-user
    credentials:
      email: dev@example.local
      password: dev123456
    auth:
      login:
        method: POST
        path: /api/dev/setup
        body:
          email: "{email}"
          password: "{password}"
      token_from: token
      inject_as: "Authorization: Bearer {token}"
`)
	var cfg Config
	if err := yaml.Unmarshal(in, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(cfg.Actors) != 1 {
		t.Fatalf("want 1 actor, got %d", len(cfg.Actors))
	}
	a := cfg.Actors[0]
	if a.Auth == nil {
		t.Fatal("want non-nil Auth")
	}
	if a.Auth.Login.Method != "POST" || a.Auth.Login.Path != "/api/dev/setup" {
		t.Fatalf("login = %+v", a.Auth.Login)
	}
	if a.Auth.Login.Body["email"] != "{email}" {
		t.Fatalf("body email = %q", a.Auth.Login.Body["email"])
	}
	if a.Auth.TokenFrom != "token" {
		t.Fatalf("token_from = %q", a.Auth.TokenFrom)
	}
	if a.Auth.InjectAs != "Authorization: Bearer {token}" {
		t.Fatalf("inject_as = %q", a.Auth.InjectAs)
	}

	// Re-marshal and parse again to confirm round-trip stability.
	out, err := yaml.Marshal(&cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var cfg2 Config
	if err := yaml.Unmarshal(bytes.TrimSpace(out), &cfg2); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if cfg2.Actors[0].Auth.TokenFrom != "token" {
		t.Fatalf("round-trip token_from = %q", cfg2.Actors[0].Auth.TokenFrom)
	}
}

func TestActorAuthAbsentByDefault(t *testing.T) {
	in := []byte("actors:\n  - name: plain\n    credentials:\n      email: a@b.c\n")
	var cfg Config
	if err := yaml.Unmarshal(in, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.Actors[0].Auth != nil {
		t.Fatal("Auth must be nil when absent (zero breakage)")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/project/ -run TestActorAuth -v`
Expected: build error — `cfg.Actors[0].Auth` undefined (`unknown field Auth` is also acceptable from strict yaml decoding).

- [ ] **Step 3: Create the types**

Create `internal/project/authflow_schema.go`:

```go
package project

// AuthFlow declaratively obtains a dynamic credential once per session and
// injects it as a static header. When Auth is nil on an Actor, cerberus keeps
// the existing static-header behavior unchanged.
type AuthFlow struct {
	Login     AuthLogin `yaml:"login"`
	TokenFrom string    `yaml:"token_from"` // dot-path into response JSON (e.g. "token", "data.accessToken")
	InjectAs  string    `yaml:"inject_as"`  // header template, "{token}" substituted (e.g. "Authorization: Bearer {token}")
}

// AuthLogin describes the single login HTTP request.
type AuthLogin struct {
	Method  string            `yaml:"method"`            // e.g. POST
	Path    string            `yaml:"path"`              // relative to service.URL, or absolute
	Body    map[string]string `yaml:"body,omitempty"`    // values may reference "{email}"/"{password}" from credentials
	Headers map[string]string `yaml:"headers,omitempty"` // optional static headers on the login request
}
```

- [ ] **Step 4: Add the `Auth` field to `Actor`**

In `internal/project/schema.go`, change the `Actor` struct (currently lines 26-31) to:

```go
type Actor struct {
	Name        string        `yaml:"name"`
	Credentials CredentialRef `yaml:"credentials"`
	Auth        *AuthFlow     `yaml:"auth,omitempty"`
	Entry       string        `yaml:"entry,omitempty"`
	Service     string        `yaml:"service,omitempty"`
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/project/ -run TestActorAuth -v`
Expected: PASS for both `TestActorAuthYAMLRoundTrip` and `TestActorAuthAbsentByDefault`.

- [ ] **Step 6: Commit**

```bash
git add internal/project/authflow_schema.go internal/project/authflow_schema_test.go internal/project/schema.go
git commit -m "feat(project): add AuthFlow schema for declarative auth"
```

---

## Task 2: Schema validation — required fields + interpolation variables

**Files:**
- Create: `internal/project/validate_auth.go`
- Modify: `internal/project/validate_actors.go:8-19` (the `validateActors` loop)
- Test: extend `internal/project/authflow_schema_test.go`

**Interfaces:**
- Consumes: `Actor.Auth *AuthFlow` (Task 1), `Actor.Credentials` (`CredentialRef{Email, Password}`).
- Produces: validation errors surfaced through `Config.Validate()`; later tasks can assume a validated `AuthFlow` has a non-empty `login.method`, `login.path`, `token_from`, `inject_as`, and that every `{email}`/`{password}` in `login.body` has a matching non-empty credential field.

- [ ] **Step 1: Write the failing tests**

Append to `internal/project/authflow_schema_test.go`:

```go
func TestValidateAuthFlowErrors(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "missing method",
			yaml: `actors:
  - name: u
    credentials: {email: a@b.c}
    auth:
      login: {path: /login}
      token_from: token
      inject_as: "Authorization: Bearer {token}"`,
			want: "login.method",
		},
		{
			name: "missing token_from",
			yaml: `actors:
  - name: u
    credentials: {email: a@b.c}
    auth:
      login: {method: POST, path: /login}
      inject_as: "Authorization: Bearer {token}"`,
			want: "token_from",
		},
		{
			name: "missing inject_as",
			yaml: `actors:
  - name: u
    credentials: {email: a@b.c}
    auth:
      login: {method: POST, path: /login}
      token_from: token`,
			want: "inject_as",
		},
		{
			name: "body references missing email credential",
			yaml: `actors:
  - name: u
    credentials: {}
    auth:
      login: {method: POST, path: /login, body: {email: "{email}"}}
      token_from: token
      inject_as: "Authorization: Bearer {token}"`,
			want: "interpolation variable {email}",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var cfg Config
			if err := yaml.Unmarshal([]byte(tc.yaml), &cfg); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			err := cfg.Validate()
			if err == nil {
				t.Fatal("want validation error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestValidateAuthFlowOK(t *testing.T) {
	in := []byte(`actors:
  - name: u
    credentials: {email: a@b.c, password: pw}
    auth:
      login: {method: POST, path: /login, body: {email: "{email}", password: "{password}"}}
      token_from: data.accessToken
      inject_as: "Authorization: Bearer {token}"`)
	var cfg Config
	if err := yaml.Unmarshal(in, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}
```

Add `"strings"` to the test file's imports if the linter requires it.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/project/ -run TestValidateAuthFlow -v`
Expected: `TestValidateAuthFlowErrors` FAILS (no validation yet, `Validate()` returns nil); `TestValidateAuthFlowOK` passes already.

- [ ] **Step 3: Create the validator**

Create `internal/project/validate_auth.go`:

```go
package project

import (
	"fmt"
	"strings"
)

// validateAuthFlow validates an actor's optional declarative auth block.
// All checks are config-time so a misconfigured flow is never a runtime
// surprise. Returns the first problem found (validation collects per-actor).
func validateAuthFlow(actorIdx int, a Actor) string {
	if a.Auth == nil {
		return ""
	}
	af := a.Auth
	prefix := fmt.Sprintf("actors[%d].auth", actorIdx)
	if af.Login.Method == "" {
		return prefix + ".login.method is required"
	}
	if af.Login.Path == "" {
		return prefix + ".login.path is required"
	}
	if af.TokenFrom == "" {
		return prefix + ".token_from is required"
	}
	if af.InjectAs == "" {
		return prefix + ".inject_as is required"
	}
	// Every {email}/{password} referenced in login.body must have a matching
	// non-empty credential field; otherwise login would interpolate to "" at
	// runtime with no warning.
	for _, v := range af.Login.Body {
		for _, ref := range []string{"{email}", "{password}"} {
			if strings.Contains(v, ref) {
				field := ref[1 : len(ref)-1] // "email" or "password"
				if credentialField(a.Credentials, field) == "" {
					return fmt.Sprintf("%s: body references interpolation variable %s but credentials.%s is empty", prefix, ref, field)
				}
			}
		}
	}
	return ""
}

// credentialField returns the named credential value by field name.
func credentialField(c CredentialRef, field string) string {
	switch field {
	case "email":
		return c.Email
	case "password":
		return c.Password
	default:
		return ""
	}
}
```

- [ ] **Step 4: Wire it into `validateActors`**

In `internal/project/validate_actors.go`, extend the loop body so a duplicate-free actor also checks its auth block. Replace the function body with:

```go
// validateActors checks actor configuration
func validateActors(cfg *Config, ve *ValidationError) {
	seenActor := make(map[string]bool)
	for i, a := range cfg.Actors {
		if a.Name == "" {
			ve.add(fmt.Sprintf("actors[%d]: name is required", i))
		} else if seenActor[a.Name] {
			ve.add(fmt.Sprintf("actors[%d]: duplicate actor name %q", i, a.Name))
		} else {
			seenActor[a.Name] = true
		}
		if msg := validateAuthFlow(i, a); msg != "" {
			ve.add(msg)
		}
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/project/ -run 'TestValidateAuthFlow|TestActorAuth' -v`
Expected: all PASS.

- [ ] **Step 6: Run full package + lint**

Run: `make lint && go test ./internal/project/ -v`
Expected: no lint errors, full package green.

- [ ] **Step 7: Commit**

```bash
git add internal/project/validate_auth.go internal/project/validate_actors.go internal/project/authflow_schema_test.go
git commit -m "feat(project): validate auth flow required fields and interpolation vars"
```

---

## Task 3: Dot-path JSON extraction helper

**Files:**
- Create: `internal/head/agent/authflow.go` (the helper section; the executor is added in Task 4)
- Test: `internal/head/agent/authflow_test.go` (helper tests; executor tests added in Task 4)

**Interfaces:**
- Consumes: nothing.
- Produces: `extractByDotPath(data map[string]any, path string) (string, error)` where `path` is a dotted path into decoded JSON (`"token"`, `"data.accessToken"`). Returns the string form of the leaf value, or an error naming the missing key.

- [ ] **Step 1: Write the failing tests**

Create `internal/head/agent/authflow_test.go`:

```go
package agent

import "testing"

func TestExtractByDotPath(t *testing.T) {
	cases := []struct {
		name string
		data map[string]any
		path string
		want string
	}{
		{
			name: "top-level string",
			data: map[string]any{"token": "abc"},
			path: "token",
			want: "abc",
		},
		{
			name: "nested object",
			data: map[string]any{"data": map[string]any{"accessToken": "xyz"}},
			path: "data.accessToken",
			want: "xyz",
		},
		{
			name: "non-string leaf coerced",
			data: map[string]any{"data": map[string]any{"id": float64(42)}},
			path: "data.id",
			want: "42",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extractByDotPath(tc.data, tc.path)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestExtractByDotPathErrors(t *testing.T) {
	cases := []struct {
		name string
		data map[string]any
		path string
	}{
		{name: "missing top key", data: map[string]any{}, path: "token"},
		{name: "missing nested key", data: map[string]any{"data": map[string]any{}}, path: "data.accessToken"},
		{name: "descend through scalar", data: map[string]any{"data": "notobj"}, path: "data.accessToken"},
		{name: "empty path", data: map[string]any{"token": "x"}, path: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := extractByDotPath(tc.data, tc.path); err == nil {
				t.Fatal("want error, got nil")
			}
		})
	}
}

func TestInterpolate(t *testing.T) {
	cases := []struct {
		name     string
		template string
		vars     map[string]string
		want     string
	}{
		{name: "email+password", template: "{email}:{password}", vars: map[string]string{"{email}": "a@b.c", "{password}": "pw"}, want: "a@b.c:pw"},
		{name: "no vars", template: "plain", vars: nil, want: "plain"},
		{name: "token", template: "Authorization: Bearer {token}", vars: map[string]string{"{token}": "JWT123"}, want: "Authorization: Bearer JWT123"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := interpolate(tc.template, tc.vars); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/head/agent/ -run 'TestExtractByDotPath|TestInterpolate' -v`
Expected: build failure — `extractByDotPath` / `interpolate` undefined.

- [ ] **Step 3: Implement the helpers**

Create `internal/head/agent/authflow.go` with only the helpers for now (Task 4 appends the executor and adds the remaining imports):

```go
package agent

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// httpClient is used by ResolveAuthHeader so tests can swap in an
// httptest.Server-aware client. It carries a conservative timeout so a hung
// login endpoint cannot stall session setup indefinitely.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// interpolate replaces every "{name}" token in template with the matching
// value from vars. Unknown tokens are left untouched.
func interpolate(template string, vars map[string]string) string {
	out := template
	for k, v := range vars {
		out = strings.ReplaceAll(out, k, v)
	}
	return out
}

// extractByDotPath walks a decoded JSON object by a dotted path (e.g.
// "data.accessToken") and returns the string form of the leaf value. Returns
// an error naming the missing or non-traversable key so callers can log it.
func extractByDotPath(data map[string]any, path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("empty token_from path")
	}
	cur := any(data)
	for _, key := range strings.Split(path, ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return "", fmt.Errorf("token_from: key %q is not an object", key)
		}
		next, exists := obj[key]
		if !exists {
			return "", fmt.Errorf("token_from: key %q not found", key)
		}
		cur = next
	}
	switch v := cur.(type) {
	case string:
		return v, nil
	case fmt.Stringer:
		return v.String(), nil
	default:
		return fmt.Sprint(v), nil
	}
}
```

(The `ResolveAuthHeader` executor is appended in Task 4 and at that point adds `context`, `encoding/json`, `io`, and `github.com/binoctal/cerberus/internal/project` to the import block. This file, as written above, already compiles — every imported package is used by the helpers or `httpClient`.)

> **Note:** Each task commits independently and builds cleanly. Task 3 commits the helpers; Task 4 appends the executor, extends the import block, and commits both as one logical unit if you deferred Task 3's commit — otherwise Task 4 is its own commit on top.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/head/agent/ -run 'TestExtractByDotPath|TestInterpolate' -v`
Expected: all PASS.

- [ ] **Step 5: Commit (together with Task 4 — see note)**

If you landed Task 3 alone with unused imports suppressed, hold the commit until Task 4. Otherwise commit the helpers now with the `context/json/io/http` imports removed and re-added in Task 4. Recommended: continue to Task 4 and commit both together:

```bash
git add internal/head/agent/authflow.go internal/head/agent/authflow_test.go
git commit -m "feat(agent): add auth flow dot-path extractor and interpolator"
```

---

## Task 4: Auth executor — `ResolveAuthHeader`

**Files:**
- Modify: `internal/head/agent/authflow.go` (append executor + parse header helper)
- Test: append to `internal/head/agent/authflow_test.go`

**Interfaces:**
- Consumes: `project.Actor` (with `.Auth`, `.Credentials`), `extractByDotPath`, `interpolate` (Task 3).
- Produces: `ResolveAuthHeader(ctx context.Context, svcURL string, actor project.Actor) (name, value string, err error)` — performs one login request, extracts the token, returns the header name and value to inject. On any failure returns a non-nil error; the caller (session) degrades.

- [ ] **Step 1: Write the failing tests**

Append to `internal/head/agent/authflow_test.go`. Add these imports to the file: `"context"`, `"encoding/json"`, `"net/http"`, `"net/http/httptest"`, `"strings"`, `"testing"`.

```go
func newLoginServer(t *testing.T, status int, respBody string, capture func(method, path, body string, headers http.Header)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if capture != nil {
			capture(r.Method, r.URL.Path, string(body), r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, respBody)
	}))
}

func TestResolveAuthHeader_TopLevelToken(t *testing.T) {
	srv := newLoginServer(t, 200, `{"token":"JWT-123"}`, nil)
	defer srv.Close()

	actor := project.Actor{Auth: &project.AuthFlow{
		Login:     project.AuthLogin{Method: "POST", Path: "/api/dev/setup"},
		TokenFrom: "token",
		InjectAs:  "Authorization: Bearer {token}",
	}}
	name, value, err := ResolveAuthHeader(context.Background(), srv.URL, actor)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if name != "Authorization" || value != "Bearer JWT-123" {
		t.Fatalf("got %q=%q", name, value)
	}
}

func TestResolveAuthHeader_DotPathToken(t *testing.T) {
	srv := newLoginServer(t, 200, `{"data":{"accessToken":"xyz"}}`, nil)
	defer srv.Close()

	actor := project.Actor{Auth: &project.AuthFlow{
		Login:     project.AuthLogin{Method: "POST", Path: "/login"},
		TokenFrom: "data.accessToken",
		InjectAs:  "X-Token: {token}",
	}}
	name, value, err := ResolveAuthHeader(context.Background(), srv.URL, actor)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if name != "X-Token" || value != "xyz" {
		t.Fatalf("got %q=%q", name, value)
	}
}

func TestResolveAuthHeader_EmailPasswordInterpolation(t *testing.T) {
	var gotBody string
	srv := newLoginServer(t, 200, `{"token":"T"}`, func(_, _ string, body string, _ http.Header) {
		gotBody = body
	})
	defer srv.Close()

	actor := project.Actor{
		Credentials: project.CredentialRef{Email: "dev@example.local", Password: "secret"},
		Auth: &project.AuthFlow{
			Login: project.AuthLogin{
				Method: "POST", Path: "/login",
				Body: map[string]string{"email": "{email}", "password": "{password}"},
			},
			TokenFrom: "token",
			InjectAs:  "Authorization: Bearer {token}",
		},
	}
	_, _, err := ResolveAuthHeader(context.Background(), srv.URL, actor)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	var sent map[string]string
	if err := json.Unmarshal([]byte(gotBody), &sent); err != nil {
		t.Fatalf("body not JSON object: %v (%s)", err, gotBody)
	}
	if sent["email"] != "dev@example.local" || sent["password"] != "secret" {
		t.Fatalf("interpolated body = %v", sent)
	}
}

func TestResolveAuthHeader_Non2xxDegrades(t *testing.T) {
	srv := newLoginServer(t, 401, `{"error":"no"}`, nil)
	defer srv.Close()

	actor := project.Actor{Auth: &project.AuthFlow{
		Login: project.AuthLogin{Method: "POST", Path: "/login"},
		TokenFrom: "token", InjectAs: "Authorization: Bearer {token}",
	}}
	_, _, err := ResolveAuthHeader(context.Background(), srv.URL, actor)
	if err == nil {
		t.Fatal("want error on non-2xx, got nil")
	}
}

func TestResolveAuthHeader_MissingTokenFieldDegrades(t *testing.T) {
	srv := newLoginServer(t, 200, `{"unrelated":"x"}`, nil)
	defer srv.Close()

	actor := project.Actor{Auth: &project.AuthFlow{
		Login: project.AuthLogin{Method: "POST", Path: "/login"},
		TokenFrom: "token", InjectAs: "Authorization: Bearer {token}",
	}}
	_, _, err := ResolveAuthHeader(context.Background(), srv.URL, actor)
	if err == nil {
		t.Fatal("want error when token field missing, got nil")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Fatalf("error should name the missing key, got %v", err)
	}
}
```

Add `"fmt"` and `"io"` to the test imports if the linter requires it.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/head/agent/ -run TestResolveAuthHeader -v`
Expected: build failure — `ResolveAuthHeader` undefined.

- [ ] **Step 3: Implement the executor**

First extend the import block of `internal/head/agent/authflow.go` to add `context`, `encoding/json`, `io`, and `github.com/binoctal/cerberus/internal/project` (the Task 3 helpers already use `fmt`, `net/http`, `strings`, `time`). Then append:

```go
// ResolveAuthHeader runs an actor's declarative login flow once and returns the
// header name and value to inject into subsequent requests. It is called at
// session setup, once per actor that has an Auth block; the result is cached
// for the session by writing it into the actor's Credentials.Headers.
//
// On any failure (network error, non-2xx response, missing token field) it
// returns an error; the caller logs a warning and leaves the actor
// unauthenticated rather than aborting the session.
//
// Token values are never returned in errors or logged by this function — the
// caller records only the header name, HTTP status, and length.
func ResolveAuthHeader(ctx context.Context, svcURL string, actor project.Actor) (name, value string, err error) {
	af := actor.Auth
	if af == nil {
		return "", "", fmt.Errorf("actor has no auth flow")
	}

	// 1. Interpolate {email}/{password} into the login body.
	vars := map[string]string{
		"{email}":    actor.Credentials.Email,
		"{password}": actor.Credentials.Password,
	}
	bodyFields := make(map[string]string, len(af.Login.Body))
	for k, v := range af.Login.Body {
		bodyFields[k] = interpolate(v, vars)
	}

	// 2. Build the login URL: absolute path wins, else join onto svcURL.
	loginURL := af.Login.Path
	if !isAbsoluteURL(loginURL) {
		loginURL = strings.TrimRight(svcURL, "/") + "/" + strings.TrimLeft(loginURL, "/")
	}

	var bodyReader io.Reader
	if len(bodyFields) > 0 {
		encoded, mErr := json.Marshal(bodyFields)
		if mErr != nil {
			return "", "", fmt.Errorf("auth flow: encode login body: %w", mErr)
		}
		bodyReader = strings.NewReader(string(encoded))
	}

	method := af.Login.Method
	if method == "" {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, loginURL, bodyReader)
	if err != nil {
		return "", "", fmt.Errorf("auth flow: build request: %w", err)
	}
	if len(bodyFields) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range af.Login.Headers {
		req.Header.Set(k, v)
	}

	// 3. Send one real request.
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("auth flow: login request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Non-2xx: never include the body (may echo credentials).
		return "", "", fmt.Errorf("auth flow: login returned status %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("auth flow: read response: %w", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", "", fmt.Errorf("auth flow: response is not a JSON object")
	}

	// 4. Extract the token by dot-path.
	token, err := extractByDotPath(decoded, af.TokenFrom)
	if err != nil {
		return "", "", err
	}

	// 5. Interpolate {token} into inject_as and split into header name/value.
	header := interpolate(af.InjectAs, map[string]string{"{token}": token})
	hName, hValue, ok := splitHeader(header)
	if !ok {
		return "", "", fmt.Errorf("auth flow: inject_as %q is not a 'Name: Value' header", af.InjectAs)
	}
	return hName, hValue, nil
}

// splitHeader splits "Name: Value" into name and value at the first colon.
// Value is space-trimmed. Returns ok=false if there is no colon.
func splitHeader(s string) (name, value string, ok bool) {
	idx := strings.Index(s, ":")
	if idx < 0 {
		return "", "", false
	}
	return strings.TrimSpace(s[:idx]), strings.TrimSpace(s[idx+1:]), true
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/head/agent/ -run 'TestResolveAuthHeader|TestExtractByDotPath|TestInterpolate' -v`
Expected: all PASS.

- [ ] **Step 5: Run package lint + race**

Run: `make lint && go test ./internal/head/agent/ -race -run Auth -v`
Expected: clean.

- [ ] **Step 6: Commit (helpers + executor together, per Task 3 note)**

If Task 3 was not committed separately:

```bash
git add internal/head/agent/authflow.go internal/head/agent/authflow_test.go
git commit -m "feat(agent): add declarative auth flow executor"
```

---

## Task 5: Session integration — resolve auth headers at setup

**Files:**
- Create: `internal/session/auth_setup.go`
- Modify: `internal/session/lifecycle_run.go:26-29` (after `rp.initialize()`)
- Modify: `internal/session/lifecycle_resume.go` (after its initialize step)
- Test: `internal/session/auth_setup_test.go`

**Interfaces:**
- Consumes: `agent.ResolveAuthHeader` (Task 4), `s.Config.Actors`, `s.Config.Services`, `s.Logger`.
- Produces: `(s *Session) resolveActorAuth(ctx context.Context)` which mutates `s.Config.Actors[i].Credentials.Headers` in place. After it runs, the existing `authHeadersFor` (`rules.go`) and `withActorHeaders` (`react_loop_helpers.go`) inject the resolved header with no changes to those functions.

- [ ] **Step 1: Locate the resume initialize hook**

Run: `grep -n "initialize" internal/session/lifecycle_resume.go internal/session/resume_phases_lifecycle.go`
Confirm the `Resume` flow calls `rp.initialize()` (in `resume_phases_lifecycle.go`). The call site to modify is in `lifecycle_resume.go` `Session.Resume`, immediately after that initialize step returns, mirroring `Run`.

- [ ] **Step 2: Write the failing test**

Create `internal/session/auth_setup_test.go`:

```go
package session

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/binoctal/cerberus/internal/project"
)

func newAuthLoginServer(t *testing.T, token string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"token":%q}`, token)
	}))
}

func TestResolveActorAuthWritesHeader(t *testing.T) {
	srv := newAuthLoginServer(t, "JWT-XYZ")
	defer srv.Close()

	s := &Session{
		Config: project.Config{
			Services: []project.Service{{Name: "api", URL: srv.URL}},
			Actors: []project.Actor{{
				Name:    "u",
				Service: "api",
				Auth: &project.AuthFlow{
					Login:     project.AuthLogin{Method: "POST", Path: "/login"},
					TokenFrom: "token",
					InjectAs:  "Authorization: Bearer {token}",
				},
			}},
		},
		Logger: testLogger(),
	}
	s.resolveActorAuth(context.Background())

	got := s.Config.Actors[0].Credentials.Headers["Authorization"]
	if got != "Bearer JWT-XYZ" {
		t.Fatalf("header = %q, want Bearer JWT-XYZ", got)
	}
}

func TestResolveActorAuthDegradesOnFailure(t *testing.T) {
	// Login server returns 401; actor must remain authenticated-ably absent
	// (no header written) and the session must not error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	s := &Session{
		Config: project.Config{
			Services: []project.Service{{Name: "api", URL: srv.URL}},
			Actors: []project.Actor{{
				Name:    "u",
				Service: "api",
				Auth: &project.AuthFlow{
					Login:     project.AuthLogin{Method: "POST", Path: "/login"},
					TokenFrom: "token",
					InjectAs:  "Authorization: Bearer {token}",
				},
			}},
		},
		Logger: testLogger(),
	}
	// Must not panic or return error.
	s.resolveActorAuth(context.Background())

	if h := s.Config.Actors[0].Credentials.Headers["Authorization"]; h != "" {
		t.Fatalf("no header should be written on failure, got %q", h)
	}
}

func TestResolveActorAuthSkipsActorsWithoutAuth(t *testing.T) {
	s := &Session{
		Config: project.Config{
			Actors: []project.Actor{{Name: "plain", Credentials: project.CredentialRef{Email: "a@b.c"}}},
		},
		Logger: testLogger(),
	}
	s.resolveActorAuth(context.Background()) // must be a no-op
}
```

If `testLogger()` does not already exist in the session test package, add a tiny helper to the test file:

```go
func testLogger() *zap.Logger {
	return zap.NewNop()
}
```

and import `"go.uber.org/zap"`. Check first: `grep -n "func testLogger\|zap.NewNop" internal/session/*_test.go`.

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/session/ -run TestResolveActorAuth -v`
Expected: build failure — `s.resolveActorAuth` undefined.

- [ ] **Step 4: Implement `resolveActorAuth`**

Create `internal/session/auth_setup.go`:

```go
package session

import (
	"context"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// resolveActorAuth runs each actor's declarative auth flow once and writes the
// resulting header into that actor's Credentials.Headers. After this runs, the
// existing static-header injection path (authHeadersFor / withActorHeaders)
// carries the dynamic token with no further changes.
//
// Failures degrade, never abort: a failed login, non-2xx response, or missing
// token field logs a warning and leaves the actor unauthenticated so invariants
// that expect rejection can still be exercised.
func (s *Session) resolveActorAuth(ctx context.Context) {
	for i := range s.Config.Actors {
		a := &s.Config.Actors[i]
		if a.Auth == nil {
			continue
		}
		svcURL := s.serviceURLForActor(a)
		name, value, err := agent.ResolveAuthHeader(ctx, svcURL, *a)
		if err != nil {
			s.Logger.Warn("auth flow failed; degrading actor to unauthenticated",
				zap.String("actor", a.Name),
				// Intentionally no token or credential value logged.
				zap.Error(err),
			)
			continue
		}
		if a.Credentials.Headers == nil {
			a.Credentials.Headers = make(map[string]string)
		}
		a.Credentials.Headers[name] = value
		s.Logger.Info("auth flow resolved",
			zap.String("actor", a.Name),
			zap.String("header", name),
			zap.Int("value_len", len(value)),
		)
	}
}

// serviceURLForActor returns the service URL the actor authenticates against:
// the actor's own service if set, else the first configured service.
func (s *Session) serviceURLForActor(a *project.Actor) string {
	if a.Service != "" {
		for _, svc := range s.Config.Services {
			if svc.Name == a.Service {
				return svc.URL
			}
		}
	}
	if len(s.Config.Services) > 0 {
		return s.Config.Services[0].URL
	}
	return ""
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/session/ -run TestResolveActorAuth -v`
Expected: all PASS.

- [ ] **Step 6: Wire into `Session.Run`**

In `internal/session/lifecycle_run.go`, immediately after the `rp.initialize()` block (currently lines 26-29), add the call:

```go
	// Initialize
	if err := rp.initialize(); err != nil {
		rp.err = err
		return err
	}

	// Resolve dynamic auth (one login per actor with an auth: block) before
	// any test case runs. Failures degrade; never abort.
	s.resolveActorAuth(ctx)
```

- [ ] **Step 7: Wire into `Session.Resume`**

In `internal/session/lifecycle_resume.go`, immediately after the resume `initialize` step returns nil, add the same call:

```go
	s.resolveActorAuth(ctx)
```

placing it before the first phase that creates a `RuleEngine` (`executeRemainingCases`).

- [ ] **Step 8: Run the full session package + lint**

Run: `make lint && go test ./internal/session/ -race -v`
Expected: clean, including pre-existing session tests.

- [ ] **Step 9: Commit**

```bash
git add internal/session/auth_setup.go internal/session/auth_setup_test.go internal/session/lifecycle_run.go internal/session/lifecycle_resume.go
git commit -m "feat(session): resolve declarative auth headers at session setup"
```

---

## Task 6: Security audit + full verification

**Files:**
- No new source; verify-only. (If a leak is found, fix at the source and add a regression test here.)

- [ ] **Step 1: Verify no token leakage in logs/errors**

Run these greps and confirm every hit is benign (comment text or a non-token field):

```bash
grep -rn "token\|Token\|Authorization\|password\|Password" internal/head/agent/authflow.go internal/session/auth_setup.go
```

Acceptance: in `authflow.go`, the only things returned in errors are HTTP status, field names, and config strings — never `token`/`value`/body. In `auth_setup.go`, logs carry `header` name and `value_len` only, never `value`. If any line violates this, refactor it and add a test asserting the error/log string does not contain a known token.

- [ ] **Step 2: Add an explicit no-leak regression test**

Append to `internal/head/agent/authflow_test.go`:

```go
func TestResolveAuthHeader_ErrorNeverContainsToken(t *testing.T) {
	// Server returns 200 but with a token at a DIFFERENT key than token_from,
	// so extraction fails. The real token must not appear in the error.
	srv := newLoginServer(t, 200, `{"token":"REAL-SECRET-VALUE"}`, nil)
	defer srv.Close()

	actor := project.Actor{Auth: &project.AuthFlow{
		Login:     project.AuthLogin{Method: "POST", Path: "/login"},
		TokenFrom: "missing", // mismatch → extraction error
		InjectAs:  "Authorization: Bearer {token}",
	}}
	_, _, err := ResolveAuthHeader(context.Background(), srv.URL, actor)
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), "REAL-SECRET-VALUE") {
		t.Fatalf("error leaks token: %v", err)
	}
}
```

- [ ] **Step 3: Run the no-leak test**

Run: `go test ./internal/head/agent/ -run TestResolveAuthHeader_ErrorNeverContainsToken -v`
Expected: PASS.

- [ ] **Step 4: Full project check**

Run: `make check`
Expected: fmt clean, lint clean, all tests pass under `-race`.

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/authflow_test.go
git commit -m "test(agent): assert auth flow errors never leak token values"
```

---

## Acceptance

After all tasks land, configure an actor against a target requiring a dynamic JWT (e.g. open-agents `/api/dev/setup`):

```yaml
actors:
  - name: test-user
    service: api
    credentials:
      email: dev@openagents.local
      password: dev123456
    auth:
      login:
        method: POST
        path: /api/dev/setup
        body: { email: "{email}", password: "{password}" }
      token_from: token
      inject_as: "Authorization: Bearer {token}"
```

Run a session: the Agent's previously-401 protected-endpoint cases now receive a freshly minted Bearer token, return 200, and are judged normally by the Examiner. Sessions without any `auth:` block behave identically to before (zero breakage).

## Follow-up (out of scope)

Spec Component 3 — `cerberus auth discover` command (LLM reads target code, suggests an `AuthFlow` via structured output, writes `project.yaml` on confirmation) and the Scout runtime fallback (in-memory discovery when the block is missing/failing) — will be a separate plan. It reuses the schema and executor built here.
