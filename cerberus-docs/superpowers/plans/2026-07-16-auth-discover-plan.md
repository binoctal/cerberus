# Auth Discover Command Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `cerberus auth discover --actor <name>` command that reads the target service's source, has the LLM infer an `AuthFlow`, and writes the `auth:` block into `project.yaml` after confirmation.

**Architecture:** A standalone `internal/authdiscover/` package owns multi-language source selection (keyword-scored, budget-capped walk) plus one `ai.Driver.Decide` call whose JSON response is deserialized into a `discoverOutput` struct and mapped to `project.AuthFlow`. The command layer (`cmd/cerberus/main_auth.go`) builds the driver from the global config, calls `Discover`, prints the suggestion, confirms (honoring overwrite), and writes back via whole-file `yaml.Marshal` (the `main_discover.go` precedent). `project.ValidateAuthFlow` is exported so the package rejects malformed suggestions without duplicating rules.

**Tech Stack:** Go 1.25, `path/filepath`, `strings`, `encoding/json`, `gopkg.in/yaml.v3`, `github.com/spf13/cobra`, existing `internal/ai`, `internal/llm`, `internal/project`, `internal/config`.

**Reference spec:** `cerberus-docs/superpowers/specs/2026-07-16-auth-discover-design.md`

## Scope

Component 3a only — the `cerberus auth discover` command. The Scout runtime fallback (3b) is out of scope (separate plan).

## Global Constraints

- Go 1.25, module `github.com/binoctal/cerberus`, pure-Go (no CGo).
- Commit author: `binoctal <binoctal@gmail.com>`, no `Co-Authored-By`.
- Code comments and commit messages in English.
- Credential **values** are never placed in the LLM prompt, stdout, or user-facing errors. Only credential field names (`email`, `password`) are used as placeholder hints.
- The raw LLM response is never surfaced to the user (wrap `Driver.Decide` parse errors).
- `make check` (fmt + lint + race) green after every task.
- Follow existing comment density and naming idiom in touched packages.

---

## File Structure

- **Modify** `internal/project/validate_auth.go` — export `ValidateAuthFlow(*AuthFlow) error`; `validateAuthFlow` delegates to it.
- **Modify** `internal/project/authflow_schema_test.go` — add `ValidateAuthFlow` tests.
- **Create** `internal/authdiscover/select.go` — `selectSourceFiles(root string) ([]SourceFile, error)`: multi-language walk, keyword scoring, top-N + byte budget.
- **Create** `internal/authdiscover/select_test.go` — tempdir-based selection tests.
- **Create** `internal/authdiscover/discover.go` — `discoverOutput`, `ErrNoAuthFlow`, `Discover(ctx, driver, cfg, actorName, serviceURL) (*AuthFlow, error)`, prompt builder.
- **Create** `internal/authdiscover/discover_test.go` — mock-driver tests.
- **Create** `cmd/cerberus/main_auth.go` — `authCmd()` parent + `authDiscoverCmd()` child, `runAuthDiscover`, driver builder, write-back.
- **Create** `cmd/cerberus/main_auth_test.go` — CLI tests (dry-run, write-back, overwrite, unknown actor).
- **Modify** `cmd/cerberus/main.go` — register `authCmd()` in the root command tree.

---

## Task 1: Export `project.ValidateAuthFlow`

**Files:**
- Modify: `internal/project/validate_auth.go`
- Test: `internal/project/authflow_schema_test.go`

**Interfaces:**
- Consumes: `project.AuthFlow` (already defined).
- Produces: `project.ValidateAuthFlow(af *AuthFlow) error` — checks `login.method`, `login.path`, `token_from`, `inject_as` are non-empty and that `inject_as` contains a colon. Returns the first problem as an error, nil if valid. `authdiscover` (Task 3) calls this.

- [ ] **Step 1: Write the failing tests**

Append to `internal/project/authflow_schema_test.go`:

```go
func TestValidateAuthFlowExported(t *testing.T) {
	cases := []struct {
		name    string
		auth    *AuthFlow
		wantErr bool
	}{
		{name: "valid", auth: &AuthFlow{
			Login: AuthLogin{Method: "POST", Path: "/login"},
			TokenFrom: "token", InjectAs: "Authorization: Bearer {token}",
		}, wantErr: false},
		{name: "missing method", auth: &AuthFlow{
			Login: AuthLogin{Path: "/login"}, TokenFrom: "token", InjectAs: "Authorization: Bearer {token}",
		}, wantErr: true},
		{name: "missing token_from", auth: &AuthFlow{
			Login: AuthLogin{Method: "POST", Path: "/login"}, InjectAs: "Authorization: Bearer {token}",
		}, wantErr: true},
		{name: "missing inject_as", auth: &AuthFlow{
			Login: AuthLogin{Method: "POST", Path: "/login"}, TokenFrom: "token",
		}, wantErr: true},
		{name: "inject_as without colon", auth: &AuthFlow{
			Login: AuthLogin{Method: "POST", Path: "/login"}, TokenFrom: "token", InjectAs: "Bearer {token}",
		}, wantErr: true},
		{name: "nil auth", auth: nil, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateAuthFlow(tc.auth)
			if tc.wantErr && err == nil {
				t.Fatal("want error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("want nil, got %v", err)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/project/ -run TestValidateAuthFlowExported -v`
Expected: build failure — `ValidateAuthFlow` undefined.

- [ ] **Step 3: Implement `ValidateAuthFlow` and refactor `validateAuthFlow`**

Replace the body of `internal/project/validate_auth.go` with:

```go
package project

import (
	"fmt"
	"strings"
)

// ValidateAuthFlow checks an AuthFlow's structural completeness: required
// fields (login.method, login.path, token_from, inject_as) are non-empty and
// inject_as contains a colon so it can split into a header name/value pair.
// It does NOT check interpolation variables against credentials — that needs
// an Actor and stays in validateAuthFlow below. Returns nil if valid.
func ValidateAuthFlow(af *AuthFlow) error {
	if af == nil {
		return fmt.Errorf("auth flow is required")
	}
	if af.Login.Method == "" {
		return fmt.Errorf("login.method is required")
	}
	if af.Login.Path == "" {
		return fmt.Errorf("login.path is required")
	}
	if af.TokenFrom == "" {
		return fmt.Errorf("token_from is required")
	}
	if af.InjectAs == "" {
		return fmt.Errorf("inject_as is required")
	}
	if !strings.Contains(af.InjectAs, ":") {
		return fmt.Errorf("inject_as %q must be a 'Name: Value' header", af.InjectAs)
	}
	return nil
}

// validateAuthFlow validates an actor's optional declarative auth block for
// config-time errors. Returns the first problem as a string (validation
// collects per-actor into ValidationError).
func validateAuthFlow(actorIdx int, a Actor) string {
	if a.Auth == nil {
		return ""
	}
	prefix := fmt.Sprintf("actors[%d].auth", actorIdx)
	if err := ValidateAuthFlow(a.Auth); err != nil {
		return prefix + "." + err.Error()
	}
	// Every {email}/{password} referenced in login.body must have a matching
	// non-empty credential field; otherwise login would interpolate to "" at
	// runtime with no warning.
	for _, v := range a.Auth.Login.Body {
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

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/project/ -run 'TestValidateAuthFlow|TestValidateAuthFlowExported|TestActorAuth' -v`
Expected: all PASS (existing validation tests still pass under the refactor).

- [ ] **Step 5: Run package lint + race**

Run: `make lint && go test ./internal/project/ -race`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/project/validate_auth.go internal/project/authflow_schema_test.go
git commit -m "feat(project): export ValidateAuthFlow with inject_as colon check"
```

---

## Task 2: `authdiscover` source-file selection

**Files:**
- Create: `internal/authdiscover/select.go`
- Create: `internal/authdiscover/select_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `SourceFile{Path, Content string}`, `selectSourceFiles(root string) ([]SourceFile, error)`. Walks `root`, skips vendored/build dirs, keeps supported source extensions, scores by auth-keyword hits, returns top-N within a byte budget. Task 3 consumes the result.

- [ ] **Step 1: Write the failing tests**

Create `internal/authdiscover/select_test.go`:

```go
package authdiscover

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestSelectSourceFiles_RanksAuthRelevant(t *testing.T) {
	root := t.TempDir()
	// High-relevance: mentions login + jwt + token.
	writeFile(t, root, "src/auth/login.go", "package auth\n// login handler issues jwt token\n")
	// Low-relevance: unrelated route.
	writeFile(t, root, "src/routes/misc.go", "package routes\n// misc handler\n")
	// Ignored: vendored.
	writeFile(t, root, "vendor/lib/secret.go", "package lib\n// login token here\n")
	// Ignored: unsupported extension.
	writeFile(t, root, "src/readme.md", "# login token auth\n")

	got, err := selectSourceFiles(root)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("want at least one selected file")
	}
	// Top hit must be the auth-relevant login.go, not misc.go.
	if filepath.Base(got[0].Path) != "login.go" {
		t.Fatalf("top file = %v, want login.go", got)
	}
	// Vendored and .md files must never appear.
	for _, f := range got {
		if strings.Contains(f.Path, "vendor") {
			t.Fatalf("vendored file selected: %s", f.Path)
		}
		if filepath.Ext(f.Path) == ".md" {
			t.Fatalf("non-source file selected: %s", f.Path)
		}
	}
}

func TestSelectSourceFiles_AdmitsMultipleLanguages(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "svc/handler.ts", "// login token\n")
	writeFile(t, root, "svc/app.py", "# login token\n")
	got, err := selectSourceFiles(root)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	exts := map[string]bool{}
	for _, f := range got {
		exts[filepath.Ext(f.Path)] = true
	}
	if !exts[".ts"] || !exts[".py"] {
		t.Fatalf("want .ts and .py selected, got %v", exts)
	}
}

func TestSelectSourceFiles_MissingRoot(t *testing.T) {
	if _, err := selectSourceFiles(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("want error for missing root")
	}
}
```

Add `"strings"` to the test file imports.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/authdiscover/ -run TestSelectSourceFiles -v`
Expected: build failure — package `authdiscover` does not exist.

- [ ] **Step 3: Implement selection**

Create `internal/authdiscover/select.go`:

```go
// Package authdiscover infers a declarative AuthFlow for an actor by reading
// the target service's source and asking the LLM. It is a one-shot authoring
// aid invoked by the `cerberus auth discover` command; it never runs at
// session time and never writes files itself.
package authdiscover

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SourceFile is a selected source file with the content the LLM will see.
type SourceFile struct {
	Path    string
	Content string
}

// Supported source extensions. Multi-language because login flows live in
// whatever stack the target service uses.
var sourceExts = map[string]bool{
	".go": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".py": true,
}

// Dirs never worth scanning for a login flow.
func isSkippedDir(dir string) bool {
	switch dir {
	case "vendor", "node_modules", "build", "dist", ".git", ".cerberus":
		return true
	}
	return false
}

// authKeywords raise a file's relevance. Lowercase substring match.
var authKeywords = []string{
	"login", "signin", "sign-in", "auth", "session", "jwt",
	"token", "bearer", "middleware", "route", "passport", "handler",
}

// maxFiles caps how many files are returned. A login flow spans a handful of
// files; more just burns prompt budget.
const maxFiles = 8

// maxBytes caps total selected content so the prompt fits the model window.
const maxBytes = 24000

type scored struct {
	file SourceFile
	hits int
}

// selectSourceFiles walks root, drops vendored/build dirs and non-source files,
// scores remaining files by auth-keyword hits, and returns the top files within
// a byte budget. Returns an error if root is missing.
func selectSourceFiles(root string) ([]SourceFile, error) {
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("code root %q is not a readable directory", root)
	}
	var picks []scored
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if isSkippedDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !sourceExts[filepath.Ext(path)] {
			return nil
		}
		content, rErr := os.ReadFile(path)
		if rErr != nil {
			return nil // skip unreadable files rather than aborting
		}
		lower := strings.ToLower(string(content))
		hits := 0
		for _, kw := range authKeywords {
			hits += strings.Count(lower, kw)
		}
		picks = append(picks, scored{file: SourceFile{Path: path, Content: string(content)}, hits: hits})
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Rank by relevance (desc); stable tiebreak keeps deterministic order.
	sort.SliceStable(picks, func(i, j int) bool { return picks[i].hits > picks[j].hits })

	out := make([]SourceFile, 0, maxFiles)
	total := 0
	for _, p := range picks {
		if len(out) >= maxFiles || total >= maxBytes {
			break
		}
		if total+len(p.file.Content) > maxBytes {
			continue // skip oversized rather than truncating mid-file
		}
		out = append(out, p.file)
		total += len(p.file.Content)
	}
	return out, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/authdiscover/ -run TestSelectSourceFiles -v`
Expected: all PASS.

- [ ] **Step 5: Lint + race**

Run: `make lint && go test ./internal/authdiscover/ -race`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/authdiscover/select.go internal/authdiscover/select_test.go
git commit -m "feat(authdiscover): add keyword-scored multi-language source selector"
```

---

## Task 3: `Discover` — LLM inference + parse

**Files:**
- Create: `internal/authdiscover/discover.go`
- Create: `internal/authdiscover/discover_test.go`

**Interfaces:**
- Consumes: `ai.Driver` (`Decide(ctx, prompt, schema any) error`), `project.Config` (`.Code.Root`, `.Actors`), `project.ValidateAuthFlow`, `selectSourceFiles` (Task 2).
- Produces: `ErrNoAuthFlow`, `Discover(ctx, driver, cfg, actorName, serviceURL) (*project.AuthFlow, error)`. Task 4 (CLI) calls this; it returns the inferred flow or `ErrNoAuthFlow` when the model reports no login flow, and a wrapped error (raw response hidden) on parse/validation failure.

- [ ] **Step 1: Write the failing tests**

Create `internal/authdiscover/discover_test.go`:

```go
package authdiscover

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

func driverReturning(resp string) (*ai.Driver, error) {
	mock := llm.NewMockClient(map[string]string{"default": resp})
	return ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000)), nil
}

func TestDiscover_ValidFlow(t *testing.T) {
	root := t.TempDir()
	// A source file so selection has something to find.
	if err := writeRootFile(root, "svc/login.go", "package svc\n// login jwt token\n"); err != nil {
		t.Fatal(err)
	}
	driver, err := driverReturning(`{
		"found": true,
		"login": {"method": "POST", "path": "/api/dev/setup", "body": {"email": "{email}", "password": "{password}"}},
		"token_from": "token",
		"inject_as": "Authorization: Bearer {token}",
		"notes": "looks like the dev setup endpoint"
	}`)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &project.Config{
		Code: project.CodeConfig{Root: root},
		Actors: []project.Actor{{Name: "u", Credentials: project.CredentialRef{Email: "a@b.c", Password: "pw"}}},
	}
	af, err := Discover(context.Background(), driver, cfg, "u", "http://svc.local")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if af.Login.Method != "POST" || af.Login.Path != "/api/dev/setup" {
		t.Fatalf("login = %+v", af.Login)
	}
	if af.TokenFrom != "token" || af.InjectAs != "Authorization: Bearer {token}" {
		t.Fatalf("got token_from=%q inject_as=%q", af.TokenFrom, af.InjectAs)
	}
	if af.Login.Body["email"] != "{email}" {
		t.Fatalf("body email = %q (must be placeholder)", af.Login.Body["email"])
	}
}

func TestDiscover_NoAuthFlow(t *testing.T) {
	root := t.TempDir()
	if err := writeRootFile(root, "svc/public.go", "package svc\n// public route\n"); err != nil {
		t.Fatal(err)
	}
	driver, err := driverReturning(`{"found": false, "notes": "no login endpoint"}`)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &project.Config{
		Code: project.CodeConfig{Root: root},
		Actors: []project.Actor{{Name: "u"}},
	}
	_, err = Discover(context.Background(), driver, cfg, "u", "http://svc.local")
	if !errors.Is(err, ErrNoAuthFlow) {
		t.Fatalf("want ErrNoAuthFlow, got %v", err)
	}
}

func TestDiscover_UnparseableHidesRaw(t *testing.T) {
	root := t.TempDir()
	if err := writeRootFile(root, "svc/login.go", "package svc\n// login\n"); err != nil {
		t.Fatal(err)
	}
	driver, err := driverReturning("not json at all SECRET-MARKER-XYZ")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &project.Config{
		Code: project.CodeConfig{Root: root},
		Actors: []project.Actor{{Name: "u"}},
	}
	_, err = Discover(context.Background(), driver, cfg, "u", "http://svc.local")
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), "SECRET-MARKER-XYZ") {
		t.Fatalf("error leaks raw LLM response: %v", err)
	}
	if errors.Is(err, ErrNoAuthFlow) {
		t.Fatal("parse failure must not be reported as ErrNoAuthFlow")
	}
}

func TestDiscover_PromptHasShapeAndNoCredentialValues(t *testing.T) {
	root := t.TempDir()
	if err := writeRootFile(root, "svc/login.go", "package svc\n// login\n"); err != nil {
		t.Fatal(err)
	}
	driver, err := driverReturning(`{"found": true, "login": {"method":"POST","path":"/login"}, "token_from":"token", "inject_as":"X: {token}"}`)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &project.Config{
		Code: project.CodeConfig{Root: root},
		Actors: []project.Actor{{Name: "u", Credentials: project.CredentialRef{Email: "REAL-EMAIL-VALUE", Password: "REAL-PASSWORD-VALUE"}}},
	}
	_ = driver // driver is not needed here; this test checks prompt construction only.
	prompt := buildDiscoverPrompt("http://svc.local", selectFilesOrEmpty(root), credentialFieldNames(cfg, "u"))
	// JSON shape is inlined.
	for _, token := range []string{"found", "login", "token_from", "inject_as"} {
		if !strings.Contains(prompt, token) {
			t.Fatalf("prompt missing JSON shape token %q", token)
		}
	}
	// Credential field names are hinted, values are not.
	if !strings.Contains(prompt, "{email}") || !strings.Contains(prompt, "{password}") {
		t.Fatal("prompt must reference {email}/{password} placeholders")
	}
	if strings.Contains(prompt, "REAL-EMAIL-VALUE") || strings.Contains(prompt, "REAL-PASSWORD-VALUE") {
		t.Fatal("prompt must not contain credential values")
	}
}

func TestDiscover_UnknownActor(t *testing.T) {
	driver, err := driverReturning(`{"found": true}`)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &project.Config{}
	_, err = Discover(context.Background(), driver, cfg, "nope", "http://svc.local")
	if err == nil || errors.Is(err, ErrNoAuthFlow) {
		t.Fatalf("want a hard error for unknown actor, got %v", err)
	}
}
```

Add a tiny test helper for writing into the code root at the top of the test file (after imports):

```go
func writeRootFile(root, rel, content string) error {
	return os.WriteFile(filepath.Join(root, rel), []byte(content), 0644)
}

// selectFilesOrEmpty is a test helper exposing selection without erroring on a
// missing root (used by the prompt-shape test).
func selectFilesOrEmpty(root string) []SourceFile {
	f, err := selectSourceFiles(root)
	if err != nil {
		return nil
	}
	return f
}
```

and add `"os"`, `"path/filepath"` to the test imports.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/authdiscover/ -run TestDiscover -v`
Expected: build failure — `Discover`, `ErrNoAuthFlow`, `buildDiscoverPrompt`, etc. undefined.

- [ ] **Step 3: Implement `Discover`**

Create `internal/authdiscover/discover.go`:

```go
package authdiscover

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/project"
)

// ErrNoAuthFlow signals the model found no login flow in the target. It is
// distinct from a hard error: the command reports it and exits cleanly rather
// than treating a public API as a failure.
var ErrNoAuthFlow = errors.New("no login flow found")

// discoverOutput is the JSON shape the LLM must return. The Driver deserializes
// the response into this struct (ParseStructuredOutput tolerates markdown
// fences). Found/Notes are not part of AuthFlow.
type discoverOutput struct {
	Found bool `json:"found"`
	Login struct {
		Method  string            `json:"method"`
		Path    string            `json:"path"`
		Body    map[string]string `json:"body"`
		Headers map[string]string `json:"headers"`
	} `json:"login"`
	TokenFrom string `json:"token_from"`
	InjectAs  string `json:"inject_as"`
	Notes     string `json:"notes"`
}

// Discover reads the target service's source, asks the LLM to infer a single
// login flow, and returns it (not written to disk). The driver is passed in so
// tests inject a mock and Discover never builds LLM clients. serviceURL is the
// base the model's login.path is relative to.
//
// On a parse/validation failure the returned error wraps the cause WITHOUT the
// raw LLM response (Driver.Decide embeds it; we hide it). On "no login flow" it
// returns ErrNoAuthFlow.
func Discover(ctx context.Context, driver *ai.Driver, cfg *project.Config, actorName, serviceURL string) (*project.AuthFlow, error) {
	if _, err := findActor(cfg, actorName); err != nil {
		return nil, err
	}

	files, err := selectSourceFiles(cfg.Code.Root)
	if err != nil {
		return nil, fmt.Errorf("select source files: %w", err)
	}

	prompt := buildDiscoverPrompt(serviceURL, files, credentialFieldNames(cfg, actorName))

	var out discoverOutput
	if err := driver.Decide(ctx, prompt, &out); err != nil {
		// Driver.Decide's error embeds the raw response; do not propagate it.
		return nil, errors.New("could not parse LLM output into AuthFlow")
	}

	if !out.Found {
		return nil, ErrNoAuthFlow
	}

	af := &project.AuthFlow{
		Login: project.AuthLogin{
			Method:  out.Login.Method,
			Path:    out.Login.Path,
			Body:    out.Login.Body,
			Headers: out.Login.Headers,
		},
		TokenFrom: out.TokenFrom,
		InjectAs:  out.InjectAs,
	}
	if vErr := project.ValidateAuthFlow(af); vErr != nil {
		return nil, fmt.Errorf("model produced an invalid auth flow: %w", vErr)
	}
	return af, nil
}

func findActor(cfg *project.Config, name string) (project.Actor, error) {
	if cfg == nil {
		return project.Actor{}, errors.New("config is nil")
	}
	for _, a := range cfg.Actors {
		if a.Name == name {
			return a, nil
		}
	}
	return project.Actor{}, fmt.Errorf("actor %q not found in config", name)
}

// credentialFieldNames returns the credential field names the actor has, so the
// prompt can ask for {email}/{password} placeholders. Values are never included.
func credentialFieldNames(cfg *project.Config, actorName string) []string {
	a, err := findActor(cfg, actorName)
	if err != nil {
		return nil
	}
	var names []string
	if a.Credentials.Email != "" {
		names = append(names, "email")
	}
	if a.Credentials.Password != "" {
		names = append(names, "password")
	}
	return names
}

// buildDiscoverPrompt assembles the prompt. It MUST inline the JSON shape
// because ai.Driver.Decide does not inject the schema into the prompt — it only
// parses the response.
func buildDiscoverPrompt(serviceURL string, files []SourceFile, credFields []string) string {
	var b strings.Builder
	b.WriteString("You are inferring the login/authentication flow for a web service.\n")
	b.WriteString("Read the source snippets below, locate the login endpoint, its request body shape, and the JSON path of the returned token.\n\n")
	fmt.Fprintf(&b, "The service URL is %q; login.path should be relative to it unless absolute.\n\n", serviceURL)
	b.WriteString("Respond with ONLY a JSON object of this exact shape:\n")
	b.WriteString("{\n")
	b.WriteString("  \"found\": <true if a login flow exists, false if the API is public>,\n")
	b.WriteString("  \"login\": {\n")
	b.WriteString("    \"method\": \"POST\",\n")
	b.WriteString("    \"path\": \"/login\",                 // relative to the service URL\n")
	b.WriteString("    \"body\": {\"field\": \"...\"},        // use placeholders for credentials\n")
	b.WriteString("    \"headers\": {}                      // optional static headers\n")
	b.WriteString("  },\n")
	b.WriteString("  \"token_from\": \"token\",             // dot-path into the JSON response, e.g. data.accessToken\n")
	b.WriteString("  \"inject_as\": \"Authorization: Bearer {token}\",\n")
	b.WriteString("  \"notes\": \"one-line rationale\"\n")
	b.WriteString("}\n\n")
	if len(credFields) > 0 {
		b.WriteString("Credential placeholders available (by field name) in login.body: ")
		for _, f := range credFields {
			fmt.Fprintf(&b, "{%s} ", f)
		}
		b.WriteString("— NEVER copy real credential values.\n\n")
	}
	b.WriteString("Source snippets:\n\n")
	for _, f := range files {
		b.WriteString("--- " + filepath.Base(f.Path) + " ---\n")
		b.WriteString(f.Content)
		if !strings.HasSuffix(f.Content, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/authdiscover/ -race -v`
Expected: all PASS.

- [ ] **Step 5: Lint**

Run: `make lint`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/authdiscover/discover.go internal/authdiscover/discover_test.go
git commit -m "feat(authdiscover): infer AuthFlow from source via LLM"
```

---

## Task 4: CLI command + write-back

**Files:**
- Create: `cmd/cerberus/main_auth.go`
- Create: `cmd/cerberus/main_auth_test.go`

**Interfaces:**
- Consumes: `authdiscover.Discover` + `ErrNoAuthFlow` (Task 3), `project.LoadFromFile` / `yaml.Marshal` / `os.WriteFile` (the `main_discover.go` pattern), `config.Load()` (global LLM creds), `llm.NewClientWithConfig` + `ai.NewDriver`.
- Produces: `authCmd() *cobra.Command` (registered in Task 5). `runAuthDiscover` is the testable core; it takes an injected `*ai.Driver` so tests avoid real LLM calls.

- [ ] **Step 1: Write the failing tests**

Create `cmd/cerberus/main_auth_test.go`:

```go
package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/authdiscover"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

func mockDriver(resp string) *ai.Driver {
	mock := llm.NewMockClient(map[string]string{"default": resp})
	return ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000))
}

func writeProjectYAML(t *testing.T, workDir, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(workDir, ".cerberus"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, ".cerberus", "project.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func readProjectYAML(t *testing.T, workDir string) *project.Config {
	t.Helper()
	cfg, err := project.LoadFromFile(filepath.Join(workDir, ".cerberus", "project.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestRunAuthDiscover_DryRunDoesNotWrite(t *testing.T) {
	workDir := t.TempDir()
	writeProjectYAML(t, workDir, "actors:\n  - name: u\n    credentials: {email: a@b.c}\n")
	before, _ := os.ReadFile(filepath.Join(workDir, ".cerberus", "project.yaml"))

	opts := authDiscoverOpts{
		Actor:   "u",
		DryRun:  true,
		confirm: func(string) bool { return true },
	}
	driver := mockDriver(`{"found": true, "login": {"method":"POST","path":"/login"}, "token_from":"token", "inject_as":"Authorization: Bearer {token}"}`)
	if err := runAuthDiscover(context.Background(), workDir, driver, opts); err != nil {
		t.Fatalf("err: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(workDir, ".cerberus", "project.yaml"))
	if string(before) != string(after) {
		t.Fatal("dry-run must not modify project.yaml")
	}
}

func TestRunAuthDiscover_WriteOnConfirm(t *testing.T) {
	workDir := t.TempDir()
	writeProjectYAML(t, workDir, "actors:\n  - name: u\n    credentials: {email: a@b.c}\n")
	opts := authDiscoverOpts{
		Actor:   "u",
		DryRun:  false,
		confirm: func(string) bool { return true },
	}
	driver := mockDriver(`{"found": true, "login": {"method":"POST","path":"/login"}, "token_from":"token", "inject_as":"Authorization: Bearer {token}"}`)
	if err := runAuthDiscover(context.Background(), workDir, driver, opts); err != nil {
		t.Fatalf("err: %v", err)
	}
	cfg := readProjectYAML(t, workDir)
	if cfg.Actors[0].Auth == nil || cfg.Actors[0].Auth.Login.Path != "/login" {
		t.Fatalf("auth not written: %+v", cfg.Actors[0].Auth)
	}
}

func TestRunAuthDiscover_OverwriteRequiresConfirm(t *testing.T) {
	workDir := t.TempDir()
	writeProjectYAML(t, workDir, "actors:\n  - name: u\n    credentials: {email: a@b.c}\n    auth:\n      login: {method: POST, path: /old}\n      token_from: token\n      inject_as: \"Authorization: Bearer {token}\"\n")
	// Decline overwrite -> old block preserved.
	opts := authDiscoverOpts{
		Actor:   "u",
		DryRun:  false,
		confirm: func(string) bool { return false },
	}
	driver := mockDriver(`{"found": true, "login": {"method":"POST","path":"/new"}, "token_from":"token", "inject_as":"Authorization: Bearer {token}"}`)
	if err := runAuthDiscover(context.Background(), workDir, driver, opts); err != nil {
		t.Fatalf("err: %v", err)
	}
	cfg := readProjectYAML(t, workDir)
	if cfg.Actors[0].Auth.Login.Path != "/old" {
		t.Fatalf("overwrite happened despite decline: %+v", cfg.Actors[0].Auth)
	}
}

func TestRunAuthDiscover_UnknownActor(t *testing.T) {
	workDir := t.TempDir()
	writeProjectYAML(t, workDir, "actors:\n  - name: other\n    credentials: {email: a@b.c}\n")
	opts := authDiscoverOpts{Actor: "missing", confirm: func(string) bool { return true }}
	driver := mockDriver(`{"found": true}`)
	if err := runAuthDiscover(context.Background(), workDir, driver, opts); err == nil {
		t.Fatal("want error for unknown actor")
	}
}

func TestRunAuthDiscover_NoAuthFlowIsNotError(t *testing.T) {
	workDir := t.TempDir()
	writeProjectYAML(t, workDir, "actors:\n  - name: u\n    credentials: {email: a@b.c}\n")
	opts := authDiscoverOpts{Actor: "u", confirm: func(string) bool { return true }}
	driver := mockDriver(`{"found": false}`)
	if err := runAuthDiscover(context.Background(), workDir, driver, opts); err != nil {
		t.Fatalf("ErrNoAuthFlow must not be an error from the command: %v", err)
	}
	cfg := readProjectYAML(t, workDir)
	if cfg.Actors[0].Auth != nil {
		t.Fatal("no auth should be written when none found")
	}
}

// Ensure the package compiles against authdiscover's public surface.
var _ = authdiscover.ErrNoAuthFlow
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/cerberus/ -run TestRunAuthDiscover -v`
Expected: build failure — `authDiscoverOpts`, `runAuthDiscover` undefined.

- [ ] **Step 3: Implement the command**

Create `cmd/cerberus/main_auth.go`:

```go
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/authdiscover"
	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

var (
	authDiscoverActor  string
	authDiscoverSvc    string
	authDiscoverDryRun bool
)

// authCmd is the parent for auth-related subcommands.
func authCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authoring aids for declarative auth flows",
	}
	cmd.AddCommand(authDiscoverCmd())
	return cmd
}

func authDiscoverCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Infer an auth flow from source and write it to project.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			driver, err := newAuthDiscoverDriver()
			if err != nil {
				return err
			}
			return runAuthDiscover(cmd.Context(), ".", driver, authDiscoverOpts{
				Actor:   authDiscoverActor,
				Service: authDiscoverSvc,
				DryRun:  authDiscoverDryRun,
				confirm: promptConfirm(os.Stdin, os.Stdout),
			})
		},
	}
	cmd.Flags().StringVar(&authDiscoverActor, "actor", "", "actor whose auth: block is written (required)")
	cmd.Flags().StringVar(&authDiscoverSvc, "service", "", "service whose source is read (default: actor.Service, else first)")
	cmd.Flags().BoolVar(&authDiscoverDryRun, "dry-run", false, "print suggestion without writing")
	_ = cmd.MarkFlagRequired("actor")
	return cmd
}

// authDiscoverOpts holds parsed command inputs. confirm abstracts the y/N
// prompt so tests inject a deterministic answerer.
type authDiscoverOpts struct {
	Actor   string
	Service string
	DryRun  bool
	confirm func(prompt string) bool
}

// runAuthDiscover is the testable core. It loads project.yaml, infers the
// AuthFlow via authdiscover, prints it, and on confirmation writes it back
// (whole-file rewrite). ErrNoAuthFlow is reported via stdout, not returned.
func runAuthDiscover(ctx context.Context, workDir string, driver *ai.Driver, opts authDiscoverOpts) error {
	if opts.Actor == "" {
		return errors.New("--actor is required")
	}
	cfgPath := filepath.Join(workDir, ".cerberus", "project.yaml")
	cfg, err := project.LoadFromFile(cfgPath)
	if err != nil {
		return fmt.Errorf("load project.yaml: %w", err)
	}
	serviceURL := resolveServiceURL(cfg, opts.Actor, opts.Service)

	af, err := authdiscover.Discover(ctx, driver, cfg, opts.Actor, serviceURL)
	if errors.Is(err, authdiscover.ErrNoAuthFlow) {
		fmt.Printf("no login endpoint found for actor %q\n", opts.Actor)
		return nil
	}
	if err != nil {
		return err
	}

	// Render only the auth block (no credential values live here — only
	// placeholders and endpoint shape).
	block, _ := yaml.Marshal(map[string]any{"auth": af})
	fmt.Printf("Suggested auth for %q:\n%s\n", opts.Actor, string(block))

	if opts.DryRun {
		return nil
	}

	existing := actorAuthPath(cfg, opts.Actor) != ""
	question := fmt.Sprintf("Write to actor %q in project.yaml? [y/N]", opts.Actor)
	if existing {
		question = fmt.Sprintf("Actor %q already has an auth block. Overwrite? [y/N]", opts.Actor)
	}
	if opts.confirm == nil || !opts.confirm(question) {
		fmt.Println("aborted; no changes written")
		return nil
	}

	setActorAuth(cfg, opts.Actor, af)
	if err := os.MkdirAll(filepath.Join(workDir, ".cerberus"), 0755); err != nil {
		return err
	}
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(cfgPath, out, 0644)
}

// resolveServiceURL mirrors Session.serviceURLForActor (Component 2): the
// actor's service if set, else the named service, else the first service.
func resolveServiceURL(cfg *project.Config, actorName, serviceFlag string) string {
	for i := range cfg.Actors {
		if cfg.Actors[i].Name == actorName {
			name := serviceFlag
			if name == "" {
				name = cfg.Actors[i].Service
			}
			for _, svc := range cfg.Services {
				if svc.Name == name {
					return svc.URL
				}
			}
			break
		}
	}
	if len(cfg.Services) > 0 {
		return cfg.Services[0].URL
	}
	return ""
}

func actorAuthPath(cfg *project.Config, actorName string) string {
	for _, a := range cfg.Actors {
		if a.Name == actorName && a.Auth != nil {
			return a.Auth.Login.Path
		}
	}
	return ""
}

func setActorAuth(cfg *project.Config, actorName string, af *project.AuthFlow) {
	for i := range cfg.Actors {
		if cfg.Actors[i].Name == actorName {
			cfg.Actors[i].Auth = af
			return
		}
	}
}

// newAuthDiscoverDriver builds the single LLM driver from global config + the
// project's model. No LLM-client code is hidden inside authdiscover.
func newAuthDiscoverDriver() (*ai.Driver, error) {
	gcfg := config.Load()
	projCfg, err := project.LoadFromFile(filepath.Join(".", ".cerberus", "project.yaml"))
	if err != nil {
		return nil, fmt.Errorf("load project.yaml: %w", err)
	}
	model := projCfg.Settings.AIBudget.Model
	client, err := llm.NewClientWithConfig(llm.ClientConfig{
		Model:      model,
		APIKey:     gcfg.LLMAPIKey,
		BaseURL:    gcfg.LLMBaseURL,
		AuthScheme: gcfg.LLMAuthScheme,
	})
	if err != nil {
		return nil, fmt.Errorf("build llm client: %w", err)
	}
	total := projCfg.Settings.AIBudget.SessionTotalTokens
	if total <= 0 {
		total = 200000
	}
	perCall := projCfg.Settings.AIBudget.PerCallLimit
	if perCall <= 0 {
		perCall = 10000
	}
	return ai.NewDriver(client, ai.NewTokenBudget(total, perCall)), nil
}

// promptConfirm returns a confirmer that reads a y/N line from in.
func promptConfirm(in io.Reader, out io.Writer) func(string) bool {
	return func(question string) bool {
		fmt.Fprint(out, question+" ")
		scanner := bufio.NewScanner(in)
		if !scanner.Scan() {
			return false
		}
		line := strings.ToLower(strings.TrimSpace(scanner.Text()))
		return line == "y" || line == "yes"
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/cerberus/ -run TestRunAuthDiscover -v`
Expected: all PASS.

- [ ] **Step 5: Lint + race**

Run: `make lint && go test ./cmd/cerberus/ -race`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add cmd/cerberus/main_auth.go cmd/cerberus/main_auth_test.go
git commit -m "feat(cmd): add cerberus auth discover command with write-back"
```

---

## Task 5: Register command + full verification

**Files:**
- Modify: `cmd/cerberus/main.go` (the `rootCmd.AddCommand(...)` block)

**Interfaces:**
- Consumes: `authCmd()` (Task 4).
- Produces: `cerberus auth discover` available on the CLI.

- [ ] **Step 1: Register the command**

In `cmd/cerberus/main.go`, add `authCmd()` to the existing `rootCmd.AddCommand(...)` call (the block around line 15). For example, if it reads:

```go
	rootCmd.AddCommand(
		runCmd(),
		discoverCmd(),
	)
```

change it to:

```go
	rootCmd.AddCommand(
		runCmd(),
		discoverCmd(),
		authCmd(),
	)
```

(Keep all existing entries; only add `authCmd()`.)

- [ ] **Step 2: Verify the CLI builds and exposes the command**

Run: `go build -o /tmp/cerberus ./cmd/cerberus && /tmp/cerberus auth discover --help`
Expected: help text printed, `--actor`, `--service`, `--dry-run` flags listed; exit 0.

- [ ] **Step 3: Confirm `--actor` is required**

Run: `/tmp/cerberus auth discover 2>&1; echo "exit=$?"`
Expected: non-zero exit, error mentioning `--actor` is required.

- [ ] **Step 4: Full project check**

Run: `make check`
Expected: fmt clean, lint clean, all tests pass under `-race`.

- [ ] **Step 5: Commit**

```bash
git add cmd/cerberus/main.go
git commit -m "feat(cmd): register cerberus auth command tree"
```

---

## Acceptance

Against a target with a real login endpoint (e.g. open-agents `/api/dev/setup`):

1. Configure `.cerberus/project.yaml` minimally: an actor with credentials and a service URL; set `code.root` to the target repo.
2. Run `cerberus auth discover --actor test-user`.
3. The command prints a suggested `auth:` block whose `login.path`, body fields, and `token_from` match the target's real login flow.
4. Confirm; the block lands in `project.yaml`.
5. Run a session — the Agent authenticates via the now-permanent block.

A target with no login flow prints `no login endpoint found` and writes nothing. `--dry-run` never writes. An actor with an existing block prompts for overwrite.

## Out of scope (future)

- Scout runtime fallback (spec Component 3b) — in-memory discovery during planning. Reuses `authdiscover`.
- Cookie / multi-step OAuth.
- Re-discovery UX beyond the overwrite prompt.
