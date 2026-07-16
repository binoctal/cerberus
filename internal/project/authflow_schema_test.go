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
