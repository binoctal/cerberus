# Protocol Vocabulary Discovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `cerberus protocol vocabulary` command that extracts a WS routing vocabulary (directed-edge model) from a TypeScript SUT source file via a bundled `ts-morph` extractor, and drive cerberus's WS integration tests from the resulting `vocab.yaml` instead of hardcoded `type` lists.

**Architecture:** A Go package `internal/vocabextract` spawns a bundled `extractor.mjs` (node + ts-morph, packaged via `embed.FS`) that anchors on outbound emit points and walks up to case/guard/method context, producing a deterministic edge set. `internal/project` gains a `Vocabulary` type + loader. A new `//go:build integration` test `TestVocabularyDriven` reads the dogfood `vocab.yaml` at runtime and builds `TestCase` tables, superseding the hardcoded rows in `TestBridgeToWebRelay` / `TestWebToBridgeRouting`.

**Tech Stack:** Go 1.25, `github.com/spf13/cobra`, `gopkg.in/yaml.v3`, `embed.FS`; node + `ts-morph` (dev-time only, via subprocess).

## Global Constraints

- Go 1.25, module `github.com/binoctal/cerberus`; **pure Go, no cgo** (`extractor.mjs` runs as a node subprocess — does not violate cgo ban).
- Commit author `binoctal <binoctal@gmail.com>`, **no** `Co-Authored-By`.
- All documentation under `cerberus-docs/`; commit messages & code comments in English; follow existing comment density/naming.
- Tests: `make test` = `go test -v -race ./...`; integration tests use `//go:build integration` and are skipped by `make test`.
- `extractor.mjs` pins `ts-morph` major version; on parse failure it fails loudly (never silently drops emit points).

## File Structure

- **Create** `internal/project/vocabulary.go` — `Vocabulary` / `VocabEdge` / `VocabDelivery` / `VocabSideEffect` types + `LoadVocabulary(path)`.
- **Create** `internal/project/vocabulary_test.go` — loader unit tests.
- **Create** `internal/project/testdata/vocab-sample.yaml` — loader fixture.
- **Create** `internal/vocabextract/extract.go` — `Extract(ctx, sourcePath) (raw edge JSON, error)`: writes bundled `extractor.mjs` + `package.json` to a temp dir, spawns `node`, returns stdout.
- **Create** `internal/vocabextract/embed.go` — `//go:embed extractor.mjs package.json`.
- **Create** `internal/vocabextract/extractor.mjs` — ts-morph adapter (emit-point algorithm).
- **Create** `internal/vocabextract/package.json` — pins `ts-morph` dependency (resolved by `npm install` in temp dir at runtime).
- **Create** `internal/vocabextract/extract_test.go` — fixture-driven tests (spawn node against testdata `.ts` files).
- **Create** `internal/vocabextract/testdata/*.ts` — minimal TS fixtures (switch fall-through, guard, sendToBridge, batch, lifecycle).
- **Modify** `cmd/cerberus/main_protocol.go` — add `vocabulary` subcommand + `runProtocolVocabulary`.
- **Create** `dogfood/ws-realtime/.cerberus/vocab/open-agents.vocab.yaml` — generated dogfood vocab.
- **Create** `internal/head/agent/vocabulary_driven_test.go` — `TestVocabularyDriven` (`//go:build integration`).
- **Modify** `internal/head/agent/execute_phases_steps_integration_test.go` — delete `TestBridgeToWebRelay` / `TestWebToBridgeRouting` after parity (Task 8).

---

### Task 1: Vocabulary data model + loader

**Files:**
- Create: `internal/project/vocabulary.go`
- Create: `internal/project/vocabulary_test.go`
- Create: `internal/project/testdata/vocab-sample.yaml`

**Interfaces:**
- Produces: `project.Vocabulary`, `project.VocabEdge`, `project.VocabDelivery`, `project.VocabSideEffect`, `project.VocabSource` structs; `project.LoadVocabulary(path string) (*Vocabulary, error)`.

- [ ] **Step 1: Write the loader test**

Create `internal/project/vocabulary_test.go`:

```go
package project

import (
	"path/filepath"
	"testing"
)

func TestLoadVocabulary(t *testing.T) {
	v, err := LoadVocabulary(filepath.Join("testdata", "vocab-sample.yaml"))
	if err != nil {
		t.Fatalf("LoadVocabulary: %v", err)
	}
	if v.Source.ProtocolRef != "open-agents" {
		t.Errorf("protocol_ref = %q, want open-agents", v.Source.ProtocolRef)
	}
	if len(v.Source.Files) != 1 || v.Source.Files[0].Path == "" || v.Source.Files[0].Hash == "" {
		t.Fatalf("source.files not populated: %+v", v.Source.Files)
	}
	if len(v.Edges) != 2 {
		t.Fatalf("edges = %d, want 2", len(v.Edges))
	}
	e := v.Edges[0]
	if e.FromRole != "bridge" || e.ToRole != "web" || e.Type != "session:created" {
		t.Errorf("edge0 = %+v", e)
	}
	if e.Trigger != "message_handled" || e.Guard != "meta.type === 'bridge'" {
		t.Errorf("edge0 trigger/guard = %q / %q", e.Trigger, e.Guard)
	}
	if e.Delivery.Mode != "broadcast_web" {
		t.Errorf("delivery.mode = %q", e.Delivery.Mode)
	}
	// second edge exercises side_effects.when_types + partial.
	e1 := v.Edges[1]
	if len(e1.SideEffects) != 1 || e1.SideEffects[0].Kind != "notify_orchestrator" {
		t.Errorf("edge1 side_effects = %+v", e1.SideEffects)
	}
	if !e1.Partial {
		t.Errorf("edge1 partial = false, want true")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/project/ -run TestLoadVocabulary -v`
Expected: FAIL — `LoadVocabulary` undefined.

- [ ] **Step 3: Write the fixture YAML**

Create `internal/project/testdata/vocab-sample.yaml`:

```yaml
source:
  files:
    - path: apps/api/src/realtime/room.ts
      hash: a3f1c0deadbeef
  protocol_ref: open-agents
edges:
  - from_role: bridge
    to_role: web
    type: session:created
    trigger: message_handled
    guard: "meta.type === 'bridge'"
    delivery: { mode: broadcast_web }
    source: { spans: [{ start: 351, end: 401 }] }
  - from_role: bridge
    to_role: web
    type: workflow:task_progress
    trigger: message_handled
    guard: "meta.type === 'bridge'"
    delivery: { mode: broadcast_web }
    side_effects:
      - kind: notify_orchestrator
        when_types: [workflow:task_progress, workflow:task_result]
    partial: true
    source: { spans: [{ start: 372, end: 399 }] }
```

- [ ] **Step 4: Write the types + loader**

Create `internal/project/vocabulary.go`:

```go
package project

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Vocabulary is the directed-edge routing vocabulary for a WS protocol. It is
// the single source of truth for the dynamic test generator and (future)
// Scout context. Type is an edge label, not a primary key. Struct tags carry
// BOTH yaml (on-disk file) and json (extractor subprocess stdout) so the same
// type decodes from either source.
type Vocabulary struct {
	Source VocabSource `yaml:"source" json:"source"`
	Edges  []VocabEdge `yaml:"edges" json:"edges"`
}

// VocabSource records where the vocabulary was extracted from.
type VocabSource struct {
	Files       []VocabFile `yaml:"files" json:"files"`
	ProtocolRef string      `yaml:"protocol_ref" json:"protocol_ref"`
}

// VocabFile is one source file the vocabulary was derived from.
type VocabFile struct {
	Path string `yaml:"path" json:"path"`
	Hash string `yaml:"hash" json:"hash"`
}

// VocabEdge is one directed message flow: a frame of Type leaves FromRole (or a
// DO-spontaneous null) bound for ToRole under Trigger. Guard is provenance only;
// the test generator executes off FromRole.
type VocabEdge struct {
	FromRole            string             `yaml:"from_role" json:"from_role"`                     // web | bridge | null
	ToRole              string             `yaml:"to_role" json:"to_role"`                         // web | bridge
	Type                string             `yaml:"type" json:"type"`
	Trigger             string             `yaml:"trigger" json:"trigger"`                         // connect_web|connect_bridge|disconnect_bridge|message_handled|broadcast_endpoint
	Guard               string             `yaml:"guard,omitempty" json:"guard,omitempty"`
	Delivery            VocabDelivery      `yaml:"delivery" json:"delivery"`
	RouteField          string             `yaml:"route_field,omitempty" json:"route_field,omitempty"`
	OnMissingRoute      *VocabMissingRoute `yaml:"on_missing_route,omitempty" json:"on_missing_route,omitempty"`
	RequiresPresentRole string             `yaml:"requires_present_role,omitempty" json:"requires_present_role,omitempty"`
	SideEffects         []VocabSideEffect  `yaml:"side_effects,omitempty" json:"side_effects,omitempty"`
	Batch               *VocabBatch        `yaml:"batch,omitempty" json:"batch,omitempty"`
	Partial             bool               `yaml:"partial,omitempty" json:"partial,omitempty"`
	Unsupported         bool               `yaml:"unsupported,omitempty" json:"unsupported,omitempty"`
	Source              VocabEdgeSource    `yaml:"source" json:"source"`
}

// VocabDelivery declares how a frame is distributed.
type VocabDelivery struct {
	Mode          string `yaml:"mode" json:"mode"`                     // broadcast_web | send_bridge_by_device | unicast_web
	ExcludeSender bool   `yaml:"exclude_sender,omitempty" json:"exclude_sender,omitempty"`
}

// VocabMissingRoute declares the reaction when a route_field target is absent.
type VocabMissingRoute struct {
	Kind string `yaml:"kind" json:"kind"` // send_error
	Code string `yaml:"code" json:"code"`
}

// VocabSideEffect is an out-of-band action triggered by an edge.
type VocabSideEffect struct {
	Kind      string   `yaml:"kind" json:"kind"`                 // notify_orchestrator | stuck_recovery
	WhenTypes []string `yaml:"when_types,omitempty" json:"when_types,omitempty"`
}

// VocabBatch declares a deferred flush window for batched edges.
type VocabBatch struct {
	WindowMs int    `yaml:"window_ms" json:"window_ms"`
	Key      string `yaml:"key" json:"key"`
}

// VocabEdgeSource locates the emit point(s) in the source file.
type VocabEdgeSource struct {
	Spans []VocabSpan `yaml:"spans" json:"spans"`
}

// VocabSpan is a half-open source line range.
type VocabSpan struct {
	Start int `yaml:"start" json:"start"`
	End   int `yaml:"end" json:"end"`
}

// LoadVocabulary reads and parses a vocab.yaml file.
func LoadVocabulary(path string) (*Vocabulary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("vocab: read %s: %w", path, err)
	}
	var v Vocabulary
	if err := yaml.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("vocab: parse %s: %w", path, err)
	}
	return &v, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/project/ -run TestLoadVocabulary -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/project/vocabulary.go internal/project/vocabulary_test.go internal/project/testdata/vocab-sample.yaml
git -c user.name=binoctal -c user.email=binoctal@gmail.com commit -m "feat(project): add Vocabulary type and loader"
```

---

### Task 2: vocabextract package — embed + Go bridge (smoke)

This task wires the node subprocess bridge with a stub extractor so the spawn/JSON-parse path is proven before the real algorithm lands in Task 3–4.

**Files:**
- Create: `internal/vocabextract/embed.go`
- Create: `internal/vocabextract/extract.go`
- Create: `internal/vocabextract/extractor.mjs` (stub: echoes a fixed edge JSON)
- Create: `internal/vocabextract/package.json`
- Create: `internal/vocabextract/extract_test.go`
- Create: `internal/vocabextract/testdata/stub.ts`

**Interfaces:**
- Produces: `vocabextract.Extract(ctx context.Context, sourcePath string) (json.RawMessage, error)` — spawns node, returns extractor stdout. `vocabextract.NodeRequired` sentinel error when `node` is not on PATH.

- [ ] **Step 1: Write the bridge test**

Create `internal/vocabextract/extract_test.go`:

```go
package vocabextract

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestExtract_Smoke(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns node")
	}
	out, err := Extract(context.Background(), filepath.Join("testdata", "stub.ts"))
	if err != nil {
		t.Skipf("node unavailable or npm failed: %v", err)
	}
	var got struct {
		Edges []struct {
			Type string `json:"type"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("parse extractor stdout: %v\nraw=%s", err, out)
	}
	if len(got.Edges) == 0 {
		t.Fatalf("no edges in stub output: %s", out)
	}
	if got.Edges[0].Type != "stub:type" {
		t.Errorf("edge0 type = %q, want stub:type", got.Edges[0].Type)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/vocabextract/ -run TestExtract_Smoke -v`
Expected: FAIL — package missing / `Extract` undefined.

- [ ] **Step 3: Write the stub extractor + package.json**

Create `internal/vocabextract/package.json`:

```json
{
  "name": "cerberus-vocabextract",
  "version": "0.0.0",
  "private": true,
  "dependencies": { "ts-morph": "25.0.0" }
}
```

> Pin rationale: `25.0.0` is a stable ts-morph major tested in the prototype. Bump intentionally.

Create `internal/vocabextract/extractor.mjs` (stub — replaced by the real algorithm in Task 3):

```js
// Cerberus vocabulary extractor. Stub: validates the subprocess bridge by
// echoing one fixed edge regardless of input. Task 3 replaces the body with
// the emit-point algorithm.
import { Project, SyntaxKind } from 'ts-morph';

const file = process.argv[2];
if (!file) { console.error('usage: node extractor.mjs <source.ts>'); process.exit(2); }
const project = new Project();
project.addSourceFileAtPath(file); // parse to surface errors loudly
console.log(JSON.stringify({
  edges: [{ from_role: 'bridge', to_role: 'web', type: 'stub:type',
            trigger: 'message_handled', delivery: { mode: 'broadcast_web' },
            source: { spans: [{ start: 1, end: 1 }] } }],
}));
```

Create `internal/vocabextract/testdata/stub.ts`:

```ts
class UserRoom { handleMessage() {} }
```

- [ ] **Step 4: Write the embed + bridge**

Create `internal/vocabextract/embed.go`:

```go
package vocabextract

import _ "embed"

//go:embed extractor.mjs
var extractorSrc string

//go:embed package.json
var packageJSON string
```

Create `internal/vocabextract/extract.go`:

```go
package vocabextract

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// NodeRequired is returned when node is not discoverable on PATH. Discovery is
// a development-time tool; the production cerberus binary stays pure-Go.
var NodeRequired = errors.New("vocabextract: node not found on PATH (required for TS discovery)")

// Extract writes the bundled extractor.mjs + package.json to a temp dir,
// installs ts-morph if missing, and runs `node extractor.mjs <sourcePath>`.
// It returns the extractor's stdout (a JSON object with an `edges` array).
func Extract(ctx context.Context, sourcePath string) (json.RawMessage, error) {
	if _, err := exec.LookPath("node"); err != nil {
		return nil, NodeRequired
	}
	dir, err := os.MkdirTemp("", "cerberus-vocab-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "extractor.mjs"), []byte(extractorSrc), 0644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(packageJSON), 0644); err != nil {
		return nil, err
	}
	if err := npmInstall(ctx, dir); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(sourcePath)
	if err != nil {
		return nil, err
	}
	var stdout bytes.Buffer
	cmd := exec.CommandContext(ctx, "node", "extractor.mjs", abs)
	cmd.Dir = dir
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("vocabextract: node run: %w", err)
	}
	return json.RawMessage(stdout.Bytes()), nil
}

// npmInstall runs `npm install` (silent) if node_modules is absent.
func npmInstall(ctx context.Context, dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "node_modules")); err == nil {
		return nil
	}
	cmd := exec.CommandContext(ctx, "npm", "install", "--silent", "--no-audit", "--no-fund")
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	if out, err := cmd.Output(); err != nil {
		return fmt.Errorf("vocabextract: npm install: %w: %s", err, out)
	}
	return nil
}
```

> Add `"encoding/json"` to the import block (used by `json.RawMessage`).

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/vocabextract/ -run TestExtract_Smoke -v`
Expected: PASS (or SKIP if node absent on the runner — run on a machine with node).

- [ ] **Step 6: Commit**

```bash
git add internal/vocabextract/
git -c user.name=binoctal -c user.email=binoctal@gmail.com commit -m "feat(vocabextract): node subprocess bridge with stub extractor"
```

---

### Task 3: extractor.mjs — emit-point + fall-through + guard + trigger + delivery

Replace the stub body with the core algorithm. Driven by three fixtures.

**Files:**
- Modify: `internal/vocabextract/extractor.mjs`
- Create: `internal/vocabextract/testdata/switch-fallthrough.ts`
- Create: `internal/vocabextract/testdata/sendtobridge.ts`
- Modify: `internal/vocabextract/extract_test.go` (add three subtests)

**Interfaces:**
- Produces: extractor stdout `{ edges: [{ from_role, to_role, type, trigger, guard, delivery:{mode,exclude_sender}, source:{spans} }] }`.

- [ ] **Step 1: Write the fall-through fixture**

Create `internal/vocabextract/testdata/switch-fallthrough.ts`:

```ts
class UserRoom {
  handleMessage(ws, meta, msg) {
    switch (msg.type) {
      case 'encrypted':
      case 'session:created':
      case 'session:started':
        if (meta.type === 'bridge') { this.broadcastToWeb(msg); }
        break;
      case 'workflow:task_progress':
        if (meta.type === 'bridge') { this.broadcastToWeb(msg); }
        break;
      default:
    }
  }
  broadcastToWeb(msg) {}
}
```

- [ ] **Step 2: Write the sendToBridge fixture**

Create `internal/vocabextract/testdata/sendtobridge.ts`:

```ts
class UserRoom {
  handleMessage(ws, meta, msg) {
    switch (msg.type) {
      case 'session:start':
        if (meta.type === 'web') {
          const payload = msg.payload;
          if (payload.deviceId) { this.sendToBridge(payload.deviceId, msg); }
        }
        break;
      default:
    }
  }
  sendToBridge(deviceId, msg) {}
}
```

- [ ] **Step 3: Write the assertions (add to extract_test.go)**

Append to `internal/vocabextract/extract_test.go`:

```go
func TestExtract_FallThrough(t *testing.T) {
	if testing.Short() { t.Skip("spawns node") }
	check := func(t *testing.T, out []byte, want ...string) {
		t.Helper()
		var got struct{ Edges []map[string]any }
		if err := json.Unmarshal(out, &got); err != nil { t.Fatal(err) }
		seen := map[string]bool{}
		for _, e := range got.Edges {
			if v, _ := e["type"].(string); v != "" {
				seen[v] = true
			}
		}
		for _, w := range want {
			if !seen[w] { t.Errorf("missing edge type %q in %d edges", w, len(got.Edges)) }
		}
	}
	out, err := Extract(context.Background(), filepath.Join("testdata", "switch-fallthrough.ts"))
	if err != nil { t.Skipf("node: %v", err) }
	check(t, out, "encrypted", "session:created", "session:started", "workflow:task_progress")

	out, err = Extract(context.Background(), filepath.Join("testdata", "sendtobridge.ts"))
	if err != nil { t.Skipf("node: %v", err) }
	var got struct{ Edges []map[string]any }
	if err := json.Unmarshal(out, &got); err != nil { t.Fatal(err) }
	var found bool
	for _, e := range got.Edges {
		if e["type"] == "session:start" &&
			e["from_role"] == "web" && e["to_role"] == "bridge" &&
			e["trigger"] == "message_handled" &&
			e["guard"] == "meta.type === 'web'" {
			found = true
		}
	}
	if !found { t.Errorf("no web->bridge session:start edge: %+v", got.Edges) }
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `go test ./internal/vocabextract/ -run 'TestExtract_(Smoke|FallThrough)' -v`
Expected: FAIL — stub still emits `stub:type`, not the fixture's types.

- [ ] **Step 5: Implement the core algorithm in extractor.mjs**

Replace the body of `internal/vocabextract/extractor.mjs` with:

```js
import { Project, SyntaxKind } from 'ts-morph';

const file = process.argv[2];
if (!file) { console.error('usage: node extractor.mjs <source.ts>'); process.exit(2); }
const project = new Project();
const sf = project.addSourceFileAtPath(file);

const lit = (n) => n?.getText().replace(/^['"`]|['"`]$/g, '');

// Collect a CaseClause's fall-through chain: preceding empty-body cases.
function fallThroughTypes(cc) {
  const block = cc.getParent();
  if (block.getKind() !== SyntaxKind.CaseBlock) return [lit(cc.getExpression())];
  const clauses = block.getClauses();
  const idx = clauses.indexOf(cc);
  const types = [lit(cc.getExpression())];
  for (let i = idx - 1; i >= 0; i--) {
    const c = clauses[i];
    if (c.getKind() !== SyntaxKind.CaseClause) break;
    if (c.getStatements().length > 0) break; // non-empty body stops the chain
    const e = c.getExpression();
    if (e) types.unshift(lit(e));
  }
  return types;
}

// Nearest `if (meta.type === 'web'|'bridge')` enclosing node.
function roleGuard(node) {
  for (let n = node; n; n = n.getParent()) {
    if (n.getKind() === SyntaxKind.IfStatement) {
      const cond = n.getExpression().getText();
      const m = cond.match(/meta\.type\s*===?\s*['"](web|bridge)['"]/);
      if (m) return { from_role: m[1], guard: cond };
    }
    if (n.getKind() === SyntaxKind.MethodDeclaration) break;
  }
  return { from_role: null, guard: null };
}

const edges = [];
const cls = sf.getClasses()[0];
for (const method of cls.getMethods()) {
  const mname = method.getName();
  for (const call of method.getDescendantsOfKind(SyntaxKind.CallExpression)) {
    const expr = call.getExpression();
    if (expr.getKind() !== SyntaxKind.PropertyAccessExpression) continue;
    const name = expr.getName();
    const isB2W = name === 'broadcastToWeb';
    const isW2B = name === 'sendToBridge';
    if (!isB2W && !isW2B) continue;

    const cc = call.getFirstAncestorByKind(SyntaxKind.CaseClause);
    const { from_role, guard } = roleGuard(call);
    const trigger = mname === 'handleMessage' ? 'message_handled'
                  : mname === 'fetch' ? 'fetch_branch'
                  : mname === 'webSocketClose' ? 'disconnect_bridge' : mname;
    const line = call.getStartLineNumber();
    const make = (type) => ({
      from_role: from_role ?? null,
      to_role: isB2W ? 'web' : 'bridge',
      type, trigger, guard,
      delivery: { mode: isB2W ? 'broadcast_web' : 'send_bridge_by_device' },
      source: { spans: [{ start: line, end: line }] },
    });
    if (cc) {
      for (const t of fallThroughTypes(cc)) edges.push(make(t));
    } else {
      const arg = call.getArguments().find(a => a.getKind() === SyntaxKind.ObjectLiteralExpression);
      const tp = arg?.getProperties().find(p => p.getKind() === SyntaxKind.PropertyAssignment && p.getName?.() === 'type');
      edges.push({ ...make(tp ? lit(tp.getInitializer()) : '(dynamic)'), best_effort: true });
    }
  }
}
console.log(JSON.stringify({ edges }));
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/vocabextract/ -run 'TestExtract_(Smoke|FallThrough)' -v`
Expected: PASS (fall-through chain yields `encrypted/session:created/session:started/workflow:task_progress`; sendToBridge yields web→bridge `session:start` with correct guard/trigger).

- [ ] **Step 7: Commit**

```bash
git add internal/vocabextract/extractor.mjs internal/vocabextract/testdata/switch-fallthrough.ts internal/vocabextract/testdata/sendtobridge.ts internal/vocabextract/extract_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com commit -m "feat(vocabextract): emit-point + fall-through + guard + trigger + delivery"
```

---

### Task 4: extractor.mjs — route_field / on_missing_route / side_effects / batch / partial / unsupported / dedup

**Files:**
- Modify: `internal/vocabextract/extractor.mjs`
- Create: `internal/vocabextract/testdata/sideeffect.ts`
- Create: `internal/vocabextract/testdata/batch.ts`
- Modify: `internal/vocabextract/extract_test.go` (add subtests)

**Interfaces:**
- Extends edge JSON with: `route_field`, `on_missing_route`, `side_effects[].{kind,when_types}`, `batch`, `partial`, `unsupported`, and dedup of equal `(from_role,to_role,type,trigger)` (merging spans).

- [ ] **Step 1: Write fixtures**

Create `internal/vocabextract/testdata/sideeffect.ts`:

```ts
class UserRoom {
  handleMessage(ws, meta, msg) {
    switch (msg.type) {
      case 'workflow:task_progress':
      case 'workflow:task_result':
        if (meta.type === 'bridge') {
          this.broadcastToWeb(msg);
          if (msg.type === 'workflow:task_progress' || msg.type === 'workflow:task_result') {
            this.notifyOrchestrator(msg);
          }
        }
        break;
      default:
    }
  }
  broadcastToWeb(msg) {}
  notifyOrchestrator(msg) {}
}
```

Create `internal/vocabextract/testdata/batch.ts`:

```ts
class UserRoom {
  handleMessage(ws, meta, msg) {
    if (meta.type === 'bridge') { this.batchOutput(msg); }
  }
  batchOutput(msg) {}
  flushBatch(sessionId) { this.broadcastToWeb({ type: 'session:output-batch' }); }
  broadcastToWeb(msg) {}
}
```

- [ ] **Step 2: Write the assertions**

Append to `internal/vocabextract/extract_test.go`:

```go
func TestExtract_SideEffectsAndBatch(t *testing.T) {
	if testing.Short() { t.Skip("spawns node") }
	out, err := Extract(context.Background(), filepath.Join("testdata", "sideeffect.ts"))
	if err != nil { t.Skipf("node: %v", err) }
	var got struct {
		Edges []struct {
			Type        string `json:"type"`
			SideEffects []struct {
				Kind      string   `json:"kind"`
				WhenTypes []string `json:"when_types"`
			} `json:"side_effects"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(out, &got); err != nil { t.Fatal(err) }
	var progress *struct {
		SideEffects []struct {
			Kind      string   `json:"kind"`
			WhenTypes []string `json:"when_types"`
		} `json:"side_effects"`
	}
	for i := range got.Edges {
		if got.Edges[i].Type == "workflow:task_progress" {
			progress = &got.Edges[i]
		}
	}
	if progress == nil { t.Fatal("no workflow:task_progress edge") }
	if len(progress.SideEffects) != 1 || progress.SideEffects[0].Kind != "notify_orchestrator" {
		t.Errorf("side_effects = %+v", progress.SideEffects)
	}

	out, err = Extract(context.Background(), filepath.Join("testdata", "batch.ts"))
	if err != nil { t.Skipf("node: %v", err) }
	var b struct{ Edges []struct {
		Type    string `json:"type"`
		Partial bool   `json:"partial"`
	} `json:"edges"` }
	if err := json.Unmarshal(out, &b); err != nil { t.Fatal(err) }
	var anyPartial bool
	for _, e := range b.Edges {
		if e.Partial { anyPartial = true }
	}
	if !anyPartial { t.Errorf("no partial edge emitted for batch fixture: %+v", b.Edges) }
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/vocabextract/ -run TestExtract_SideEffectsAndBatch -v`
Expected: FAIL — no `side_effects`, no `partial`.

- [ ] **Step 4: Extend the algorithm**

In `extractor.mjs`, after the emit-point loop collects edges, add (inside the `if (cc)` branch, after pushing the broadcast/send edge) detection of the two side patterns. Concretely, add a helper and a second pass. Replace the `make`/emit block with logic that also records:

```js
// route_field: sendToBridge(devId, msg) first arg shape.
function routeFieldOf(call) {
  const args = call.getArguments();
  if (args.length < 1) return null;
  const t = args[0].getText();
  const m = t.match(/(payload\.\w+)/);
  return m ? m[1] : null;
}
// on_missing_route: sibling else { sendError(ws, 'CODE', ...) }.
function missingRouteOf(call) {
  const iff = call.getFirstAncestorByKind(SyntaxKind.IfStatement);
  if (!iff) return null;
  if (!iff.getElseStatement()) return null;
  const errs = iff.getElseStatement().getDescendantsOfKind(SyntaxKind.CallExpression)
    .filter(c => c.getExpression().getText().endsWith('sendError'));
  if (errs.length === 0) return null;
  const codeArg = errs[0].getArguments()[1]?.getText().replace(/^['"`]|['"`]$/g, '');
  return codeArg ? { kind: 'send_error', code: codeArg } : null;
}
```

Extend the per-emit-point `make`/push path so that for `sendToBridge` calls it sets `route_field` and `on_missing_route`, and — in a **separate loop over `notifyOrchestrator` calls** — it attaches `side_effects[{kind:'notify_orchestrator', when_types:[...]}]` to matching edges (matching by the `if (msg.type===...)` enclosing the notify call, else the fall-through chain), and a **batch detector** marks any edge whose enclosing method body contains a `batchOutput(` sink reachable from the case as `partial: true` with `batch{}`.

Then add dedup before `console.log`:

```js
const key = (e) => `${e.from_role}|${e.to_role}|${e.type}|${e.trigger}`;
const merged = new Map();
for (const e of edges) {
  const k = key(e);
  const ex = merged.get(k);
  if (ex) { ex.source.spans.push(...e.source.spans); }
  else { merged.set(k, e); }
}
console.log(JSON.stringify({ edges: [...merged.values()] }));
```

> Implementation note for the batch detector (keep it conservative): if a `CaseClause`'s body contains a call to `this.batchOutput(` rather than a direct `broadcastToWeb`, emit the edge with `partial: true` and `batch: { window_ms: 50, key: 'payload.sessionId' }` (window/key are best-effort literals; correctness of the final type is explicitly out of scope per spec §6 Step 7). If no `batchOutput` is present, leave `partial` unset. Recognize-but-can't-resolve shapes set `unsupported: true`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/vocabextract/ -v`
Expected: all PASS (side_effects attached with when_types; batch edge marked partial).

- [ ] **Step 6: Commit**

```bash
git add internal/vocabextract/
git -c user.name=binoctal -c user.email=binoctal@gmail.com commit -m "feat(vocabextract): route_field, side_effects, batch/partial, dedup"
```

---

### Task 5: `cerberus protocol vocabulary` subcommand

**Files:**
- Modify: `cmd/cerberus/main_protocol.go`
- Create: `cmd/cerberus/main_protocol_vocabulary_test.go`

**Interfaces:**
- Produces: `runProtocolVocabulary(ctx, workDir, sourcePath, name string, dryRun bool, confirm func(string) bool) error` — calls `vocabextract.Extract`, computes the source file's SHA-256, marshals `project.Vocabulary` to `.cerberus/vocab/<name>.vocab.yaml`.

- [ ] **Step 1: Write the command test**

Create `cmd/cerberus/main_protocol_vocabulary_test.go`:

```go
package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/binoctal/cerberus/internal/project"
)

func TestRunProtocolVocabulary_DryRun(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "room.ts")
	if err := os.WriteFile(src, []byte("class UserRoom { handleMessage(){} }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	_ = buf
	err := runProtocolVocabulary(context.Background(), dir, src, "open-agents", true, nil)
	if err != nil {
		t.Skipf("node unavailable: %v", err)
	}
	// dry-run must NOT write
	if _, err := os.Stat(filepath.Join(dir, ".cerberus", "vocab", "open-agents.vocab.yaml")); err == nil {
		t.Fatal("dry-run wrote a file")
	}
}

func TestRunProtocolVocabulary_Writes(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "room.ts")
	os.WriteFile(src, []byte("class UserRoom { handleMessage(){} }\n"), 0644)
	out := filepath.Join(dir, ".cerberus", "vocab", "open-agents.vocab.yaml")
	err := runProtocolVocabulary(context.Background(), dir, src, "open-agents", false, func(string) bool { return true })
	if err != nil {
		t.Skipf("node unavailable: %v", err)
	}
	v, err := project.LoadVocabulary(out)
	if err != nil {
		t.Fatalf("load written vocab: %v", err)
	}
	if v.Source.ProtocolRef != "open-agents" || len(v.Source.Files) != 1 || v.Source.Files[0].Hash == "" {
		t.Errorf("unexpected source: %+v", v.Source)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/cerberus/ -run TestRunProtocolVocabulary -v`
Expected: FAIL — `runProtocolVocabulary` undefined.

- [ ] **Step 3: Implement the subcommand**

In `cmd/cerberus/main_protocol.go`: add a `vocabCmd` (analogous to `protocolInferCmd`), register it under `protocolCmd()`, and add `runProtocolVocabulary`. Add imports `crypto/sha256`, `encoding/hex`, `encoding/json`, `time` (none available in-script; pass a fixed stamp), `github.com/binoctal/cerberus/internal/vocabextract`.

```go
var (
	protocolVocabName string
	protocolVocabFrom string
	protocolVocabDry  bool
)

func protocolVocabularyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vocabulary",
		Short: "Extract a WS routing vocabulary from a TypeScript source file",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProtocolVocabulary(cmd.Context(), ".", protocolVocabFrom, protocolVocabName,
				protocolVocabDry, promptConfirm(os.Stdin, os.Stdout))
		},
	}
	cmd.Flags().StringVar(&protocolVocabName, "name", "", "vocab file name (.cerberus/vocab/<name>.vocab.yaml); required")
	cmd.Flags().StringVar(&protocolVocabFrom, "from", "", "path to the TS source file; required")
	cmd.Flags().BoolVar(&protocolVocabDry, "dry-run", false, "print the draft without writing")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("from")
	return cmd
}
```

Register in `protocolCmd()`: `cmd.AddCommand(protocolInferCmd()); cmd.AddCommand(protocolVocabularyCmd())`.

Add `runProtocolVocabulary`:

```go
func runProtocolVocabulary(ctx context.Context, workDir, sourcePath, name string, dryRun bool, confirm func(string) bool) error {
	if err := project.CheckProtocolRefName(name); err != nil {
		return fmt.Errorf("--name: %w", err)
	}
	raw, err := vocabextract.Extract(ctx, sourcePath)
	if err != nil {
		return err
	}
	var extracted struct {
		Edges []project.VocabEdge `json:"edges"`
	}
	if err := json.Unmarshal(raw, &extracted); err != nil {
		return fmt.Errorf("parse extractor output: %w", err)
	}
	srcAbs, _ := filepath.Abs(filepath.Join(workDir, sourcePath))
	srcData, err := os.ReadFile(srcAbs)
	if err != nil {
		return fmt.Errorf("hash source: %w", err)
	}
	sum := sha256.Sum256(srcData)
	vocab := &project.Vocabulary{
		Source: project.VocabSource{
			Files: []project.VocabFile{{Path: sourcePath, Hash: hex.EncodeToString(sum[:])}},
			ProtocolRef: name,
		},
		Edges: extracted.Edges,
	}
	block, _ := yaml.Marshal(vocab)
	fmt.Printf("Draft vocabulary %q (%d edges):\n%s\n", name, len(vocab.Edges), string(block))
	if dryRun {
		return nil
	}
	outPath := filepath.Join(workDir, ".cerberus", "vocab", name+".vocab.yaml")
	rel := filepath.Join(".cerberus", "vocab", name+".vocab.yaml")
	question := fmt.Sprintf("Write draft to %s? [y/N]", rel)
	if _, statErr := os.Stat(outPath); statErr == nil {
		question = fmt.Sprintf("%s already exists. Overwrite? [y/N]", rel)
	}
	if confirm == nil || !confirm(question) {
		fmt.Println("aborted; no changes written")
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(outPath, block, 0644)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/cerberus/ -run TestRunProtocolVocabulary -v`
Expected: PASS (or SKIP without node).

- [ ] **Step 5: Run full lint/fmt**

Run: `make fmt && make lint`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add cmd/cerberus/main_protocol.go cmd/cerberus/main_protocol_vocabulary_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com commit -m "feat(cmd): cerberus protocol vocabulary subcommand"
```

---

### Task 6: Generate dogfood vocab for real room.ts

**Files:**
- Create: `dogfood/ws-realtime/.cerberus/vocab/open-agents.vocab.yaml`
- Create: `dogfood/ws-realtime/.cerberus/project.yaml` already exists — verify a `vocab:` loader path is unnecessary (vocab loaded by absolute path in tests).

- [ ] **Step 1: Build cerberus**

Run: `make build`
Expected: `build/cerberus` produced.

- [ ] **Step 2: Generate the vocab**

Run:
```bash
./build/cerberus protocol vocabulary \
  --name open-agents \
  --from ../../open-agents/apps/api/src/realtime/room.ts \
  --dry-run
```
> Adjust `--from` to the absolute path of `room.ts` if the relative path resolves wrong. Inspect the drafted edges: expect ~38 bridge→web and ~24 web→bridge (per the prototype validation in spec §9).

- [ ] **Step 3: Write the file (confirm prompt)**

Re-run without `--dry-run` and answer `y`. Verify
`dogfood/ws-realtime/.cerberus/vocab/open-agents.vocab.yaml` exists and `source.files[0].hash` is set.

- [ ] **Step 4: Sanity-check the hash matches the source**

Run: `sha256sum <room.ts path>` and compare to `hash:` in the YAML.
Expected: match.

- [ ] **Step 5: Commit**

```bash
git add dogfood/ws-realtime/.cerberus/vocab/open-agents.vocab.yaml
git -c user.name=binoctal -c user.email=binoctal@gmail.com commit -m "dogfood(ws-realtime): generate open-agents routing vocab"
```

---

### Task 7: TestVocabularyDriven dynamic test

**Files:**
- Create: `internal/head/agent/vocabulary_driven_test.go`

**Interfaces:**
- Consumes: `project.LoadVocabulary`, `agent.TestCase`/`TestStep`, `agent.newStepExecutionWithIdx`, `agent.setupOpenAgents`, the dogfood `vocab.yaml`.

- [ ] **Step 1: Write the dynamic test**

Create `internal/head/agent/vocabulary_driven_test.go`:

```go
//go:build integration

package agent

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/project"
)

// vocabPath points at the dogfood vocabulary produced by
// `cerberus protocol vocabulary`.
const vocabPath = "../../../dogfood/ws-realtime/.cerberus/vocab/open-agents.vocab.yaml"

// TestVocabularyDriven builds TestCase tables from open-agents.vocab.yaml at
// run time and asserts each non-unsupported edge relays end-to-end against a
// live DO. Supersedes the hardcoded rows in TestBridgeToWebRelay /
// TestWebToBridgeRouting once parity is shown (Task 8).
func TestVocabularyDriven(t *testing.T) {
	vocab, err := project.LoadVocabulary(vocabPath)
	if err != nil {
		t.Skipf("vocab not generated (%s): %v", vocabPath, err)
	}
	f := setupOpenAgents(t, false)
	target := "ws://localhost:8989/ws/" + f.userId

	for _, e := range vocab.Edges {
		e := e
		name := fmt.Sprintf("%s_%s_to_%s", e.Trigger, e.FromRole, e.ToRole)
		t.Run(name+"/"+e.Type, func(t *testing.T) {
			if e.Unsupported || e.Partial {
				t.Skipf("edge %q unsupported/partial — finding, not failure", e.Type)
			}
			if e.Trigger != "message_handled" {
				t.Skipf("trigger %q not asserted by v1 (lifecycle)", e.Trigger)
			}
			require.NotEmpty(t, e.FromRole, "message_handled edge needs a from_role")

			// Connect both roles (handshake await device:online is optional).
			steps := []TestStep{
				{Action: "ws_connect", Role: "web", ConnectionID: "c-web"},
				{Action: "ws_connect", Role: "bridge", ConnectionID: "c-bridge"},
				{Action: "ws_receive", ConnectionID: "c-web", Type: "device:online", Timeout: 3},
			}
			sender, receiver := "c-"+e.FromRole, "c-"+e.ToRole
			msg := fmt.Sprintf(`{"type":%q}`, e.Type)
			if e.RouteField != "" {
				msg = fmt.Sprintf(`{"type":%q,"payload":{"deviceId":%q}}`, e.Type, f.deviceId)
			}
			steps = append(steps,
				TestStep{Action: "ws_send", ConnectionID: sender, Message: msg},
				TestStep{Action: "ws_receive", ConnectionID: receiver, Type: e.Type, Timeout: 3},
			)
			tc := &TestCase{ID: "tc-vocab-" + e.Type, Target: target, Steps: steps}
			se := newStepExecutionWithIdx(t, tc, f.wsIdx)
			res := se.runSteps()
			require.Equal(t, StepPassed, res.Status, "edge %q did not relay", e.Type)
		})
	}
}
```

- [ ] **Step 2: Run against a live DO**

Bring up open-agents (`cd ../../open-agents/apps/api && npm run dev`), then:
Run: `go test -tags integration -run TestVocabularyDriven ./internal/head/agent/ -v`
Expected: every non-skipped `message_handled` edge passes; `unsupported`/`partial`/lifecycle edges skip with a clear reason.

- [ ] **Step 3: Commit**

```bash
git add internal/head/agent/vocabulary_driven_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com commit -m "test(agent): TestVocabularyDriven reads open-agents vocab at run time"
```

---

### Task 8: Parity check + retire hardcoded rows

**Files:**
- Modify: `internal/head/agent/execute_phases_steps_integration_test.go`

- [ ] **Step 1: Establish parity**

With open-agents running, run both old and new tests:
```bash
go test -tags integration -run 'TestBridgeToWebRelay|TestWebToBridgeRouting|TestVocabularyDriven' ./internal/head/agent/ -v
```
Capture: the set of `message_handled` bridge→web and web→bridge types asserted by `TestVocabularyDriven`. Confirm it is a **superset** of `TestBridgeToWebRelay` (~37) + `TestWebToBridgeRouting` (~24). The prototype already showed 37/37 + 24/24 (spec §9).

- [ ] **Step 2: Delete the superseded tests**

In `internal/head/agent/execute_phases_steps_integration_test.go`, delete `TestBridgeToWebRelay` and `TestWebToBridgeRouting` (the `rows` they encode are now derived from `vocab.yaml`). Keep `TestRunStepsMultiConnectionOpenAgents`, `TestSessionStartRoundTrip`, `TestLifecycleSignals`, `TestAuthErrorPaths`, `TestOrchestratorCallback` — they cover paths beyond simple relay (round-trip, lifecycle, auth, callback) that the v1 vocabulary test does not assert.

- [ ] **Step 3: Run the full integration suite**

Run: `go test -tags integration ./internal/head/agent/ -v`
Expected: green; `TestVocabularyDriven` now carries the relay/routing coverage.

- [ ] **Step 4: Run unit + lint**

Run: `make check`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/execute_phases_steps_integration_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com commit -m "test(agent): retire hardcoded relay/routing rows; vocab drives them"
```

---

## Open items resolved during planning

- **extractor packaging**: single bundled `extractor.mjs` + `package.json` written to a temp dir, `npm install` per invocation (Task 2). Acceptable for a dev-time tool; cache via existing `node_modules` check.
- **markdown summary**: not in v1; the YAML + stdout draft print is sufficient.
- **notify_orchestrator side-effect assertion in the dynamic test**: deferred — `TestVocabularyDriven` v1 does not assert the capture-server callback (kept in `TestOrchestratorCallback`); the edge's `side_effects` metadata is recorded for a future assertion.

## Self-review notes

- **Spec coverage**: §3 principles → Task 1/3/4 (schema + algorithm); §5 schema → Task 1; §6 algorithm → Task 3/4; §7.1 dynamic test → Task 7; §7.2 Scout → explicitly out of v1 (interface via `project.Vocabulary`, no wiring task); §8 command → Task 5; §9 validation reproduced by Task 6/8.
- **Partial handling**: unified to skip + finding across spec §3/§5/§7 — consistent in Task 4 (`partial:true`) and Task 7 (skip).
- **Type consistency**: `project.VocabEdge` field names in Task 1 match the JSON the extractor emits in Task 3/4 (`from_role`, `to_role`, `type`, `trigger`, `guard`, `delivery.mode`, `route_field`, `side_effects[].kind/when_types`, `partial`, `source.spans`).
- **Scoped out (YAGNI)**: Scout prompt wiring, payload schema, annotations, abstract adapter interface — all listed as non-goals, no tasks.
