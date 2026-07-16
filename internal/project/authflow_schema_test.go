package project

import (
	"bytes"
	"strings"
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

func TestValidateAuthFlowExported(t *testing.T) {
	cases := []struct {
		name    string
		auth    *AuthFlow
		wantErr bool
	}{
		{name: "valid", auth: &AuthFlow{
			Login:     AuthLogin{Method: "POST", Path: "/login"},
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
