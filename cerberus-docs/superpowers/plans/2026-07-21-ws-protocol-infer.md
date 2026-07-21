# WebSocket Realtime Engine (M3-3) — `cerberus protocol infer` Implementation Plan

> **STATUS: PROVISIONAL — DO NOT EXECUTE until dogfooding greenlights (per the M3 proposal trigger conditions and the design spec's Status note).** Architecture mirrors the shipped `auth discover` command exactly, so Tasks 1–2 are concrete and fully testable with a mock driver. The LLM prompt content (the `buildInferPrompt` text) is provisional and must be tuned against real protocol docs before trusting.

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** `cerberus protocol infer --name <n> --from <path> [--service <svc>] [--dry-run]` drafts a `*project.Protocol` from docs/examples via the LLM, validates it, prints it, and on confirmation writes `.cerberus/protocols/<n>.yaml` (the M3-1 artifact) for human review.

**Architecture:** Near-exact mirror of `auth discover`. New `internal/protocoldiscover/Infer(ctx, driver, cfg, service, inputs) (*project.Protocol, error)` mirrors `authdiscover.Discover`. New `cmd/cerberus/main_protocol.go` (`protocolCmd`/`protocolInferCmd`/`runProtocolInfer`/`newProtocolInferDriver`) mirrors `main_auth.go`. Registered in `main.go`'s `AddCommand`. No change to `internal/project/`, the executor, or Scout.

**Tech Stack:** Go 1.25 · cobra · `internal/ai` (Driver) · `internal/project` (Protocol/ValidateProtocol) · testify.

## Global Constraints

- Go 1.25, pure Go. No new deps.
- Commit author `binoctal <binoctal@gmail.com>`, no Co-Authored-By. English comments/messages.
- Docs only in `cerberus-docs/`. `make check` (fmt+lint+test -race) green.
- `auth discover` must keep working (additive only; do not mutate `main_auth.go`).
- `--name` validated as a plain name via the M3-1 rule (no `/`, `\`, `..`).

**Spec:** `cerberus-docs/superpowers/specs/2026-07-21-ws-protocol-infer-design.md`

---

## Task 1: `protocoldiscover.Infer` core + tests

**Files:**
- Create: `internal/protocoldiscover/infer.go`
- Create: `internal/protocoldiscover/infer_test.go`

**Interfaces:**
- Produces: `func Infer(ctx context.Context, driver *ai.Driver, cfg *project.Config, serviceName string, inputs []SourceFile) (*project.Protocol, error)`; `var ErrNoProtocol = errors.New("no websocket protocol found")`; `type SourceFile` (mirror `authdiscover.SourceFile`: `{Path, Content string}`).

- [ ] **Step 1: Write the failing tests** (`internal/protocoldiscover/infer_test.go`):

```go
package protocoldiscover

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInfer_ReturnsValidatedProtocol(t *testing.T) {
	cfg := &project.Config{
		Services: []project.Service{{Name: "rt", URL: "http://x"}},
		Actors:   []project.Actor{{Name: "web"}},
	}
	driver := mockDriver(t, map[string]any{
		"found":     true,
		"framing":   "json",
		"type_path": "type",
		"auth":      map[string]any{"strategy": "query", "param": "token", "credential_ref": "web"},
		"notes":     "ok",
	})
	p, err := Infer(context.Background(), driver, cfg, "rt", []SourceFile{{Path: "docs.md", Content: "..."}})
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "json", p.Framing)
	assert.Equal(t, "type", p.TypePath)
	require.NotNil(t, p.Auth)
	assert.Equal(t, "web", p.Auth.CredentialRef)
}

func TestInfer_NoProtocol(t *testing.T) {
	driver := mockDriver(t, map[string]any{"found": false})
	_, err := Infer(context.Background(), driver, cfgWithService(), "rt", nil)
	assert.ErrorIs(t, err, ErrNoProtocol)
}

func TestInfer_InvalidCredentialRefFailsValidation(t *testing.T) {
	driver := mockDriver(t, map[string]any{
		"found": true, "framing": "json",
		"auth": map[string]any{"strategy": "query", "param": "token", "credential_ref": "ghost"},
	})
	_, err := Infer(context.Background(), driver, cfgWithService(), "rt", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid")
}

func cfgWithService() *project.Config {
	return &project.Config{Services: []project.Service{{Name: "rt", URL: "http://x"}}, Actors: []project.Actor{{Name: "web"}}}
}

// mockDriver builds an *ai.Driver whose Decide writes a JSON-encoded response
// for the given value into the schema. (Mirror the authdiscover test helper
// pattern; the exact constructor is whatever internal/ai exposes for tests —
// if none, build a mock llm.Client returning the JSON and wrap with ai.NewDriver.)
func mockDriver(t *testing.T, out any) *ai.Driver {
	t.Helper()
	raw, err := json.Marshal(out)
	require.NoError(t, err)
	// TODO(provisional): wire a mock llm.Client whose Complete returns {Content: string(raw)}
	// into ai.NewDriver. If internal/ai has no test helper, add a tiny mock in this
	// _test.go implementing llm.Client (Complete returns the canned content).
	_ = raw
	return newMockDriver(t, string(raw))
}
```

- [ ] **Step 2: Run to verify fail** — `go test ./internal/protocoldiscover/` → FAIL (package undefined).

- [ ] **Step 3: Implement** (`internal/protocoldiscover/infer.go`):

```go
package protocoldiscover

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/project"
)

// ErrNoProtocol signals the model found no WS protocol in the inputs.
var ErrNoProtocol = errors.New("no websocket protocol found")

// SourceFile is a doc/example file fed to the model.
type SourceFile struct {
	Path    string
	Content string
}

// inferOutput is the JSON shape the LLM must return (mirrors authdiscover's
// discoverOutput). Field names match the project.Protocol yaml tags for clarity.
type inferOutput struct {
	Found   bool                     `json:"found"`
	Framing string                   `json:"framing"`
	TypePath string                  `json:"type_path"`
	Auth    *inferAuth               `json:"auth,omitempty"`
	Roles   map[string]*inferRole    `json:"roles,omitempty"`
	Notes   string                   `json:"notes"`
}

type inferAuth struct {
	Strategy      string `json:"strategy"`
	Param         string `json:"param"`
	CredentialRef string `json:"credential_ref"`
}

type inferRole struct {
	CredentialRef string            `json:"credential_ref,omitempty"`
	Params        map[string]string `json:"params,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	Subprotocols  []string          `json:"subprotocols,omitempty"`
	Handshake     *inferHandshake   `json:"handshake,omitempty"`
}

type inferHandshake struct {
	AwaitType string `json:"await_type"`
	Timeout   int    `json:"timeout"`
}

// Infer asks the LLM to draft a protocol description from the given inputs,
// validates it, and returns it (not written to disk). The driver is passed in
// so tests inject a mock. serviceName names the service the draft targets
// (used only to resolve the actor list for credential_ref validation).
func Infer(ctx context.Context, driver *ai.Driver, cfg *project.Config, serviceName string, inputs []SourceFile) (*project.Protocol, error) {
	if driver == nil {
		return nil, errors.New("nil driver")
	}
	prompt := buildInferPrompt(serviceName, inputs)
	var out inferOutput
	if err := driver.Decide(ctx, prompt, &out); err != nil {
		return nil, errors.New("could not parse LLM output into Protocol")
	}
	if !out.Found {
		return nil, ErrNoProtocol
	}
	p := &project.Protocol{
		Framing:  out.Framing,
		TypePath: out.TypePath,
	}
	if out.Auth != nil {
		p.Auth = &project.ProtocolAuth{
			Strategy:      out.Auth.Strategy,
			Param:         out.Auth.Param,
			CredentialRef: out.Auth.CredentialRef,
		}
	}
	for name, r := range out.Roles {
		role := &project.ProtocolRole{
			CredentialRef: r.CredentialRef,
			Params:        r.Params,
			Headers:       r.Headers,
			Subprotocols:  r.Subprotocols,
		}
		if r.Handshake != nil {
			role.Handshake = &project.RoleHandshake{AwaitType: r.Handshake.AwaitType, Timeout: r.Handshake.Timeout}
		}
		p.Roles[name] = role
	}
	if err := project.ValidateProtocol(p, actorsOf(cfg)); err != nil {
		return nil, fmt.Errorf("model produced an invalid protocol: %w", err)
	}
	return p, nil
}

func actorsOf(cfg *project.Config) []project.Actor {
	if cfg == nil {
		return nil
	}
	return cfg.Actors
}

// buildInferPrompt assembles the prompt (PROVISIONAL content — tune via
// dogfooding). It MUST inline the JSON shape because ai.Driver.Decide does not
// inject the schema into the prompt. NEVER include credential values.
func buildInferPrompt(serviceName string, inputs []SourceFile) string {
	var b strings.Builder
	b.WriteString("You are drafting a WebSocket protocol description for a cerberus test config.\n")
	b.WriteString("From the docs/message examples below, infer: the wire framing, the routing-key path, how auth is attached, and any connection roles.\n")
	fmt.Fprintf(&b, "The target service is %q.\n\n", serviceName)
	b.WriteString("Respond with ONLY a JSON object of this shape:\n")
	b.WriteString("{\n")
	b.WriteString("  \"found\": <true if a WS protocol is described, false otherwise>,\n")
	b.WriteString("  \"framing\": \"json\",                      // json | text | binary\n")
	b.WriteString("  \"type_path\": \"type\",                    // dotted path to the routing key\n")
	b.WriteString("  \"auth\": {\"strategy\":\"query\",\"param\":\"token\",\"credential_ref\":\"<actor>\"},\n")
	b.WriteString("  \"roles\": {\"web\": {\"credential_ref\":\"<actor>\",\"params\":{\"type\":\"web\"},\"handshake\":{\"await_type\":\"ready\",\"timeout\":5}}},\n")
	b.WriteString("  \"notes\": \"one-line rationale\"\n")
	b.WriteString("}\n\n")
	b.WriteString("credential_ref MUST name an actor that exists in project.yaml — NEVER invent one, and NEVER include real credential values or tokens.\n\n")
	for _, f := range inputs {
		b.WriteString("--- " + filepath.Base(f.Path) + " ---\n")
		b.WriteString(truncateContent(f.Content, 8000))
		b.WriteString("\n\n")
	}
	return b.String()
}

func truncateContent(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n…[truncated]"
}
```

- [ ] **Step 4: Run to verify pass** — `go test ./internal/protocoldiscover/` → PASS (resolve the mock-driver TODO with a tiny `llm.Client` mock in the test file).

- [ ] **Step 5: Commit** — `git commit -m "feat(protocoldiscover): infer a protocol description from docs/examples"`.

---

## Task 2: `cerberus protocol infer` CLI + register + tests

**Files:**
- Create: `cmd/cerberus/main_protocol.go`
- Modify: `cmd/cerberus/main.go:15` (register `protocolCmd()`)
- Test: `cmd/cerberus/main_protocol_test.go`

**Interfaces:** Consumes `protocoldiscover.Infer` (Task 1), `project.LoadFromFile`, `promptConfirm` (already in `main_auth.go`, same package).

- [ ] **Step 1: Write the failing tests** — mirror `runAuthDiscover`'s test: `runProtocolInfer` with a mock driver + temp dir → `--dry-run` prints without writing; confirm=yes writes `.cerberus/protocols/<name>.yaml`; existing-file → overwrite confirm; `--name "../../x"` → rejected.

- [ ] **Step 2: Run to verify fail.**

- [ ] **Step 3: Implement** (`cmd/cerberus/main_protocol.go`), mirroring `main_auth.go`:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/protocoldiscover"
)

var (
	protocolInferName    string
	protocolInferFrom    string
	protocolInferService string
	protocolInferDryRun  bool
)

func protocolCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "protocol", Short: "Authoring aids for WS protocol declarations"}
	cmd.AddCommand(protocolInferCmd())
	return cmd
}

func protocolInferCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "infer",
		Short: "Draft a WS protocol description from docs/examples for human review",
		RunE: func(cmd *cobra.Command, args []string) error {
			driver, err := newProtocolInferDriver()
			if err != nil {
				return err
			}
			return runProtocolInfer(cmd.Context(), ".", driver, protocolInferOpts{
				Name:    protocolInferName,
				From:    protocolInferFrom,
				Service: protocolInferService,
				DryRun:  protocolInferDryRun,
				confirm: promptConfirm(os.Stdin, os.Stdout),
			})
		},
	}
	cmd.Flags().StringVar(&protocolInferName, "name", "", "protocol file name (.cerberus/protocols/<name>.yaml); plain name, required")
	cmd.Flags().StringVar(&protocolInferFrom, "from", "", "path to a doc/example file or dir to infer from (required)")
	cmd.Flags().StringVar(&protocolInferService, "service", "", "service the protocol targets (default: first service)")
	cmd.Flags().BoolVar(&protocolInferDryRun, "dry-run", false, "print the draft without writing")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("from")
	return cmd
}

type protocolInferOpts struct {
	Name    string
	From    string
	Service string
	DryRun  bool
	confirm func(string) bool
}

func runProtocolInfer(ctx context.Context, workDir string, driver *ai.Driver, opts protocolInferOpts) error {
	if err := checkProtocolRefName(opts.Name); err != nil {
		return fmt.Errorf("--name: %w", err)
	}
	cfgPath := filepath.Join(workDir, ".cerberus", "project.yaml")
	cfg, err := project.LoadFromFile(cfgPath)
	if err != nil {
		return fmt.Errorf("load project.yaml: %w", err)
	}
	service := opts.Service
	if service == "" && len(cfg.Services) > 0 {
		service = cfg.Services[0].Name
	}
	inputs, err := readInputs(filepath.Join(workDir, opts.From))
	if err != nil {
		return fmt.Errorf("read --from: %w", err)
	}
	p, err := protocoldiscover.Infer(ctx, driver, cfg, service, inputs)
	if errors.Is(err, protocoldiscover.ErrNoProtocol) {
		fmt.Println("no WebSocket protocol found in the provided inputs")
		return nil
	}
	if err != nil {
		return err
	}
	block, _ := yaml.Marshal(p)
	fmt.Printf("Draft protocol %q:\n%s\n", opts.Name, string(block))
	if opts.DryRun {
		return nil
	}
	outPath := filepath.Join(workDir, ".cerberus", "protocols", opts.Name+".yaml")
	question := fmt.Sprintf("Write draft to %s? [y/N]", filepath.Join(".cerberus", "protocols", opts.Name+".yaml"))
	if _, statErr := os.Stat(outPath); statErr == nil {
		question = fmt.Sprintf("%s already exists. Overwrite? [y/N]", filepath.Join(".cerberus", "protocols", opts.Name+".yaml"))
	}
	if opts.confirm == nil || !opts.confirm(question) {
		fmt.Println("aborted; no changes written")
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(outPath, block, 0644)
}

// readInputs reads a file or enumerates a dir into SourceFiles (text only).
func readInputs(path string) ([]protocoldiscover.SourceFile, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return []protocoldiscover.SourceFile{{Path: path, Content: string(data)}}, nil
	}
	var out []protocoldiscover.SourceFile
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !isTextFile(e.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(path, e.Name()))
		if err != nil {
			continue
		}
		out = append(out, protocoldiscover.SourceFile{Path: e.Name(), Content: string(data)})
	}
	return out, nil
}

func isTextFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".md", ".markdown", ".txt", ".json", ".yaml", ".yml", ".ts", ".js", ".go", ".py":
		return true
	}
	return false
}

// newProtocolInferDriver mirrors newAuthDiscoverDriver.
func newProtocolInferDriver() (*ai.Driver, error) {
	gcfg := config.Load()
	projCfg, err := project.LoadFromFile(filepath.Join(".", ".cerberus", "project.yaml"))
	if err != nil {
		return nil, fmt.Errorf("load project.yaml: %w", err)
	}
	client, err := llm.NewClientWithConfig(llm.ClientConfig{
		Model:      projCfg.Settings.AIBudget.Model,
		APIKey:     gcfg.LLMAPIKey,
		BaseURL:    gcfg.LLMBaseURL,
		AuthScheme: gcfg.LLMAuthScheme,
	})
	if err != nil {
		return nil, fmt.Errorf("build llm client: %w", err)
	}
	total, perCall := projCfg.Settings.AIBudget.SessionTotalTokens, projCfg.Settings.AIBudget.PerCallLimit
	if total <= 0 {
		total = 200000
	}
	if perCall <= 0 {
		perCall = 10000
	}
	return ai.NewDriver(client, ai.NewTokenBudget(total, perCall)), nil
}
```

Register in `cmd/cerberus/main.go` by adding `protocolCmd(),` to the `rootCmd.AddCommand(...)` list.

- [ ] **Step 4: Run to verify pass** (incl. `auth discover` regression — additive).

- [ ] **Step 5: Commit** — `git commit -m "feat(cmd): add 'cerberus protocol infer' subcommand"`.

---

## Task 3: Docs

**Files:** `cerberus-docs/configuration/project.md`, `cerberus-docs/cli.md`

- [ ] **Step 1:** In `project.md`'s WebSocket protocol section (added in M3-1), add a line: "Draft a description from docs/examples with `cerberus protocol infer` (writes `.cerberus/protocols/<name>.yaml` for review) — see [CLI](cli.md)." In `cli.md`, add a `protocol infer` entry mirroring the `auth discover` entry, noting the draft/human-review posture.
- [ ] **Step 2:** `make check` green.
- [ ] **Step 3:** Commit — `git commit -m "docs(ws): document 'cerberus protocol infer'"`.

---

## Self-Review Notes

- **Spec coverage:** D1 (mirror auth discover) → Tasks 1–2 ✓; D2 (docs/examples input) → `readInputs` ✓; D3 (write M3-1 artifact + human gate) → `runProtocolInfer` confirm/dry-run ✓; D4 (secret hygiene, credential_ref only) → prompt + validation ✓; D5 (ValidateProtocol before write) → Task 1 ✓.
- **Provisional:** `buildInferPrompt` content, `isTextFile` set, and the mock-driver wiring TODO are explicitly provisional (🐶). The architecture and tests are concrete.
- **No mutation of `main_auth.go`** (additive command; `promptConfirm` reused read-only).
- **The Task 1 mock-driver TODO** is the one open code detail: confirm `internal/ai` exposes a way to inject a mock `llm.Client` (the spec's map says `ai.NewDriver(client, budget)` takes an `llm.Client`, so a test-local mock client works); the implementer resolves this at execution time.
