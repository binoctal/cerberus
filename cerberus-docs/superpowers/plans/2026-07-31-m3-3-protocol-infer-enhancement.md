# M3-3 `protocol infer` Enhancement — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate `protocoldiscover.Infer` from free-form JSON (`Decide`) to tool-calling (`DecideWithTools`) and align its LLM output with the `project.Protocol` model (Roles, Batches, `RoleHandshake.Optional`), then validate once against the real `open-agents` target.

**Architecture:** A new `tools.go` defines one `protocol_draft` tool (hand-written `InputSchema`, mirroring Scout's style) and a pure `argsToProtocol` assembler. `Infer` swaps `Decide` for `DecideWithTools` and uses three-state error handling (found=false → `ErrNoProtocol`; zero tool calls → drift hard error). `buildInferPrompt` drops its hand-written JSON shape and gains recognition guidance for four in-scope structures. No底层 model change.

**Tech Stack:** Go 1.25, `github.com/binoctal/cerberus`, `internal/llm` tool-calling (`DecideWithTools`), `internal/ai.Driver`, `internal/project.Protocol`.

## Global Constraints

- Commit author: `binoctal <binoctal@gmail.com>`, **no** `Co-Authored-By`.
- Commit messages and code comments in English.
- `make check` (fmt + lint + test) EXIT 0 + clean git tree after every task.
- No CGo. Follow existing comment density and naming idiom.
- Documents only in `cerberus-docs/` (never `docs/`).
- TDD: write the failing test, run it RED, implement, run it GREEN, commit. Every task also includes a negative-verification test where noted.

## File Structure

- **Create** `internal/protocoldiscover/tools.go` — `protocolDraftTool()` (the `llm.Tool`) + `argsToProtocol(map[string]any) (*project.Protocol, error)` (pure assembler). One responsibility: the tool surface + arg→Protocol assembly.
- **Create** `internal/protocoldiscover/tools_test.go` — unit tests for `argsToProtocol` and the tool schema.
- **Modify** `internal/protocoldiscover/infer.go` — `Infer` uses `DecideWithTools`; `buildInferPrompt` rewritten; `inferOutput`/`inferHandshake` extended.
- **Modify** `internal/protocoldiscover/infer_test.go` — migrate mocks from raw-JSON to tool-call fixtures; add three-state tests.
- **Create** `cerberus-docs/technical/dogfood/2026-07-31-m3-3-protocol-infer-dogfood.md` — dogfood record (Task 5).

---

## Task 1: `argsToProtocol` pure assembler

**Files:**
- Create: `internal/protocoldiscover/tools.go`
- Test: `internal/protocoldiscover/tools_test.go`

**Interfaces:**
- Produces: `func argsToProtocol(input map[string]any) (*project.Protocol, error)` — consumed by `Infer` in Task 4.

- [ ] **Step 1: Write the failing test**

Create `internal/protocoldiscover/tools_test.go`:

```go
package protocoldiscover

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/project"
)

func TestArgsToProtocol_FullShape(t *testing.T) {
	input := map[string]any{
		"found":     true,
		"framing":   "json",
		"type_path": "type",
		"auth": map[string]any{
			"strategy":      "query",
			"param":         "token",
			"credential_ref": "web",
		},
		"roles": map[string]any{
			"web": map[string]any{
				"credential_ref": "web",
				"params":         map[string]any{"type": "web"},
				"handshake": map[string]any{
					"await_type": "devices:sync",
					"timeout":    5,
					"optional":   true,
				},
			},
		},
		"batches": map[string]any{
			"session:output-batch": map[string]any{
				"item_type":  "session:output",
				"items_path": "payload.lines",
			},
		},
	}

	p, err := argsToProtocol(input)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "json", p.Framing)
	assert.Equal(t, "type", p.TypePath)
	require.NotNil(t, p.Auth)
	assert.Equal(t, "web", p.Auth.CredentialRef)

	require.Contains(t, p.Roles, "web")
	web := p.Roles["web"]
	assert.Equal(t, "web", web.CredentialRef)
	require.NotNil(t, web.Handshake)
	assert.Equal(t, "devices:sync", web.Handshake.AwaitType)
	assert.Equal(t, 5, web.Handshake.Timeout)
	assert.True(t, web.Handshake.Optional, "optional must propagate")

	require.Contains(t, p.Batches, "session:output-batch")
	b := p.Batches["session:output-batch"]
	assert.Equal(t, "session:output", b.ItemType)
	assert.Equal(t, "payload.lines", b.ItemsPath)
}

func TestArgsToProtocol_OmitsAbsentOptionals(t *testing.T) {
	// Minimal input: no roles/batches/handshake. The assembled Protocol must
	// have nil maps, not zero-length placeholders, and no handshake.
	input := map[string]any{
		"found":     true,
		"framing":   "json",
		"type_path": "type",
	}
	p, err := argsToProtocol(input)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Nil(t, p.Roles)
	assert.Nil(t, p.Batches)
	assert.Nil(t, p.Auth)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/protocoldiscover/ -run TestArgsToProtocol -v`
Expected: FAIL / compile error — `argsToProtocol` undefined.

- [ ] **Step 3: Write minimal implementation**

Create `internal/protocoldiscover/tools.go`:

```go
package protocoldiscover

import (
	"encoding/json"
	"fmt"

	"github.com/binoctal/cerberus/internal/project"
)

// argsToProtocol assembles a *project.Protocol from a parsed protocol_draft
// tool-call input. It JSON-round-trips the map through inferOutput (the same
// struct shape the legacy Decide path used) so assembly stays in one place;
// the round-trip cannot leak credential values because Protocol has no field
// for them (credential_ref is an actor name, not a token).
func argsToProtocol(input map[string]any) (*project.Protocol, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal tool args: %w", err)
	}
	var out inferOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parse tool args: %w", err)
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
	if len(out.Roles) > 0 {
		p.Roles = map[string]*project.ProtocolRole{}
	}
	for name, r := range out.Roles {
		role := &project.ProtocolRole{
			CredentialRef: r.CredentialRef,
			Params:        r.Params,
			Headers:       r.Headers,
			Subprotocols:  r.Subprotocols,
		}
		if r.Handshake != nil {
			role.Handshake = &project.RoleHandshake{
				AwaitType: r.Handshake.AwaitType,
				Timeout:   r.Handshake.Timeout,
				Optional:  r.Handshake.Optional,
			}
		}
		p.Roles[name] = role
	}
	if len(out.Batches) > 0 {
		p.Batches = map[string]*project.ProtocolBatch{}
	}
	for key, b := range out.Batches {
		p.Batches[key] = &project.ProtocolBatch{
			ItemType:  b.ItemType,
			ItemsPath: b.ItemsPath,
		}
	}
	return p, nil
}
```

`inferOutput` and `inferHandshake` (in `infer.go`) must be extended first. Add `Batches` and `Optional` fields now — add to `infer.go`:

```go
type inferOutput struct {
	Found    bool                   `json:"found"`
	Framing  string                 `json:"framing"`
	TypePath string                 `json:"type_path"`
	Auth     *inferAuth             `json:"auth,omitempty"`
	Roles    map[string]*inferRole  `json:"roles,omitempty"`
	Batches  map[string]*inferBatch `json:"batches,omitempty"`
	Notes    string                 `json:"notes"`
}

type inferBatch struct {
	ItemType  string `json:"item_type"`
	ItemsPath string `json:"items_path"`
}
```

and extend `inferHandshake`:

```go
type inferHandshake struct {
	AwaitType string `json:"await_type"`
	Timeout   int    `json:"timeout"`
	Optional  bool   `json:"optional"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/protocoldiscover/ -run TestArgsToProtocol -v`
Expected: PASS.

- [ ] **Step 5: `make check` + commit**

Run: `make check`
Commit:
```bash
git add internal/protocoldiscover/tools.go internal/protocoldiscover/tools_test.go internal/protocoldiscover/infer.go
git commit -m "feat(protocoldiscover): argsToProtocol assembles Protocol from tool args"
```

---

## Task 2: `protocol_draft` tool schema

**Files:**
- Modify: `internal/protocoldiscover/tools.go`
- Test: `internal/protocoldiscover/tools_test.go`

**Interfaces:**
- Produces: `func protocolDraftTool() llm.Tool` — consumed by `Infer` in Task 4.

- [ ] **Step 1: Write the failing test**

Append to `internal/protocoldiscover/tools_test.go`:

```go
func TestProtocolDraftTool_SchemaCoversAllStructures(t *testing.T) {
	tool := protocolDraftTool()
	assert.Equal(t, "protocol_draft", tool.Name)

	top := tool.InputSchema
	assert.Equal(t, "object", top["type"])
	props := top["properties"].(map[string]any)

	for _, field := range []string{"found", "framing", "type_path", "auth", "roles", "batches", "notes"} {
		assert.Contains(t, props, field, "schema missing top-level field %q", field)
	}

	// roles.<role>.handshake must expose `optional` (peer-gated conditional
	// handshake). Navigate the nested object schema.
	rolesProp := props["roles"].(map[string]any)
	assert.Equal(t, "object", rolesProp["type"])
	rolesProps := rolesProp["additionalProperties"].(map[string]any)
	roleProps := rolesProps["properties"].(map[string]any)
	handshake := roleProps["handshake"].(map[string]any)
	handshakeProps := handshake["properties"].(map[string]any)
	assert.Contains(t, handshakeProps, "optional", "handshake schema must expose optional")

	// batches.<key> exposes item_type + items_path.
	batchesProp := props["batches"].(map[string]any)
	batchesProps := batchesProp["additionalProperties"].(map[string]any)
	batchProps := batchesProps["properties"].(map[string]any)
	assert.Contains(t, batchProps, "item_type")
	assert.Contains(t, batchProps, "items_path")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/protocoldiscover/ -run TestProtocolDraftTool -v`
Expected: FAIL — `protocolDraftTool` undefined.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/protocoldiscover/tools.go`. Note the import addition of `github.com/binoctal/cerberus/internal/llm`:

```go
import (
	"encoding/json"
	"fmt"

	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

// protocolDraftTool is the typed tool Infer offers the LLM. Its InputSchema is
// hand-written (cerberus has no struct->schema reflection; this mirrors Scout's
// tools.go). The schema is the inferable subset of project.Protocol plus a
// `found` flag that lets the model explicitly signal "no WS protocol here"
// (distinct from drift — see Infer's zero-tool-call handling).
func protocolDraftTool() llm.Tool {
	str := func() map[string]any { return map[string]any{"type": "string"} }
	return llm.Tool{
		Name:        "protocol_draft",
		Description: "Draft a WebSocket protocol declaration from the provided docs/source. Call this once; set found=false if no WS protocol is described.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"found":     map[string]any{"type": "boolean", "description": "true if a WS protocol is described; false otherwise."},
				"framing":   map[string]any{"type": "string", "enum": []any{"json", "text", "binary"}},
				"type_path": map[string]any{"type": "string", "description": "Dotted JSON path to the routing key (e.g. \"type\", \"payload.kind\")."},
				"auth": map[string]any{"type": "object", "properties": map[string]any{
					"strategy":       map[string]any{"type": "string", "enum": []any{"query", "header", "subprotocol"}},
					"param":          str(),
					"credential_ref": str(),
				}},
				"roles": map[string]any{
					"type": "object",
					"description":      "Named connection types (e.g. web, bridge). Keys are role names.",
					"additionalProperties": map[string]any{"type": "object", "properties": map[string]any{
						"credential_ref": str(),
						"params":         map[string]any{"type": "object"},
						"headers":        map[string]any{"type": "object"},
						"subprotocols":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"handshake": map[string]any{"type": "object", "description": "Mandatory/best-effort post-connect exchange.", "properties": map[string]any{
							"await_type": str(),
							"timeout":    map[string]any{"type": "number"},
							"optional":   map[string]any{"type": "boolean", "description": "true = best-effort: a timeout still succeeds the connect (peer-gated handshake)."},
						}},
					}},
				},
				"batches": map[string]any{
					"type": "object",
					"description":      "Batch decomposition: when a frame's routing key matches a key here, expand the array at items_path into item_type frames.",
					"additionalProperties": map[string]any{"type": "object", "properties": map[string]any{
						"item_type":  str(),
						"items_path": str(),
					}},
				},
				"notes": str(),
			},
			"required": []any{"found"},
		},
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/protocoldiscover/ -run TestProtocolDraftTool -v`
Expected: PASS.

- [ ] **Step 5: Negative-verification test**

Append: a guard that the schema does NOT carry path-param fields (they belong to the actor layer, out of scope):

```go
func TestProtocolDraftTool_SchemaHasNoPathParam(t *testing.T) {
	tool := protocolDraftTool()
	props := tool.InputSchema["properties"].(map[string]any)
	for _, banned := range []string{"path_params", "url", "path"} {
		assert.NotContains(t, props, banned, "path-param concerns belong to the actor/auth layer, not Protocol")
	}
}
```

Run: `go test ./internal/protocoldiscover/ -run TestProtocolDraftTool -v`
Expected: PASS. (RED only if someone later adds a path field — the guard catches scope drift.)

- [ ] **Step 6: `make check` + commit**

```bash
git add internal/protocoldiscover/tools.go internal/protocoldiscover/tools_test.go
git commit -m "feat(protocoldiscover): protocol_draft tool schema (roles/batches/optional)"
```

---

## Task 3: Rewrite `buildInferPrompt`

**Files:**
- Modify: `internal/protocoldiscover/infer.go` (`buildInferPrompt`)
- Test: `internal/protocoldiscover/infer_test.go`

**Interfaces:**
- Consumes: `SourceFile`.
- Produces: the prompt string consumed by `Infer` (still called `buildInferPrompt`).

- [ ] **Step 1: Write the failing test**

Append to `internal/protocoldiscover/infer_test.go`:

```go
func TestBuildInferPrompt_RecognitionGuidance(t *testing.T) {
	prompt := buildInferPrompt("rt", []SourceFile{{Path: "room.ts", Content: "..."}})

	// The four in-scope structures must be called out so the model knows to
	// populate the corresponding tool fields.
	for _, want := range []string{"envelope", "batch", "handshake", "role"} {
		assert.Contains(t, prompt, want, "prompt must guide recognition of %q", want)
	}
	// The prompt must no longer hand-write a JSON shape block (the tool schema
	// now carries the shape). The legacy marker was the literal `\"found\":`.
	assert.NotContains(t, prompt, `"found":`, "prompt must not hand-write the JSON shape; the tool schema owns it")
	// credential_ref safety constraint must remain.
	assert.Contains(t, prompt, "credential_ref")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/protocoldiscover/ -run TestBuildInferPrompt -v`
Expected: FAIL — the old prompt still contains `"found":` and lacks the guidance words.

- [ ] **Step 3: Rewrite `buildInferPrompt`**

Replace the body of `buildInferPrompt` in `internal/protocoldiscover/infer.go` with:

```go
// buildInferPrompt assembles the prompt. It describes WHAT to recognize and
// leaves the JSON shape to the protocol_draft tool schema (the tool definition
// carries it; the prompt no longer hand-writes a shape block). It never
// includes credential values — credential_ref names an actor, it is not a token.
func buildInferPrompt(serviceName string, inputs []SourceFile) string {
	var b strings.Builder
	b.WriteString("You are drafting a WebSocket protocol description for a cerberus test config.\n")
	b.WriteString("Read the docs/source below and call the protocol_draft tool once with what you infer.\n")
	fmt.Fprintf(&b, "The target service is %q.\n\n", serviceName)
	b.WriteString("Recognize and fill these structures where present:\n")
	b.WriteString("- Wire framing (json/text/binary) and the envelope: the dotted path to the message routing key (e.g. a {type,payload,...} envelope -> type_path \"type\").\n")
	b.WriteString("- How auth is attached (query param / header / subprotocol) and which actor supplies it.\n")
	b.WriteString("- Connection roles: distinct connection types (e.g. web, bridge) and their discriminator params/headers/subprotocols.\n")
	b.WriteString("- Post-connect handshake: a message the server sends (or you must send) right after connect. If it is peer-gated (only sent when a peer socket is online), set handshake.optional=true so a timeout does not fail the connect.\n")
	b.WriteString("- Message batching: if the server coalesces frames (e.g. emits a batch type carrying an array), record the batch routing key with item_type (per-item routing key) and items_path (dotted path to the array).\n\n")
	b.WriteString("If no WebSocket protocol is described, call protocol_draft with found=false.\n")
	b.WriteString("credential_ref MUST name an actor that exists in project.yaml — NEVER invent one, and NEVER include real credential values or tokens.\n\n")
	for _, f := range inputs {
		b.WriteString("--- " + filepath.Base(f.Path) + " ---\n")
		b.WriteString(truncateContent(f.Content, 8000))
		b.WriteString("\n\n")
	}
	return b.String()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/protocoldiscover/ -run TestBuildInferPrompt -v`
Expected: PASS.

- [ ] **Step 5: `make check` + commit**

```bash
git add internal/protocoldiscover/infer.go internal/protocoldiscover/infer_test.go
git commit -m "feat(protocoldiscover): prompt recognizes roles/batches/optional handshake"
```

---

## Task 4: Migrate `Infer` to `DecideWithTools` + three-state errors

**Files:**
- Modify: `internal/protocoldiscover/infer.go` (`Infer`)
- Test: `internal/protocoldiscover/infer_test.go`

**Interfaces:**
- Consumes: `protocolDraftTool()` (Task 2), `argsToProtocol` (Task 1), `Driver.DecideWithTools`.
- Produces: unchanged `Infer` signature and `ErrNoProtocol`.

- [ ] **Step 1: Migrate the positive test first**

The existing `TestInfer_ReturnsValidatedProtocol` uses `mockDriver` (raw JSON via `Decide`). Replace the mock with a tool-call fixture. First replace `mockDriver`/`mockDriverRaw` helpers in `infer_test.go` with a tool-call helper:

```go
// mockToolDriver presets a single protocol_draft tool call returned regardless
// of prompt (key "default" matches any prompt per MockClient.matchKey).
func mockToolDriver(t *testing.T, input map[string]any) *ai.Driver {
	t.Helper()
	mock := llm.NewMockClient(nil)
	mock.SetToolResponse("default", []llm.ToolCall{
		{Name: "protocol_draft", Input: input},
	})
	return ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000))
}

// mockEmptyDriver returns no tool calls (simulates drift).
func mockEmptyDriver(t *testing.T) *ai.Driver {
	t.Helper()
	mock := llm.NewMockClient(nil) // no SetToolResponse -> empty ToolCalls
	return ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000))
}
```

Update `TestInfer_ReturnsValidatedProtocol` to use `mockToolDriver` with a map shaped like the tool input (found=true, framing, type_path, auth). Keep its assertions (Framing, TypePath, Auth.CredentialRef). Delete the now-unused `mockDriver`/`mockDriverRaw` if no other test references them.

- [ ] **Step 2: Run the positive test to verify it fails**

Run: `go test ./internal/protocoldiscover/ -run TestInfer_ReturnsValidatedProtocol -v`
Expected: FAIL — `Infer` still calls `Decide`, which gets no JSON content and errors.

- [ ] **Step 3: Add the three-state tests**

Append to `infer_test.go`:

```go
func TestInfer_FoundFalse_ReturnsErrNoProtocol(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{Name: "rt", URL: "http://x"}}, Actors: []project.Actor{{Name: "web"}}}
	driver := mockToolDriver(t, map[string]any{"found": false})
	_, err := Infer(context.Background(), driver, cfg, "rt", []SourceFile{{Path: "d.md", Content: "..."}})
	require.ErrorIs(t, err, ErrNoProtocol)
}

// Negative-verification: drift (zero tool calls) must NOT be reported as
// ErrNoProtocol. It is a hard error. This test RED-fails if a future change
// collapses drift back into the clean not-found path.
func TestInfer_ZeroToolCalls_IsHardErrorNotErrNoProtocol(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{Name: "rt", URL: "http://x"}}, Actors: []project.Actor{{Name: "web"}}}
	driver := mockEmptyDriver(t)
	_, err := Infer(context.Background(), driver, cfg, "rt", []SourceFile{{Path: "d.md", Content: "..."}})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrNoProtocol, "drift (zero tool calls) is a hard error, not a clean not-found")
}

func TestInfer_InvalidCredentialRef_IsHardError(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{Name: "rt", URL: "http://x"}}, Actors: []project.Actor{{Name: "web"}}}
	// credential_ref "ghost" names no actor -> ValidateProtocol rejects.
	driver := mockToolDriver(t, map[string]any{
		"found":     true,
		"framing":   "json",
		"type_path": "type",
		"auth":      map[string]any{"strategy": "query", "param": "token", "credential_ref": "ghost"},
	})
	_, err := Infer(context.Background(), driver, cfg, "rt", []SourceFile{{Path: "d.md", Content: "..."}})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrNoProtocol)
	assert.NotContains(t, err.Error(), "raw", "error must not leak raw LLM response")
}
```

- [ ] **Step 4: Implement the three-state `Infer`**

Replace the body of `Infer` in `internal/protocoldiscover/infer.go`:

```go
func Infer(ctx context.Context, driver *ai.Driver, cfg *project.Config, serviceName string, inputs []SourceFile) (*project.Protocol, error) {
	if driver == nil {
		return nil, errors.New("nil driver")
	}

	prompt := buildInferPrompt(serviceName, inputs)
	res, err := driver.DecideWithTools(ctx, prompt, []llm.Tool{protocolDraftTool()})
	if err != nil {
		return nil, errors.New("could not run protocol inference")
	}
	if len(res.ToolCalls) == 0 {
		// Drift: the model produced no tool call. This is NOT "no protocol
		// found" — that is signalled explicitly by found=false below. Drift is
		// a hard error, aligned with the S2 ADR's drift policy.
		return nil, errors.New("model produced no protocol tool call")
	}

	input := res.ToolCalls[0].Input
	// Safe read: the model may omit `found`; a missing flag is treated as
	// "not found" rather than panicking on a failed type assertion.
	if found, _ := input["found"].(bool); !found {
		return nil, ErrNoProtocol
	}

	p, err := argsToProtocol(input)
	if err != nil {
		// Malformed args; do not propagate the raw map in the error.
		return nil, errors.New("could not parse model output")
	}

	if err := project.ValidateProtocol(p, actorsOf(cfg)); err != nil {
		return nil, fmt.Errorf("model produced an invalid protocol: %w", err)
	}
	return p, nil
}
```

- [ ] **Step 5: Run all protocoldiscover tests**

Run: `go test ./internal/protocoldiscover/ -v`
Expected: all PASS, including the three-state tests.

- [ ] **Step 6: `make check` + commit**

```bash
git add internal/protocoldiscover/infer.go internal/protocoldiscover/infer_test.go
git commit -m "feat(protocoldiscover): Infer uses DecideWithTools, three-state errors"
```

---

## Task 5: Signal-level dogfood against `open-agents`

**Files:**
- Create: `cerberus-docs/technical/dogfood/2026-07-31-m3-3-protocol-infer-dogfood.md`

This task is a manual run + record; no Go test. It produces the document that closes the M3-3 trigger opened on 2026-07-23.

- [ ] **Step 1: Start the target**

In the `open-agents` repo (`/home/mason/Documents/code_projects/private/open-agents`):

```
fnm use 22
cd apps/api && npm run dev    # wrangler dev, port 8989
```

Confirm it is up: a direct WS dial to `ws://localhost:8989/ws/demo_user?type=web&token=demo_token` must OPEN (per the 2026-07-23 dogfood). Stop here and record a setup blocker if it does not.

- [ ] **Step 2: Run `protocol infer`**

From a cerberus project scratch dir configured to reach open-agents (an existing `protocol_ref` project works; reuse `.cerberus/project.yaml` from the 2026-07-23 dogfood if present):

```
cerberus protocol infer --name open-agents \
  --from apps/api/src/realtime --service rt --dry-run
```

Capture the full draft output. Use `--dry-run` so nothing is written.

- [ ] **Step 3: Assess per-structure coverage**

Score the draft against the four in-scope structures the 2026-07-23 dogfood discovered manually:

| Structure | Expected (from manual discovery) | Drafted? |
|---|---|---|
| Envelope / type_path | `{type,payload,timestamp}` → type_path `type` | yes/no |
| Multi-role | `web` and `bridge` roles | yes/no |
| Conditional handshake | `devices:sync` await, optional (peer-gated) | yes/no |
| Batching | `session:output-batch` → item_type `session:output`, items_path `payload.lines` | yes/no |

- [ ] **Step 4: Write the dogfood record**

Create `cerberus-docs/technical/dogfood/2026-07-31-m3-3-protocol-infer-dogfood.md` with: the draft YAML, the coverage table, which structures were missed and why (prompt iteration points), and the path-param note (it is an actor/auth-discover follow-up, not an Infer gap). Use the same structure as the other files under `cerberus-docs/technical/dogfood/`.

- [ ] **Step 5: Commit**

```bash
git add cerberus-docs/technical/dogfood/2026-07-31-m3-3-protocol-infer-dogfood.md
git commit -m "docs(dogfood): M3-3 protocol infer signal-level run against open-agents"
```

---

## Self-Review

**Spec coverage:** spec §Architecture → T1 (argsToProtocol) + T2 (tool) + T4 (Infer migration) + T3 (prompt). spec §"Error handling" three-state table → T4 tests (found=false, zero calls, invalid args). spec §Testing → T1/T2/T4. spec §Dogfood → T5. spec §"Out of scope" (path params) → T2 negative test + T5 note. All sections mapped.

**Placeholder scan:** No TBD/TODO/"add validation"/"implement later" remains. T4's negative-verification test (`ZeroToolCalls_IsHardErrorNotErrNoProtocol`) is written as a complete, correct test up front. All code blocks are complete and directly runnable.

**Type consistency:** `argsToProtocol(map[string]any) (*project.Protocol, error)` (T1) matches the call in T4. `protocolDraftTool() llm.Tool` (T2) matches T4's `[]llm.Tool{protocolDraftTool()}`. `inferOutput.Batches`/`inferHandshake.Optional` (T1) match T2's schema fields. `found` is read from the raw `Input` map in T4, consistent with the tool schema carrying it. Mock helpers (`mockToolDriver`/`mockEmptyDriver`) defined once in T4 Step 1.
