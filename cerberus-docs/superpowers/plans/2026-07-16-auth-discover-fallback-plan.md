# Auth Fallback (Session-Only Discovery) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When an actor has no `auth:` block and `settings.auth.discover_fallback` is on, discover an `AuthFlow` in-memory at session setup via the already-merged `authdiscover` package, use it for that session only, and log a persist hint.

**Architecture:** A small opt-in branch inside the existing `Session.resolveActorAuth` (`internal/session/auth_setup.go`): for an actor with `Auth == nil`, if the fallback is enabled and `s.Driver` is non-nil, call `authdiscover.Discover`; on success set `a.Auth` in-memory (never write `project.yaml`) and fall through to the existing `agent.ResolveAuthHeader` login; on failure degrade. One new `AuthSettings` field on `project.Settings`.

**Tech Stack:** Go 1.25, existing `internal/session`, `internal/project`, `internal/authdiscover`, `internal/ai`, `internal/llm`.

**Reference spec:** `cerberus-docs/superpowers/specs/2026-07-16-auth-discover-fallback-design.md`

## Scope

Component 3b only — session-only runtime fallback. Reuses the `authdiscover` package merged in Component 3a. No CLI changes, no disk writes.

## Global Constraints

- Go 1.25, module `github.com/binoctal/cerberus`, pure-Go (no CGo).
- Commit author: `binoctal <binoctal@gmail.com>`, no `Co-Authored-By`.
- Code comments and commit messages in English.
- Credential **values** are never logged or placed in the prompt (`authdiscover` guarantee).
- The discovered flow is **in-memory only** — never write `project.yaml`.
- `make check` (fmt + lint + race) green after every task.
- Follow existing comment density and naming idiom.

---

## File Structure

- **Modify** `internal/project/schema.go` — add `AuthSettings` type and `Auth AuthSettings` field on `Settings`.
- **Modify** `internal/project/authflow_schema_test.go` (or a settings test file) — yaml round-trip for `settings.auth.discover_fallback`.
- **Modify** `internal/session/auth_setup.go` — fallback branch in `resolveActorAuth` + new `discoverActorAuth` helper; add `authdiscover` import.
- **Modify** `internal/session/auth_setup_test.go` — fallback tests with a mock driver.

---

## Task 1: `Settings.Auth.DiscoverFallback` field

**Files:**
- Modify: `internal/project/schema.go`
- Test: `internal/project/authflow_schema_test.go`

**Interfaces:**
- Produces: `project.AuthSettings{DiscoverFallback bool}` and `Settings.Auth AuthSettings` (yaml `auth`). Task 2 reads `s.Config.Settings.Auth.DiscoverFallback`.

- [ ] **Step 1: Write the failing test**

Append to `internal/project/authflow_schema_test.go`:

```go
func TestSettingsAuthFallbackParses(t *testing.T) {
	in := []byte(`
settings:
  auth:
    discover_fallback: true
`)
	var cfg Config
	if err := yaml.Unmarshal(in, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !cfg.Settings.Auth.DiscoverFallback {
		t.Fatal("want Settings.Auth.DiscoverFallback = true")
	}
}

func TestSettingsAuthAbsentByDefault(t *testing.T) {
	in := []byte("settings: {}\n")
	var cfg Config
	if err := yaml.Unmarshal(in, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.Settings.Auth.DiscoverFallback {
		t.Fatal("DiscoverFallback must default to false (opt-in)")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/project/ -run TestSettingsAuth -v`
Expected: build failure — `cfg.Settings.Auth` undefined.

- [ ] **Step 3: Add the type and field**

In `internal/project/schema.go`, add `Auth AuthSettings \`yaml:"auth,omitempty"\`` to the `Settings` struct (after the existing `Coverage CoverageSettings` field), and add the type near the other settings types (e.g. after `CoverageSettings`):

```go
// AuthSettings configures session-only auth discovery. All optional; unset
// fields preserve prior behavior (no fallback).
type AuthSettings struct {
	DiscoverFallback bool `yaml:"discover_fallback,omitempty"` // opt-in: discover an AuthFlow in-memory at setup when an actor has no auth block
}
```

The `Settings` struct becomes (showing the tail):

```go
type Settings struct {
	Mode                string            `yaml:"mode,omitempty"`
	MaxDuration         string            `yaml:"max_duration,omitempty"`
	ConfidenceThreshold float64           `yaml:"confidence_threshold,omitempty"`
	AutoFix             string            `yaml:"auto_fix,omitempty"`
	AIBudget            AIBudget          `yaml:"ai_budget,omitempty"`
	CostAlerts          CostAlerts        `yaml:"cost_alerts,omitempty"`
	Models              Models            `yaml:"models,omitempty"`
	ToT                 ToTSettings       `yaml:"tot,omitempty"`
	Reflexion           ReflexionSettings `yaml:"reflexion,omitempty"`
	Coverage            CoverageSettings  `yaml:"coverage,omitempty"`
	Auth                AuthSettings      `yaml:"auth,omitempty"`
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/project/ -run TestSettingsAuth -v`
Expected: both PASS.

- [ ] **Step 5: Lint + race**

Run: `make lint && go test ./internal/project/ -race`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/project/schema.go internal/project/authflow_schema_test.go
git commit -m "feat(project): add Settings.Auth.DiscoverFallback opt-in flag"
```

---

## Task 2: `resolveActorAuth` fallback branch

**Files:**
- Modify: `internal/session/auth_setup.go`
- Test: `internal/session/auth_setup_test.go`

**Interfaces:**
- Consumes: `project.AuthSettings.DiscoverFallback` (Task 1), `authdiscover.Discover` + `authdiscover.ErrNoAuthFlow` (merged in 3a), `s.Driver *ai.Driver` (ready at session construction), `agent.ResolveAuthHeader` (existing).
- Produces: extended `resolveActorAuth` that fills an actor's header from a session-only discovered flow when enabled; `discoverActorAuth` helper.

- [ ] **Step 1: Write the failing tests**

Append to `internal/session/auth_setup_test.go`. Add `"github.com/binoctal/cerberus/internal/ai"` and `"github.com/binoctal/cerberus/internal/llm"` to the test imports (the file already imports `context`, `fmt`, `net/http`, `net/http/httptest`, `testing`, `go.uber.org/zap`, and `project`).

```go
// fallbackDriver builds a mock *ai.Driver whose Decide returns the given JSON.
func fallbackDriver(t *testing.T, resp string) *ai.Driver {
	t.Helper()
	return ai.NewDriver(llm.NewMockClient(map[string]string{"default": resp}), ai.NewTokenBudget(200000, 10000))
}

// writeCodeFile seeds a source file under root so authdiscover.selectSourceFiles finds something.
func writeCodeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveActorAuth_FallbackFillsHeader(t *testing.T) {
	srv := newAuthLoginServer(t, "T") // returns {"token":"T"} at any path
	defer srv.Close()

	root := t.TempDir()
	writeCodeFile(t, root, "svc/login.go", "package svc\n// login jwt token\n")
	s := &Session{
		Driver: fallbackDriver(t, `{"found": true, "login": {"method":"POST","path":"/login"}, "token_from":"token", "inject_as":"Authorization: Bearer {token}"}`),
		Config: &project.Config{
			Settings: project.Settings{Auth: project.AuthSettings{DiscoverFallback: true}},
			Code:     project.CodeConfig{Root: root},
			Services: []project.Service{{Name: "api", URL: srv.URL}},
			Actors:   []project.Actor{{Name: "u", Service: "api"}},
		},
		Logger: zap.NewNop(),
	}
	s.resolveActorAuth(context.Background())

	if s.Config.Actors[0].Auth == nil {
		t.Fatal("want Auth set in-memory by fallback")
	}
	if got := s.Config.Actors[0].Credentials.Headers["Authorization"]; got != "Bearer T" {
		// Exercises the full fallback path: Discover sets Auth in-memory, then
		// ResolveAuthHeader logs in against srv and writes the header.
		t.Fatalf("Authorization header = %q, want \"Bearer T\"", got)
	}
}

func TestResolveActorAuth_FallbackOffSkipsDiscovery(t *testing.T) {
	root := t.TempDir()
	writeCodeFile(t, root, "svc/login.go", "package svc\n// login\n")
	s := &Session{
		Driver: fallbackDriver(t, `{"found": true, "login": {"method":"POST","path":"/login"}, "token_from":"token", "inject_as":"Authorization: Bearer {token}"}`),
		Config: &project.Config{
			Settings: project.Settings{Auth: project.AuthSettings{DiscoverFallback: false}},
			Code:     project.CodeConfig{Root: root},
			Actors:   []project.Actor{{Name: "u"}},
		},
		Logger: zap.NewNop(),
	}
	s.resolveActorAuth(context.Background()) // must not call Discover
	if s.Config.Actors[0].Auth != nil {
		t.Fatal("fallback off must not set Auth")
	}
	if h := s.Config.Actors[0].Credentials.Headers["Authorization"]; h != "" {
		t.Fatalf("fallback off must not write header, got %q", h)
	}
}

func TestResolveActorAuth_FallbackFailureDegrades(t *testing.T) {
	root := t.TempDir()
	writeCodeFile(t, root, "svc/login.go", "package svc\n// login\n")
	s := &Session{
		Driver: fallbackDriver(t, `{"found": false}`), // ErrNoAuthFlow
		Config: &project.Config{
			Settings: project.Settings{Auth: project.AuthSettings{DiscoverFallback: true}},
			Code:     project.CodeConfig{Root: root},
			Actors:   []project.Actor{{Name: "u"}},
		},
		Logger: zap.NewNop(),
	}
	s.resolveActorAuth(context.Background()) // must not panic, must not set Auth
	if s.Config.Actors[0].Auth != nil {
		t.Fatal("failed discovery must not set Auth")
	}
}

func TestResolveActorAuth_DriverNilDegrades(t *testing.T) {
	root := t.TempDir()
	writeCodeFile(t, root, "svc/login.go", "package svc\n// login\n")
	s := &Session{
		Driver: nil, // no driver (e.g. some in-memory test sessions)
		Config: &project.Config{
			Settings: project.Settings{Auth: project.AuthSettings{DiscoverFallback: true}},
			Code:     project.CodeConfig{Root: root},
			Actors:   []project.Actor{{Name: "u"}},
		},
		Logger: zap.NewNop(),
	}
	s.resolveActorAuth(context.Background()) // must not panic on nil driver
	if s.Config.Actors[0].Auth != nil {
		t.Fatal("nil driver must not set Auth")
	}
}
```

Add `"os"` and `"path/filepath"` to the test imports if not already present.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/session/ -run TestResolveActorAuth_Fallback -v`
Expected: failures — the fallback branch does not exist yet; actors with `Auth == nil` stay nil even with the flag on, and the header is never written. (`DriverNilDegrades` may already pass.)

- [ ] **Step 3: Implement the fallback branch**

In `internal/session/auth_setup.go`, add `authdiscover` to the import block:

```go
import (
	"context"
	"errors"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/authdiscover"
	"github.com/binoctal/cerberus/internal/project"
)
```

Replace the `resolveActorAuth` function body so an actor with no `Auth` block goes through the fallback when enabled (the `serviceURLForActor` helper and the existing success/failure logging are unchanged). New body:

```go
// resolveActorAuth runs each actor's declarative auth flow once and writes the
// resulting header into that actor's Credentials.Headers. After this runs, the
// existing static-header injection path (authHeadersFor / withActorHeaders)
// carries the dynamic token with no further changes.
//
// When an actor has no auth: block and settings.auth.discover_fallback is on,
// an AuthFlow is discovered in-memory (never persisted) and used for this
// session only. Failures degrade, never abort.
func (s *Session) resolveActorAuth(ctx context.Context) {
	for i := range s.Config.Actors {
		a := &s.Config.Actors[i]
		if a.Auth == nil {
			if !s.Config.Settings.Auth.DiscoverFallback || s.Driver == nil {
				continue
			}
			if err := s.discoverActorAuth(ctx, a); err != nil {
				if errors.Is(err, authdiscover.ErrNoAuthFlow) {
					s.Logger.Info("no auth flow found for actor; staying unauthenticated",
						zap.String("actor", a.Name),
					)
				} else {
					s.Logger.Warn("auth discovery fallback failed; degrading actor to unauthenticated",
						zap.String("actor", a.Name),
						zap.Error(err),
					)
				}
				continue
			}
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

// discoverActorAuth infers an AuthFlow for an actor with no auth: block and
// sets it on the in-memory config (NEVER persisted to project.yaml). On
// success the caller proceeds through ResolveAuthHeader as if the block had
// been configured. Credential values are never placed in the prompt
// (authdiscover guarantee).
func (s *Session) discoverActorAuth(ctx context.Context, a *project.Actor) error {
	svcURL := s.serviceURLForActor(a)
	af, err := authdiscover.Discover(ctx, s.Driver, s.Config, a.Name, svcURL)
	if err != nil {
		return err
	}
	a.Auth = af
	s.Logger.Info("auth discovered for session only; persist with `cerberus auth discover --actor "+a.Name+"`",
		zap.String("actor", a.Name),
	)
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/session/ -run 'TestResolveActorAuth' -v`
Expected: all PASS (the pre-existing Task-5 resolveActorAuth tests AND the new fallback tests).

- [ ] **Step 5: Lint + race**

Run: `make lint && go test ./internal/session/ -race`
Expected: clean, including pre-existing session tests.

- [ ] **Step 6: Commit**

```bash
git add internal/session/auth_setup.go internal/session/auth_setup_test.go
git commit -m "feat(session): session-only auth discovery fallback when actor has no auth block"
```

---

## Acceptance

Against a target requiring a JWT, with no `auth:` block but `settings: { auth: { discover_fallback: true } }`: a session authenticates using the session-discovered flow, and the log suggests running `cerberus auth discover --actor <name>` to persist. Re-running after persisting behaves identically (persistent block path). With `discover_fallback` off (the default), behavior is unchanged from today (actor stays unauthenticated).

## Out of scope

- Persisting the discovered flow (that is `cerberus auth discover`).
- Re-discovery on login failure of an existing block.
- Mid-session token refresh.
