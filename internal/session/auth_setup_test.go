package session

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

// uuidRE matches a canonical hyphenated v4 uuid (8-4-4-4-12 hex).
var uuidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

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

// F3: declared path_params are captured into Credentials.PathParams so the
// WS connect layer can template {name} placeholders in the service URL.
func TestResolveActorAuth_StoresPathParams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"token":"T","config":{"userId":"user_1"}}`)
	}))
	defer srv.Close()

	s := &Session{
		Config: &project.Config{
			Services: []project.Service{{Name: "api", URL: srv.URL}},
			Actors: []project.Actor{{
				Name:    "u",
				Service: "api",
				Auth: &project.AuthFlow{
					Login:      project.AuthLogin{Method: "POST", Path: "/login"},
					TokenFrom:  "token",
					InjectAs:   "Authorization: Bearer {token}",
					PathParams: map[string]string{"userId": "config.userId"},
				},
			}},
		},
		Logger: zap.NewNop(),
	}
	s.resolveActorAuth(context.Background())

	pp := s.Config.Actors[0].Credentials.PathParams
	if pp["userId"] != "user_1" {
		t.Fatalf("Credentials.PathParams[userId] = %q, want user_1", pp["userId"])
	}
}

// Backwards-compat: an auth flow without path_params leaves Credentials.PathParams nil.
func TestResolveActorAuth_NoPathParamsLeavesNil(t *testing.T) {
	srv := newAuthLoginServer(t, "T")
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

	if s.Config.Actors[0].Credentials.PathParams != nil {
		t.Fatalf("Credentials.PathParams = %v, want nil when none declared", s.Config.Actors[0].Credentials.PathParams)
	}
}

// Generated path params: a NO-AUTH actor declaring generated_path_params still
// gets a synthesized value merged into Credentials.PathParams — generation is
// independent of the auth flow (a client may connect to /ws/{clientId} with no
// login at all). Proves the new pass runs for actors the auth loop skips.
func TestResolveActorAuth_GeneratedPathParamsForNoAuthActor(t *testing.T) {
	s := &Session{
		Config: &project.Config{
			Actors: []project.Actor{{
				Name:                "web",
				GeneratedPathParams: map[string]string{"clientId": "uuid"},
			}},
		},
		Logger: zap.NewNop(),
	}
	s.resolveActorAuth(context.Background())

	pp := s.Config.Actors[0].Credentials.PathParams
	if v, ok := pp["clientId"]; !ok || !uuidRE.MatchString(v) {
		t.Fatalf("Credentials.PathParams[clientId] = %q (%v), want a uuid", v, ok)
	}
}

// An actor with BOTH captured (auth.path_params) and generated path params keeps
// BOTH after resolution — generated values merge into PathParams rather than
// clobbering the auth-captured ones.
func TestResolveActorAuth_CapturedAndGeneratedCoexist(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"token":"T","config":{"userId":"user_1"}}`)
	}))
	defer srv.Close()

	s := &Session{
		Config: &project.Config{
			Services: []project.Service{{Name: "api", URL: srv.URL}},
			Actors: []project.Actor{{
				Name:    "u",
				Service: "api",
				Auth: &project.AuthFlow{
					Login:      project.AuthLogin{Method: "POST", Path: "/login"},
					TokenFrom:  "token",
					InjectAs:   "Authorization: Bearer {token}",
					PathParams: map[string]string{"userId": "config.userId"},
				},
				GeneratedPathParams: map[string]string{"clientId": "uuid"},
			}},
		},
		Logger: zap.NewNop(),
	}
	s.resolveActorAuth(context.Background())

	pp := s.Config.Actors[0].Credentials.PathParams
	if pp["userId"] != "user_1" {
		t.Fatalf("captured userId lost after generated-params pass: %q", pp["userId"])
	}
	if v := pp["clientId"]; !uuidRE.MatchString(v) {
		t.Fatalf("generated clientId missing/invalid after merge: %q", v)
	}
}

// TestRefreshActorAuthRotatesTokens pins the background refresh contract:
// the SUT's access tokens expire in 15 minutes while a sweep runs for hours
// (run33: 119 "Invalid token" 401 verdicts), so re-running an actor's auth
// flow mid-run must rotate both the actor's credentials and the live
// protocol index every http_request consults.
func TestRefreshActorAuthRotatesTokens(t *testing.T) {
	var mu sync.Mutex
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		n++
		tok := fmt.Sprintf("JWT-%d", n)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"token":%q}`, tok)
	}))
	defer srv.Close()

	s := &Session{
		Config: &project.Config{
			Services: []project.Service{{Name: "api", URL: srv.URL,
				Protocol: &project.Protocol{Roles: map[string]*project.ProtocolRole{
					"web": {CredentialRef: "u"},
				}}}},
			Actors: []project.Actor{{
				Name:    "u",
				Service: "api",
				Auth: &project.AuthFlow{
					Login:         project.AuthLogin{Method: "POST", Path: "/login"},
					TokenFrom:     "token",
					HTTPLogin:     &project.AuthLogin{Method: "POST", Path: "/login"},
					HTTPTokenFrom: "token",
					InjectAs:      "Authorization: Bearer {token}",
				},
			}},
		},
		Logger: zap.NewNop(),
	}
	ctx := context.Background()
	s.resolveActorAuth(ctx)
	idx := agent.BuildWSProtocolIndex(s.Config)
	// The resolve pass runs TWO logins (primary + http_login), so the initial
	// http token is the second response.
	if got := idx.ActorHTTPToken("u"); got != "JWT-2" {
		t.Fatalf("initial http token = %q, want JWT-2", got)
	}

	// What the background refresher does each interval: two more logins, a
	// fresh token in both the actor and the live index.
	s.refreshActorAuth(ctx, idx)
	if got := idx.ActorHTTPToken("u"); got != "JWT-4" {
		t.Fatalf("rotated http token = %q, want JWT-4", got)
	}
	if got := s.Config.Actors[0].Credentials.RawHTTPToken; got != "JWT-4" {
		t.Fatalf("actor RawHTTPToken = %q, want JWT-4", got)
	}
}
