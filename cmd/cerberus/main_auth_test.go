package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

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
	// authdiscover.Discover requires cfg.Code.Root to be a readable directory
	// containing at least one source file (selectSourceFiles errors otherwise).
	// These tests exercise write-back, not source selection, so seed a minimal
	// code root and append it to the YAML — matches the convention in
	// authdiscover's own discover_test.go.
	codeRoot := filepath.Join(workDir, "code")
	if err := os.MkdirAll(codeRoot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codeRoot, "svc.go"), []byte("package svc\n// login token\n"), 0644); err != nil {
		t.Fatal(err)
	}
	full := content + fmt.Sprintf("code:\n  root: %s\n", codeRoot)
	if err := os.WriteFile(filepath.Join(workDir, ".cerberus", "project.yaml"), []byte(full), 0644); err != nil {
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

// TestAuthCmd_Tree verifies the cobra command tree is wired: authCmd registers
// a discover subcommand with the expected flags. This also anchors the command
// constructors (authCmd/authDiscoverCmd/newAuthDiscoverDriver/promptConfirm)
// so they are not flagged as unused before Task 5 registers authCmd in main.go.
func TestAuthCmd_Tree(t *testing.T) {
	root := authCmd()
	if root.Use != "auth" {
		t.Fatalf("root Use = %q, want %q", root.Use, "auth")
	}
	var discover *cobra.Command
	for _, c := range root.Commands() {
		if c.Use == "discover" {
			discover = c
			break
		}
	}
	if discover == nil {
		t.Fatal("authCmd must register a 'discover' subcommand")
	}
	for _, name := range []string{"actor", "service", "dry-run"} {
		if discover.Flags().Lookup(name) == nil {
			t.Errorf("discover subcommand missing required flag %q", name)
		}
	}
	// promptConfirm must return a function that answers y/yes.
	yes := promptConfirm(strings.NewReader("y\n"), io.Discard)
	if !yes("ok?") {
		t.Error("promptConfirm must accept 'y'")
	}
	no := promptConfirm(strings.NewReader("n\n"), io.Discard)
	if no("ok?") {
		t.Error("promptConfirm must reject 'n'")
	}
}
