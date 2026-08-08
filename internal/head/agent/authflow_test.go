package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/binoctal/cerberus/internal/project"
)

func newLoginServer(t *testing.T, status int, respBody string, capture func(method, path, body string, headers http.Header)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if capture != nil {
			capture(r.Method, r.URL.Path, string(body), r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, respBody)
	}))
}

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

func TestResolveAuthHeader_TopLevelToken(t *testing.T) {
	srv := newLoginServer(t, 200, `{"token":"JWT-123"}`, nil)
	defer srv.Close()

	actor := project.Actor{Auth: &project.AuthFlow{
		Login:     project.AuthLogin{Method: "POST", Path: "/api/dev/setup"},
		TokenFrom: "token",
		InjectAs:  "Authorization: Bearer {token}",
	}}
	res, err := ResolveAuthHeader(context.Background(), srv.URL, actor)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.HeaderName != "Authorization" || res.HeaderValue != "Bearer JWT-123" {
		t.Fatalf("got %q=%q", res.HeaderName, res.HeaderValue)
	}
	if res.PathParams != nil {
		t.Fatalf("PathParams = %v, want nil when none declared", res.PathParams)
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
	res, err := ResolveAuthHeader(context.Background(), srv.URL, actor)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.HeaderName != "X-Token" || res.HeaderValue != "xyz" {
		t.Fatalf("got %q=%q", res.HeaderName, res.HeaderValue)
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
	_, err := ResolveAuthHeader(context.Background(), srv.URL, actor)
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
		Login:     project.AuthLogin{Method: "POST", Path: "/login"},
		TokenFrom: "token", InjectAs: "Authorization: Bearer {token}",
	}}
	_, err := ResolveAuthHeader(context.Background(), srv.URL, actor)
	if err == nil {
		t.Fatal("want error on non-2xx, got nil")
	}
}

func TestResolveAuthHeader_MissingTokenFieldDegrades(t *testing.T) {
	srv := newLoginServer(t, 200, `{"unrelated":"x"}`, nil)
	defer srv.Close()

	actor := project.Actor{Auth: &project.AuthFlow{
		Login:     project.AuthLogin{Method: "POST", Path: "/login"},
		TokenFrom: "token", InjectAs: "Authorization: Bearer {token}",
	}}
	_, err := ResolveAuthHeader(context.Background(), srv.URL, actor)
	if err == nil {
		t.Fatal("want error when token field missing, got nil")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Fatalf("error should name the missing key, got %v", err)
	}
}

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
	_, err := ResolveAuthHeader(context.Background(), srv.URL, actor)
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), "REAL-SECRET-VALUE") {
		t.Fatalf("error leaks token: %v", err)
	}
}

func TestResolveAuthHeaderReturnsRawToken(t *testing.T) {
	srv := newLoginServer(t, 200, `{"token":"JWT-RAW"}`, nil)
	defer srv.Close()
	actor := project.Actor{Auth: &project.AuthFlow{
		Login:     project.AuthLogin{Method: "POST", Path: "/login"},
		TokenFrom: "token", InjectAs: "Authorization: Bearer {token}",
	}}
	res, err := ResolveAuthHeader(context.Background(), srv.URL, actor)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.RawToken != "JWT-RAW" {
		t.Fatalf("raw token = %q, want JWT-RAW", res.RawToken)
	}
}

// F3: declared path_params are captured from the same login response.

func TestResolveAuthHeader_CapturesPathParams(t *testing.T) {
	srv := newLoginServer(t, 200, `{"token":"T","config":{"userId":"user_1","tenant":"acme"}}`, nil)
	defer srv.Close()
	actor := project.Actor{Auth: &project.AuthFlow{
		Login:      project.AuthLogin{Method: "POST", Path: "/login"},
		TokenFrom:  "token",
		InjectAs:   "Authorization: Bearer {token}",
		PathParams: map[string]string{"userId": "config.userId", "tenant": "config.tenant"},
	}}
	res, err := ResolveAuthHeader(context.Background(), srv.URL, actor)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.PathParams["userId"] != "user_1" {
		t.Fatalf("userId = %q, want user_1", res.PathParams["userId"])
	}
	if res.PathParams["tenant"] != "acme" {
		t.Fatalf("tenant = %q, want acme", res.PathParams["tenant"])
	}
}

func TestResolveAuthHeader_PathParamAbsentDotPathIsEmpty(t *testing.T) {
	// Declared path param whose dot-path is absent in the response yields ""
	// (non-fatal); auth still succeeds.
	srv := newLoginServer(t, 200, `{"token":"T","config":{"userId":"user_1"}}`, nil)
	defer srv.Close()
	actor := project.Actor{Auth: &project.AuthFlow{
		Login:      project.AuthLogin{Method: "POST", Path: "/login"},
		TokenFrom:  "token",
		InjectAs:   "Authorization: Bearer {token}",
		PathParams: map[string]string{"userId": "config.userId", "missing": "config.nope"},
	}}
	res, err := ResolveAuthHeader(context.Background(), srv.URL, actor)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.PathParams["userId"] != "user_1" {
		t.Fatalf("userId = %q, want user_1", res.PathParams["userId"])
	}
	if res.PathParams["missing"] != "" {
		t.Fatalf("absent dot-path should yield \"\", got %q", res.PathParams["missing"])
	}
}

func TestResolveAuthHeader_NoPathParamsDeclaresNil(t *testing.T) {
	// Backwards-compat: no path_params declared ⇒ nil map (old behavior).
	srv := newLoginServer(t, 200, `{"token":"T"}`, nil)
	defer srv.Close()
	actor := project.Actor{Auth: &project.AuthFlow{
		Login:     project.AuthLogin{Method: "POST", Path: "/login"},
		TokenFrom: "token",
		InjectAs:  "Authorization: Bearer {token}",
	}}
	res, err := ResolveAuthHeader(context.Background(), srv.URL, actor)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.PathParams != nil {
		t.Fatalf("PathParams = %v, want nil when none declared", res.PathParams)
	}
}

func TestResolveAuthHeader_LoginURLDropsServiceURLPath(t *testing.T) {
	// When the service URL carries a path component (e.g. the ws-realtime
	// template "http://<host>/ws/{userId}") and login.path is host-relative
	// ("/api/dev/setup"), the login request MUST be sent to the host root,
	// not under the service URL's path. The stub records the request path;
	// it must be "/api/dev/setup", not "/ws/{userId}/api/dev/setup".
	var gotPath string
	srv := newLoginServer(t, 200, `{"token":"T"}`, func(_, path string, _ string, _ http.Header) {
		gotPath = path
	})
	defer srv.Close()

	// srv.URL is "http://127.0.0.1:<port>"; append the templated path that
	// should NOT bleed into the login URL.
	svcURL := srv.URL + "/ws/{userId}"

	actor := project.Actor{Auth: &project.AuthFlow{
		Login:     project.AuthLogin{Method: "POST", Path: "/api/dev/setup"},
		TokenFrom: "token",
		InjectAs:  "Authorization: Bearer {token}",
	}}
	_, err := ResolveAuthHeader(context.Background(), svcURL, actor)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if gotPath != "/api/dev/setup" {
		t.Fatalf("login request path = %q, want %q (service URL path must not carry into login URL)", gotPath, "/api/dev/setup")
	}
}

func TestResolveAuthHeader_ProvisioningOnly_StaticTokenPlusPathParams(t *testing.T) {
	// TokenFrom empty ⇒ use static Credentials.Token, but still run login to
	// capture PathParams. Provision a fake /api/dev/setup response carrying userId.
	srv := newLoginServer(t, 200, `{"config":{"userId":"user_42","deviceToken":"tok_dev"}}`, nil)
	defer srv.Close()

	actor := project.Actor{
		Credentials: project.CredentialRef{Token: "demo_token"},
		Auth: &project.AuthFlow{
			Login:     project.AuthLogin{Method: "POST", Path: "/api/dev/setup"},
			TokenFrom: "", // provisioning-only: static token + captured path params
			InjectAs:  "Authorization: Bearer {token}",
			PathParams: map[string]string{
				"userId": "config.userId",
			},
		},
	}
	res, err := ResolveAuthHeader(context.Background(), srv.URL, actor)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.RawToken != "demo_token" {
		t.Fatalf("empty TokenFrom ⇒ static Credentials.Token is used; raw token = %q, want demo_token", res.RawToken)
	}
	if res.PathParams == nil || res.PathParams["userId"] != "user_42" {
		t.Fatalf("login still runs to capture path params; PathParams = %v, want userId=user_42", res.PathParams)
	}
}
