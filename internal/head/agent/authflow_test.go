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
		Login:     project.AuthLogin{Method: "POST", Path: "/login"},
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
		Login:     project.AuthLogin{Method: "POST", Path: "/login"},
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
