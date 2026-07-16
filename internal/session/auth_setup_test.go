package session

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

func newAuthLoginServer(t *testing.T, token string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"token":%q}`, token)
	}))
}

func TestResolveActorAuthWritesHeader(t *testing.T) {
	srv := newAuthLoginServer(t, "JWT-XYZ")
	defer srv.Close()

	s := &Session{
		Config: &project.Config{
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
		Logger: zap.NewNop(),
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
		Config: &project.Config{
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
		Logger: zap.NewNop(),
	}
	// Must not panic or return error.
	s.resolveActorAuth(context.Background())

	if h := s.Config.Actors[0].Credentials.Headers["Authorization"]; h != "" {
		t.Fatalf("no header should be written on failure, got %q", h)
	}
}

func TestResolveActorAuthSkipsActorsWithoutAuth(t *testing.T) {
	s := &Session{
		Config: &project.Config{
			Actors: []project.Actor{{Name: "plain", Credentials: project.CredentialRef{Email: "a@b.c"}}},
		},
		Logger: zap.NewNop(),
	}
	s.resolveActorAuth(context.Background()) // must be a no-op
}

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
		Driver: nil,
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
