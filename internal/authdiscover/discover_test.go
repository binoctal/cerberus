package authdiscover

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
		Code:   project.CodeConfig{Root: root},
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
		Code:   project.CodeConfig{Root: root},
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
		Code:   project.CodeConfig{Root: root},
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
		Code:   project.CodeConfig{Root: root},
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

func writeRootFile(root, rel, content string) error {
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(content), 0644)
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
