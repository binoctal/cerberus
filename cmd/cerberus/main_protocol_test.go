package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/protocoldiscover"
)

// mockProtocolDriver wires a deterministic tool-call response into the ai.Driver
// so protocoldiscover.Infer is exercised without any network. input is the
// protocol_draft tool-call payload the "model" returns for any prompt.
func mockProtocolDriver(input map[string]any) *ai.Driver {
	mock := llm.NewMockClient(nil)
	mock.SetToolResponse("default", []llm.ToolCall{
		{Name: "protocol_draft", Input: input},
	})
	return ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000))
}

// writeProtocolProjectYAML seeds a minimal .cerberus/project.yaml. Unlike the
// auth fixture, protocoldiscover does not read source, so no code root is
// required; actors are needed so credential_ref validation in Infer succeeds.
func writeProtocolProjectYAML(t *testing.T, workDir, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(workDir, ".cerberus"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, ".cerberus", "project.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// writeProtocolFrom seeds a --from doc file under workDir and returns its
// relative path (the CLI joins workDir + From).
func writeProtocolFrom(t *testing.T, workDir, name, content string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(workDir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return name
}

// validProtocolInput is a protocol_draft tool payload that yields a protocol
// passing project.ValidateProtocol: query-auth carried by actor "u".
var validProtocolInput = map[string]any{
	"found":     true,
	"framing":   "json",
	"type_path": "type",
	"auth":      map[string]any{"strategy": "query", "param": "token", "credential_ref": "u"},
	"notes":     "ok",
}

const protocolFixture = "actors:\n  - name: u\n    credentials: {email: a@b.c}\n"

func TestRunProtocolInfer_DryRunDoesNotWrite(t *testing.T) {
	workDir := t.TempDir()
	writeProtocolProjectYAML(t, workDir, protocolFixture)
	from := writeProtocolFrom(t, workDir, "doc.md", "# WS spec\nregister event\n")
	opts := protocolInferOpts{
		Name:    "ws",
		From:    from,
		DryRun:  true,
		confirm: func(string) bool { return true },
	}
	if err := runProtocolInfer(context.Background(), workDir, mockProtocolDriver(validProtocolInput), opts); err != nil {
		t.Fatalf("err: %v", err)
	}
	_, err := os.Stat(filepath.Join(workDir, ".cerberus", "protocols", "ws.yaml"))
	if !os.IsNotExist(err) {
		t.Fatalf("dry-run must not write protocol file; stat err=%v", err)
	}
}

func TestRunProtocolInfer_WriteOnConfirm(t *testing.T) {
	workDir := t.TempDir()
	writeProtocolProjectYAML(t, workDir, protocolFixture)
	from := writeProtocolFrom(t, workDir, "doc.md", "# WS spec\n")
	opts := protocolInferOpts{
		Name:    "ws",
		From:    from,
		DryRun:  false,
		confirm: func(string) bool { return true },
	}
	if err := runProtocolInfer(context.Background(), workDir, mockProtocolDriver(validProtocolInput), opts); err != nil {
		t.Fatalf("err: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(workDir, ".cerberus", "protocols", "ws.yaml"))
	if err != nil {
		t.Fatalf("protocol file not written: %v", err)
	}
	body := string(data)
	// Secret hygiene: only the credential_ref name may appear, never a token.
	if strings.Contains(body, "a@b.c") {
		t.Errorf("written protocol leaked a credential value: %s", body)
	}
	if !strings.Contains(body, "framing: json") {
		t.Errorf("written protocol missing framing: %s", body)
	}
	if !strings.Contains(body, "credential_ref: u") {
		t.Errorf("written protocol missing credential_ref name: %s", body)
	}
}

func TestRunProtocolInfer_OverwriteRequiresConfirm(t *testing.T) {
	workDir := t.TempDir()
	writeProtocolProjectYAML(t, workDir, protocolFixture)
	from := writeProtocolFrom(t, workDir, "doc.md", "# WS spec\n")
	protDir := filepath.Join(workDir, ".cerberus", "protocols")
	if err := os.MkdirAll(protDir, 0755); err != nil {
		t.Fatal(err)
	}
	old := []byte("framing: text\nold: true\n")
	if err := os.WriteFile(filepath.Join(protDir, "ws.yaml"), old, 0644); err != nil {
		t.Fatal(err)
	}
	// Decline overwrite -> pre-existing content must be preserved.
	opts := protocolInferOpts{
		Name:    "ws",
		From:    from,
		DryRun:  false,
		confirm: func(string) bool { return false },
	}
	if err := runProtocolInfer(context.Background(), workDir, mockProtocolDriver(validProtocolInput), opts); err != nil {
		t.Fatalf("err: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(protDir, "ws.yaml"))
	if string(after) != string(old) {
		t.Fatalf("overwrite happened despite decline:\n%s", string(after))
	}
}

func TestRunProtocolInfer_PathTraversalRejected(t *testing.T) {
	workDir := t.TempDir()
	writeProtocolProjectYAML(t, workDir, protocolFixture)
	from := writeProtocolFrom(t, workDir, "doc.md", "# WS spec\n")
	opts := protocolInferOpts{
		Name:    "../../x",
		From:    from,
		confirm: func(string) bool { return true },
	}
	if err := runProtocolInfer(context.Background(), workDir, mockProtocolDriver(validProtocolInput), opts); err == nil {
		t.Fatal("want error for path-traversal --name")
	}
	// Nothing written outside the protocols dir.
	if _, err := os.Stat(filepath.Join(workDir, "x.yaml")); err == nil {
		t.Fatal("path traversal escaped the protocols directory")
	}
}

func TestRunProtocolInfer_NoProtocolIsNotError(t *testing.T) {
	workDir := t.TempDir()
	writeProtocolProjectYAML(t, workDir, protocolFixture)
	from := writeProtocolFrom(t, workDir, "doc.md", "# WS spec\n")
	opts := protocolInferOpts{
		Name:    "ws",
		From:    from,
		confirm: func(string) bool { return true },
	}
	// {"found": false} -> protocoldiscover.ErrNoProtocol; command reports and exits 0.
	if err := runProtocolInfer(context.Background(), workDir, mockProtocolDriver(map[string]any{"found": false}), opts); err != nil {
		t.Fatalf("ErrNoProtocol must not be returned as error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, ".cerberus", "protocols", "ws.yaml")); !os.IsNotExist(err) {
		t.Fatalf("no file should be written when no protocol found; stat err=%v", err)
	}
}

// TestRunProtocolInfer_EmptyFromDirErrors guards against an empty --from dir
// silently proceeding to an LLM call with no signal: readInputs must error
// before Infer is reached, so the driver is never contacted.
func TestRunProtocolInfer_EmptyFromDirErrors(t *testing.T) {
	workDir := t.TempDir()
	writeProtocolProjectYAML(t, workDir, protocolFixture)
	emptyDir := filepath.Join(workDir, "empty")
	if err := os.MkdirAll(emptyDir, 0755); err != nil {
		t.Fatal(err)
	}
	opts := protocolInferOpts{
		Name:    "ws",
		From:    "empty",
		DryRun:  true,
		confirm: func(string) bool { return true },
	}
	err := runProtocolInfer(context.Background(), workDir, mockProtocolDriver(validProtocolInput), opts)
	if err == nil {
		t.Fatal("want error for empty --from dir, got nil")
	}
	if !strings.Contains(err.Error(), "no readable text files") {
		t.Errorf("error should mention 'no readable text files', got: %v", err)
	}
}

// TestRunProtocolInfer_SamplesDefaultAndPlumbed verifies that runProtocolInfer
// normalizes a zero Samples to DefaultInferSamples and forwards it to Infer.
// The sequence mock's first element is a false negative and the rest are good
// drafts. With the default N=3 the false negative is absorbed and a protocol is
// produced (and written, since DryRun is false + confirm true); with N=1 it
// would report "no protocol found" and write nothing. Asserting the file exists
// therefore pins the normalization + plumbing, not just the absence of error.
func TestRunProtocolInfer_SamplesDefaultAndPlumbed(t *testing.T) {
	workDir := t.TempDir()
	writeProtocolProjectYAML(t, workDir, protocolFixture)
	from := writeProtocolFrom(t, workDir, "doc.md", "# WS spec\n")

	good := map[string]any{
		"found":     true,
		"framing":   "json",
		"type_path": "type",
		"auth":      map[string]any{"strategy": "query", "param": "token", "credential_ref": "u"},
	}
	seq := [][]llm.ToolCall{
		{{Name: "protocol_draft", Input: map[string]any{"found": false}}}, // false negative
		{{Name: "protocol_draft", Input: good}},
		{{Name: "protocol_draft", Input: good}},
	}
	mock := llm.NewMockClient(nil)
	mock.SetToolResponseSequence("default", seq)
	driver := ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000))

	// Samples left zero -> runProtocolInfer must normalize to DefaultInferSamples.
	opts := protocolInferOpts{
		Name:    "ws",
		From:    from,
		DryRun:  false,
		confirm: func(string) bool { return true },
	}
	if err := runProtocolInfer(context.Background(), workDir, driver, opts); err != nil {
		t.Fatalf("err: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, ".cerberus", "protocols", "ws.yaml")); err != nil {
		t.Fatalf("default N=3 must absorb the false negative and write a protocol; stat err=%v", err)
	}
}

// Ensure the package compiles against protocoldiscover's public surface.
var _ = protocoldiscover.ErrNoProtocol

// TestProtocolCmd_Tree verifies the cobra wiring: protocolCmd registers an
// infer subcommand with the expected flags. Anchors the constructors so they
// are not flagged unused before main.go registers protocolCmd.
func TestProtocolCmd_Tree(t *testing.T) {
	root := protocolCmd()
	if root.Use != "protocol" {
		t.Fatalf("root Use = %q, want %q", root.Use, "protocol")
	}
	var infer *cobra.Command
	for _, c := range root.Commands() {
		if c.Use == "infer" {
			infer = c
			break
		}
	}
	if infer == nil {
		t.Fatal("protocolCmd must register an 'infer' subcommand")
	}
	for _, name := range []string{"name", "from", "service", "dry-run", "samples"} {
		if infer.Flags().Lookup(name) == nil {
			t.Errorf("infer subcommand missing required flag %q", name)
		}
	}
	if err := infer.MarkFlagRequired("name"); err != nil {
		t.Errorf("name must be marked required: %v", err)
	}
}
