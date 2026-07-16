package session

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"

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
