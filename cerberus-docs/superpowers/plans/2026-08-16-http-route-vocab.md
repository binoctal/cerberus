# HTTP Route Vocabulary Extraction & Coverage Attribution — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract Hono HTTP routes into the vocabulary (`http_routes`) and credit them in path coverage via a synthesized required edge per route.

**Architecture:** New `VocabHTTPRoute` list on `Vocabulary` (separate from WS `VocabEdge`s). The ts-morph extractor gains a module-level HTTP pass that walks `app.route()` mounts with Node-style import resolution. Coverage synthesizes one `VocabEdge` per non-exempt route in `requiredEdges()` and credits exercise from structured `http_request` evidence fields.

**Tech Stack:** Go 1.25 (`internal/project`, `internal/session`, `internal/head/agent`, `cmd/cerberus`), ts-morph (extractor.mjs), YAML/JSON dual tags.

**Spec:** `cerberus-docs/superpowers/specs/2026-08-16-http-route-vocab-design.md`

## Global Constraints

- Commit author: `binoctal <binoctal@gmail.com>`, NO Co-Authored-By lines.
- Comments and commit messages in English; code follows existing density/idiom.
- Node-spawning tests use the existing pattern: `t.Skip` on `testing.Short()`, `t.Skipf` when node/npm unavailable.
- `make check` (fmt + lint + test) must pass at each task's commit.
- All documents go under `cerberus-docs/`, never `docs/`.

---

### Task 1: `VocabHTTPRoute` schema + `ValidateVocabulary`

**Files:**
- Modify: `internal/project/vocabulary.go`
- Test: `internal/project/vocabulary_test.go`

**Interfaces:**
- Produces: `Vocabulary.HTTPRoutes []VocabHTTPRoute` (yaml+json tags); `VocabHTTPRoute{Method, Path, Mount string; Partial, Unsupported bool; Source VocabEdgeSource}`; `ValidateVocabulary(v *Vocabulary) error`; `LoadVocabulary` calls it.

- [ ] **Step 1: Write the failing test** — append to `internal/project/vocabulary_test.go`:

```go
func TestValidateVocabulary_HTTPRoutes(t *testing.T) {
	ok := &Vocabulary{HTTPRoutes: []VocabHTTPRoute{
		{Method: "POST", Path: "/api/sessions"},
		{Method: "GET", Path: "/api/sessions/:id"},
		{Method: "ALL", Path: "/api/workflows/jobs/*"},
	}}
	if err := ValidateVocabulary(ok); err != nil {
		t.Fatalf("valid routes rejected: %v", err)
	}
	bad := []VocabHTTPRoute{
		{Method: "FETCH", Path: "/x"},   // method not in enum
		{Method: "GET", Path: "x"},      // no leading slash
		{Method: "GET", Path: "/a//b"},  // double slash
		{Method: "GET", Path: "/a/*/b"}, // * must be the final segment
		{Method: "GET", Path: "/a/:/b"}, // empty param name
	}
	for _, r := range bad {
		if err := ValidateVocabulary(&Vocabulary{HTTPRoutes: []VocabHTTPRoute{r}}); err == nil {
			t.Errorf("route %+v: want validation error, got nil", r)
		}
	}
}

func TestLoadVocabulary_RejectsBadRoute(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "v.vocab.yaml")
	body := "source:\n  files: []\nhttp_routes:\n  - method: FETCH\n    path: /x\n"
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadVocabulary(p); err == nil {
		t.Fatal("LoadVocabulary accepted an invalid http_route (broken denominator must not pass silently)")
	}
}
```

Check the file's existing imports (`os`, `path/filepath`, `testing` are likely present; add if missing).

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/project/ -run 'TestValidateVocabulary_HTTPRoutes|TestLoadVocabulary_RejectsBadRoute' -v`
Expected: FAIL — `undefined: VocabHTTPRoute` / `undefined: ValidateVocabulary`.

- [ ] **Step 3: Implement** — in `internal/project/vocabulary.go`:

Add field to `Vocabulary` after `Edges`:

```go
	// HTTPRoutes is the mounted HTTP route surface (Hono-extracted). Kept
	// separate from Edges: WS delivery semantics do not apply; coverage
	// synthesizes one edge per route in requiredEdges (http_trigger pattern).
	HTTPRoutes []VocabHTTPRoute `yaml:"http_routes,omitempty" json:"http_routes,omitempty"`
```

Add after the `VocabEdge` declaration block:

```go
// VocabHTTPRoute is one mounted HTTP route. Identity is METHOD|Path. Path is
// the full normalized pattern (mount chain + route path); :param matches one
// segment, a trailing * matches one-or-more, ALL matches any method.
type VocabHTTPRoute struct {
	Method      string          `yaml:"method" json:"method"`
	Path        string          `yaml:"path" json:"path"`
	Mount       string          `yaml:"mount,omitempty" json:"mount,omitempty"`
	Partial     bool            `yaml:"partial,omitempty" json:"partial,omitempty"`
	Unsupported bool            `yaml:"unsupported,omitempty" json:"unsupported,omitempty"`
	Source      VocabEdgeSource `yaml:"source" json:"source"`
}

// vocabRouteMethods is the closed method enum (ALL = Hono app.all).
var vocabRouteMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "DELETE": true,
	"PATCH": true, "OPTIONS": true, "HEAD": true, "ALL": true,
}

// ValidateVocabulary checks the HTTP route surface so a broken denominator
// cannot pass silently (same principle as claims).
func ValidateVocabulary(v *Vocabulary) error {
	for i, r := range v.HTTPRoutes {
		if !vocabRouteMethods[r.Method] {
			return fmt.Errorf("http_routes[%d]: method %q not in enum", i, r.Method)
		}
		if !strings.HasPrefix(r.Path, "/") || strings.Contains(r.Path, "//") {
			return fmt.Errorf("http_routes[%d]: path %q must start with / and contain no empty segments", i, r.Path)
		}
		segs := strings.Split(strings.Trim(r.Path, "/"), "/")
		for j, s := range segs {
			if strings.Contains(s, "*") && (s != "*" || j != len(segs)-1) {
				return fmt.Errorf("http_routes[%d]: * must be the lone final segment in %q", i, r.Path)
			}
			if s == ":" || (strings.HasPrefix(s, ":") && len(s) == 1) {
				return fmt.Errorf("http_routes[%d]: empty param name in %q", i, r.Path)
			}
		}
	}
	return nil
}
```

In `LoadVocabulary`, after unmarshal: `if err := ValidateVocabulary(&v); err != nil { return nil, fmt.Errorf("vocab: %s: %w", path, err) }`. Add `"strings"` to imports if missing.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/project/ -run 'TestValidateVocabulary_HTTPRoutes|TestLoadVocabulary_RejectsBadRoute' -v`
Expected: PASS. Then `go test ./internal/project/` — existing tests unaffected (no fixture carries http_routes).

- [ ] **Step 5: Commit**

```bash
git add internal/project/vocabulary.go internal/project/vocabulary_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com commit -m "feat(project): VocabHTTPRoute schema + ValidateVocabulary (method enum, path shape)"
```

---

### Task 2: Extractor HTTP pass (Hono) + fixture

**Files:**
- Modify: `internal/vocabextract/extractor.mjs`
- Create: `internal/vocabextract/testdata/hono/worker.ts`, `testdata/hono/routes/things.ts`, `testdata/hono/routes/nested/index.ts`, `testdata/hono/routes/unmounted.ts`
- Test: `internal/vocabextract/extract_test.go`

**Interfaces:**
- Produces: extractor stdout JSON gains `http_routes: [{method, path, mount, source}]`, `files: [{path}]`, `skipped_on_registrations: number`. WS `edges` output unchanged. Existing `edges`-only consumers ignore the new fields.

- [ ] **Step 1: Create the fixture**

`internal/vocabextract/testdata/hono/worker.ts`:
```ts
import { Hono } from 'hono';
import { thingRoutes } from './routes/things';
import { helper } from './routes/unmounted';

const app = new Hono();

app.get('/health', (c) => c.json({ ok: true }));
app.post('/api/dev/setup', strictRateLimit, (c) => c.json({}));
app.post('/api/dev/setup', (c) => c.json({}));
app.route('/api/things', thingRoutes);

export default app;
```

`testdata/hono/routes/things.ts`:
```ts
import { Hono } from 'hono';
import { nestedRoutes } from './nested';

const app = new Hono();

app.get('/', (c) => c.json({}));
app.get('/:id', (c) => c.json({}));
app.on('GET', '/multi', (c) => c.json({}));
app.route('/nested', nestedRoutes);

export { app as thingRoutes };
```

`testdata/hono/routes/nested/index.ts`:
```ts
import { Hono } from 'hono';

const app = new Hono();

app.delete('/jobs/*', (c) => c.json({}));

export default app;
```

`testdata/hono/routes/unmounted.ts` (imported but never `app.route`d — must NOT appear):
```ts
import { Hono } from 'hono';

const app = new Hono();

app.put('/secret', (c) => c.json({}));

export const helper = app;
```

- [ ] **Step 2: Write the failing test** — append to `internal/vocabextract/extract_test.go`:

```go
func TestExtract_HonoRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns node")
	}
	out, err := Extract(context.Background(), filepath.Join("testdata", "hono", "worker.ts"))
	if err != nil {
		t.Skipf("node unavailable or npm failed: %v", err)
	}
	var got struct {
		HTTPRoutes []struct {
			Method string `json:"method"`
			Path   string `json:"path"`
			Mount  string `json:"mount"`
		} `json:"http_routes"`
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
		SkippedOn int `json:"skipped_on_registrations"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("parse extractor stdout: %v\nraw=%s", err, out)
	}
	seen := map[string]int{}
	for _, r := range got.HTTPRoutes {
		seen[r.Method+" "+r.Path]++
	}
	for _, w := range []string{
		"GET /health",
		"POST /api/dev/setup",
		"GET /api/things",
		"GET /api/things/:id",
		"DELETE /api/things/nested/jobs/*",
	} {
		if seen[w] == 0 {
			t.Errorf("route %q missing; got %+v", w, got.HTTPRoutes)
		}
	}
	if seen["POST /api/dev/setup"] != 1 {
		t.Errorf("duplicate registration must merge to one entry, got %d", seen["POST /api/dev/setup"])
	}
	if seen["PUT /secret"] != 0 {
		t.Error("unmounted route leaked into http_routes")
	}
	if seen["GET /multi"] != 0 {
		t.Error("app.on registration extracted despite v1 skip")
	}
	if got.SkippedOn != 1 {
		t.Errorf("skipped_on_registrations = %d, want 1", got.SkippedOn)
	}
	fileSet := map[string]bool{}
	for _, f := range got.Files {
		fileSet[f.Path] = true
	}
	for _, w := range []string{"worker.ts", "things.ts", "nested"} {
		found := false
		for p := range fileSet {
			if strings.Contains(filepath.ToSlash(p), w) {
				found = true
			}
		}
		if !found {
			t.Errorf("traversed file %q missing from files output: %+v", w, got.Files)
		}
	}
	for p := range fileSet {
		if strings.Contains(p, "unmounted") {
			t.Errorf("unmounted file %q must not be traversed", p)
		}
	}
}
```

Add `"strings"` to the test file imports if missing.

- [ ] **Step 3: Run to verify failure**

Run: `go test ./internal/vocabextract/ -run TestExtract_HonoRoutes -v`
Expected: FAIL — `http_routes` empty (no routes extracted yet).

- [ ] **Step 4: Implement the HTTP pass** — in `internal/vocabextract/extractor.mjs`:

Top of file, after the ts-morph import:
```js
import { Project, SyntaxKind } from 'ts-morph';
import path from 'node:path';
import fs from 'node:fs';
```

Restructure the WS section: wrap the two `for (const method of cls.getMethods())` loops' body access so a classless file does not crash. Change `const cls = sf.getClasses()[0];` to:
```js
const cls = sf.getClasses()[0];
const edges = [];
if (cls) {
  for (const method of cls.getMethods()) {
    // ... (existing broadcastToWeb/sendToBridge loop body, unchanged)
  }
  // ... (existing notifyOrchestrator loop, unchanged)
  // ... (existing batchOutput loop, unchanged)
}
```
(Move `const edges = [];` above the guard; the three loops keep their bodies verbatim, just indented one level under `if (cls) {`.)

After the dedup block, before the final `console.log`, add the HTTP pass:

```js
// ── HTTP route extraction (Hono) ────────────────────────────────
// Walks app.<method>('path') and app.route('/prefix', router) at module
// level, following relative imports with Node resolution semantics
// (exact file beats directory: base, base.ts, base/index.ts). Only mounted
// routers are traversed; an imported-but-unmounted router never appears.
const HTTP_METHODS = ['get', 'post', 'put', 'delete', 'patch', 'options', 'head', 'all'];
const httpRoutes = [];
const routeKey = (r) => `${r.method}|${r.path}`;
const routeMap = new Map();
const traversed = [file];
const visitedFiles = new Set();
let skippedOn = 0;

function joinPath(prefix, p) {
  const a = prefix.replace(/\/+$/, '');
  const b = p.replace(/^\/+/, '');
  if (!b) return a || '/';
  if (!a) return '/' + b;
  return a + '/' + b;
}

function resolveSpecifier(fromFile, spec) {
  if (!spec.startsWith('.')) return null;
  const base = path.resolve(path.dirname(fromFile), spec);
  for (const cand of [base, base + '.ts', path.join(base, 'index.ts')]) {
    if (fs.existsSync(cand) && fs.statSync(cand).isFile()) return cand;
  }
  return null;
}

// importName → resolved source path for relative imports (default, named, ns).
function importMap(sf) {
  const m = new Map();
  for (const imp of sf.getImportDeclarations()) {
    const src = resolveSpecifier(sf.getFilePath(), imp.getModuleSpecifierValue());
    if (!src) continue;
    const def = imp.getDefaultImport();
    if (def) m.set(def.getText(), src);
    for (const n of imp.getNamedImports()) m.set(n.getName(), src);
    const ns = imp.getNamespaceImport();
    if (ns) m.set(ns.getText(), src);
  }
  return m;
}

function addRoute(method, fullPath, mount, filePath, line) {
  const e = { method, path: fullPath, mount: mount || undefined,
              source: { spans: [{ start: line, end: line }] } };
  const k = routeKey(e);
  const ex = routeMap.get(k);
  if (ex) { ex.source.spans.push(...e.source.spans); return; }
  routeMap.set(k, e);
  httpRoutes.push(e);
}

function walkFile(filePath, prefix, depth) {
  const abs = path.resolve(filePath);
  if (depth > 8 || visitedFiles.has(abs)) return;
  visitedFiles.add(abs);
  let sf2;
  try { sf2 = project.addSourceFileAtPath(abs); } catch { return; }
  const honoVars = new Set();
  for (const d of sf2.getVariableDeclarations()) {
    const init = d.getInitializer();
    if (init?.getKind() === SyntaxKind.NewExpression && init.getExpression().getText() === 'Hono') {
      honoVars.add(d.getName());
    }
  }
  const imports = importMap(sf2);
  for (const stmt of sf2.getStatements()) {
    if (stmt.getKind() !== SyntaxKind.ExpressionStatement) continue;
    const call = stmt.getExpression();
    if (call.getKind() !== SyntaxKind.CallExpression) continue;
    const prop = call.getExpression();
    if (prop.getKind() !== SyntaxKind.PropertyAccessExpression) continue;
    if (!honoVars.has(prop.getExpression().getText())) continue;
    const name = prop.getName();
    const arg0 = call.getArguments()[0];
    const lit0 = arg0 && arg0.getKind() === SyntaxKind.StringLiteral ? lit(arg0) : null;
    if (HTTP_METHODS.includes(name) && lit0 !== null) {
      addRoute(name.toUpperCase(), joinPath(prefix, lit0), prefix, abs, call.getStartLineNumber());
    } else if (name === 'route' && lit0 !== null) {
      const target = imports.get(call.getArguments()[1]?.getText().trim());
      if (target) { traversed.push(target); walkFile(target, joinPath(prefix, lit0), depth + 1); }
    } else if (name === 'on') {
      skippedOn++;
    }
  }
}
walkFile(file, '', 0);

console.log(JSON.stringify({
  edges: [...merged.values()],
  http_routes: httpRoutes,
  files: traversed.map((p) => ({ path: p })),
  skipped_on_registrations: skippedOn,
}));
```

Replace the old final `console.log(JSON.stringify({ edges: [...merged.values()] }));` with the block above (it is included there).

- [ ] **Step 5: Run to verify pass**

Run: `go test ./internal/vocabextract/ -v`
Expected: PASS including the pre-existing WS tests (`TestExtract_Smoke`, `TestExtract_FallThrough`, …) — the classless-`stub` fixtures still produce edges; if any old fixture had no class and relied on `[0]` being undefined, the new guard fixes rather than breaks it.

- [ ] **Step 6: Commit**

```bash
git add internal/vocabextract/extractor.mjs internal/vocabextract/extract_test.go internal/vocabextract/testdata/hono/
git -c user.name=binoctal -c user.email=binoctal@gmail.com commit -m "feat(vocabextract): Hono HTTP route pass — mount-aware traversal, span merge, app.on skip count"
```

---

### Task 3: CLI wiring — http_routes, multi-file provenance, marks merge

**Files:**
- Modify: `cmd/cerberus/main_protocol.go:71-118` (`runProtocolVocabulary`) and the `protocolVocabularyCmd` Short string
- Test: `cmd/cerberus/main_protocol_vocabulary_test.go`

**Interfaces:**
- Consumes: extractor JSON `http_routes`/`files` (Task 2), `VocabHTTPRoute` (Task 1).
- Produces: written vocab yaml carries `http_routes` + per-file hashes; re-extraction preserves route marks keyed `method|path`.

- [ ] **Step 1: Write the failing test** — append to `cmd/cerberus/main_protocol_vocabulary_test.go`:

```go
// TestRunProtocolVocabulary_HonoRoutes: extraction over a Hono entry writes
// http_routes with per-file hashes, and re-extraction preserves route marks
// (method|path) the same way WS edge marks survive.
func TestRunProtocolVocabulary_HonoRoutes(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "routes"), 0755); err != nil {
		t.Fatal(err)
	}
	worker := `import { Hono } from 'hono';
import { thingRoutes } from './routes/things';
const app = new Hono();
app.get('/health', (c) => c.json({}));
app.route('/api/things', thingRoutes);
export default app;
`
	things := `import { Hono } from 'hono';
const app = new Hono();
app.post('/', (c) => c.json({}));
export { app as thingRoutes };
`
	if err := os.WriteFile(filepath.Join(dir, "worker.ts"), []byte(worker), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "routes", "things.ts"), []byte(things), 0644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, ".cerberus", "vocab", "hono.vocab.yaml")
	if err := runProtocolVocabulary(context.Background(), dir, filepath.Join(dir, "worker.ts"), "hono", false, func(string) bool { return true }); err != nil {
		t.Skipf("node unavailable: %v", err)
	}
	v, err := project.LoadVocabulary(out)
	if err != nil {
		t.Fatalf("load vocab: %v", err)
	}
	if len(v.HTTPRoutes) != 2 {
		t.Fatalf("http_routes = %+v, want 2 routes", v.HTTPRoutes)
	}
	if len(v.Source.Files) != 2 {
		t.Fatalf("source.files = %+v, want worker+things", v.Source.Files)
	}
	for _, f := range v.Source.Files {
		if f.Hash == "" {
			t.Errorf("file %q has no hash", f.Path)
		}
	}
	// Annotate POST as partial, re-extract, mark must survive.
	for i := range v.HTTPRoutes {
		if v.HTTPRoutes[i].Method == "POST" {
			v.HTTPRoutes[i].Partial = true
		}
	}
	block, err := yaml.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, block, 0644); err != nil {
		t.Fatal(err)
	}
	if err := runProtocolVocabulary(context.Background(), dir, filepath.Join(dir, "worker.ts"), "hono", false, func(string) bool { return true }); err != nil {
		t.Skipf("node unavailable: %v", err)
	}
	v2, err := project.LoadVocabulary(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range v2.HTTPRoutes {
		if r.Method == "POST" && !r.Partial {
			t.Fatal("re-extraction dropped partial mark on POST route")
		}
		if r.Method == "GET" && r.Partial {
			t.Fatal("GET route unexpectedly marked partial")
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./cmd/cerberus/ -run TestRunProtocolVocabulary_HonoRoutes -v`
Expected: FAIL — `http_routes` empty (`want 2 routes`) or files count 1.

- [ ] **Step 3: Implement** — in `runProtocolVocabulary`:

Replace the unmarshal struct (lines ~79-84) with:
```go
	var extracted struct {
		Edges      []project.VocabEdge `json:"edges"`
		HTTPRoutes []project.VocabHTTPRoute `json:"http_routes"`
		Files      []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
```

Replace the single-file hash block (lines ~85-93) with multi-file hashing over reported files, falling back to the entry:
```go
	paths := make([]string, 0, len(extracted.Files)+1)
	for _, f := range extracted.Files {
		paths = append(paths, f.Path)
	}
	if len(paths) == 0 {
		paths = append(paths, sourcePath)
	}
	var files []project.VocabFile
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("hash source %s: %w", p, err)
		}
		sum := sha256.Sum256(data)
		files = append(files, project.VocabFile{Path: p, Hash: hex.EncodeToString(sum[:])})
	}
```
Note: extractor reports absolute paths; hash them directly (do not join workDir). Construct the `Vocabulary` with `Files: files` and `HTTPRoutes: extracted.HTTPRoutes`.

Extend the marks-merge block inside the `if prev, perr := ...` guard with:
```go
		routeMarks := make(map[string]project.VocabHTTPRoute, len(prev.HTTPRoutes))
		for _, r := range prev.HTTPRoutes {
			routeMarks[r.Method+"|"+r.Path] = r
		}
		for i := range vocab.HTTPRoutes {
			if old, ok := routeMarks[vocab.HTTPRoutes[i].Method+"|"+vocab.HTTPRoutes[i].Path]; ok {
				vocab.HTTPRoutes[i].Partial = old.Partial
				vocab.HTTPRoutes[i].Unsupported = old.Unsupported
			}
		}
```

Update the draft print to include routes: `fmt.Printf("Draft vocabulary %q (%d edges, %d http routes):\n%s\n", name, len(vocab.Edges), len(vocab.HTTPRoutes), string(block))`.

Update the cobra `Short` in `protocolVocabularyCmd` to `"Extract a WS routing vocabulary and HTTP route surface from a TypeScript source file"`.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./cmd/cerberus/ -run TestRunProtocolVocabulary -v`
Expected: PASS (all four vocabulary tests; `Writes` still sees 1 file for the classless non-Hono entry).

- [ ] **Step 5: Commit**

```bash
git add cmd/cerberus/main_protocol.go cmd/cerberus/main_protocol_vocabulary_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com commit -m "feat(cli): protocol vocabulary writes http_routes + multi-file hashes; route marks survive re-extraction"
```

---

### Task 4: Structured HTTP evidence fields

**Files:**
- Modify: `internal/head/agent/types.go` (`Evidence`), `internal/head/agent/execute_phases_steps.go:29-36` (`stepEvidence`)
- Test: `internal/head/agent/execute_phases_steps_test.go` (append; create if absent)

**Interfaces:**
- Produces: `Evidence.Method string`, `Evidence.URL string`, `Evidence.StatusCode int` (all `json:",omitempty"`), populated for `http_request` steps. Downstream Task 5 reads them.

- [ ] **Step 1: Write the failing test** — append (create file if it does not exist) to `internal/head/agent/`:

```go
package agent

import (
	"testing"
	"time"

	"github.com/binoctal/cerberus/internal/types"
)

func TestStepEvidence_HTTPRequestStructured(t *testing.T) {
	s := TestStep{Action: "http_request", Method: "post", URL: "http://x/api/sessions"}
	res := types.HTTPResult{OK: true, StatusCode: 201, URL: "http://x/api/sessions", Latency: time.Millisecond}
	ev := stepEvidence(s, res)
	if ev.Method != "POST" {
		t.Errorf("Method = %q, want POST (normalized upper)", ev.Method)
	}
	if ev.URL != "http://x/api/sessions" {
		t.Errorf("URL = %q, want the executed URL", ev.URL)
	}
	if ev.StatusCode != 201 {
		t.Errorf("StatusCode = %d, want 201", ev.StatusCode)
	}
	// Default method when the step omits it.
	ev2 := stepEvidence(TestStep{Action: "http_request"}, types.HTTPResult{OK: true, StatusCode: 401, URL: "http://x/y"})
	if ev2.Method != "GET" || ev2.StatusCode != 401 {
		t.Errorf("defaults: Method=%q StatusCode=%d, want GET/401", ev2.Method, ev2.StatusCode)
	}
}
```

Check `TestStep` field names (`Method`, `URL`, `Action`) in `internal/head/agent/executor_types.go`/`ws_protocol.go` and adapt the literals if they differ.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/head/agent/ -run TestStepEvidence_HTTPRequestStructured -v`
Expected: FAIL — `ev.Method` empty.

- [ ] **Step 3: Implement**

In `internal/head/agent/types.go` `Evidence` add after `ExpectAbsent`:
```go
	// http_request structured facts for coverage attribution (route pattern
	// matching). Rule-engine HTTP evidence does not populate these.
	Method     string `json:"method,omitempty"`
	URL        string `json:"url,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
```

In `stepEvidence` extend the `s.Action == "http_request"` branch:
```go
	if s.Action == "http_request" {
		if hr, ok := result.(types.HTTPResult); ok {
			ev.Content = fmt.Sprintf("http_request: %s %d", hr.URL, hr.StatusCode)
			ev.URL = hr.URL
			ev.StatusCode = hr.StatusCode
			m := strings.ToUpper(s.Method)
			if m == "" {
				m = "GET"
			}
			ev.Method = m
		}
	}
```
Add `"strings"` to imports if missing.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/head/agent/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/types.go internal/head/agent/execute_phases_steps.go internal/head/agent/execute_phases_steps_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com commit -m "feat(agent): structured Method/URL/StatusCode on http_request evidence"
```

---

### Task 5: Coverage — denominator synthesis + http attribution + sessionHasVocab fix

**Files:**
- Modify: `internal/session/coverage.go` (`requiredEdges`, `exercisedEdges`, `sessionHasVocab`)
- Test: `internal/session/coverage_test.go` (append; create if absent)

**Interfaces:**
- Consumes: `Evidence.Method/URL/StatusCode` (Task 4), `Vocabulary.HTTPRoutes` (Task 1).
- Produces: `routePatternMatches(pattern, p string) bool`; `routeMethodMatches(declared, actual string) bool`; http routes in the `requiredEdges` denominator as `VocabEdge{FromRole: "http_client", ToRole: "api", Type: "METHOD path", Trigger: "http_request"}`.

- [ ] **Step 1: Write the failing tests** — append to `internal/session/coverage_test.go`:

```go
func TestRoutePatternMatches(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"/api/sessions", "/api/sessions", true},
		{"/api/sessions", "/api/sessions?x=1", true}, // query ignored by caller; matcher sees path only
		{"/api/sessions/:id", "/api/sessions/s_1", true},
		{"/api/sessions/:id", "/api/sessions", false},
		{"/api/sessions/:id", "/api/sessions/s_1/extra", false},
		{"/api/workflows/jobs/*", "/api/workflows/jobs/a/b", true},
		{"/api/workflows/jobs/*", "/api/workflows/jobs", false},
		{"/health", "/healthz", false},
	}
	for _, c := range cases {
		if got := routePatternMatches(c.pattern, c.path); got != c.want {
			t.Errorf("routePatternMatches(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestRouteMethodMatches(t *testing.T) {
	if !routeMethodMatches("ALL", "DELETE") {
		t.Error("ALL must match any method")
	}
	if routeMethodMatches("GET", "POST") {
		t.Error("GET must not match POST")
	}
}

func TestRequiredEdges_HTTPRoutes(t *testing.T) {
	sess := &Session{Config: project.Config{Services: []project.Service{{
		Vocabulary: &project.Vocabulary{HTTPRoutes: []project.VocabHTTPRoute{
			{Method: "POST", Path: "/api/sessions"},
			{Method: "GET", Path: "/api/health", Unsupported: true},
			{Method: "PUT", Path: "/api/x", Partial: true},
		}},
	}}}}
	req := requiredEdges(sess)
	if len(req) != 1 {
		t.Fatalf("required = %+v, want exactly the non-exempt POST route", req)
	}
	if req[0].FromRole != "http_client" || req[0].ToRole != "api" ||
		req[0].Type != "POST /api/sessions" || req[0].Trigger != "http_trigger" {
		if req[0].Trigger != "http_request" {
			t.Fatalf("synthesized route edge wrong: %+v", req[0])
		}
	}
	if req[0].Trigger != "http_request" {
		t.Fatalf("Trigger = %q, want http_request", req[0].Trigger)
	}
}

func TestExercisedEdges_HTTP(t *testing.T) {
	required := []project.VocabEdge{
		{FromRole: "http_client", ToRole: "api", Type: "POST /api/sessions/:id", Trigger: "http_request"},
		{FromRole: "http_client", ToRole: "api", Type: "GET /health", Trigger: "http_request"},
	}
	results := []agent.StepResult{{
		TestCase: &agent.TestCase{
			Steps: []agent.TestStep{}, // no ws_connect; http attribution needs no role
		},
		Evidence: []agent.Evidence{
			{Action: "http_request", Method: "POST", URL: "http://localhost:8989/api/sessions/s_42?verbose=1", StatusCode: 401},
		},
	}}
	ex, _ := exercisedEdges(results, required)
	if !ex[edgeKey("http_client", "api", "POST /api/sessions/:id")] {
		t.Error("401 response must still credit the :param route (any status exercises)")
	}
	if ex[edgeKey("http_client", "api", "GET /health")] {
		t.Error("unhit route credited")
	}
}

func TestSessionHasVocab_HTTPRoutesOnly(t *testing.T) {
	sess := &Session{Config: project.Config{Services: []project.Service{{
		Vocabulary: &project.Vocabulary{
			HTTPRoutes: []project.VocabHTTPRoute{{Method: "GET", Path: "/health"}},
		},
	}}}}
	if !sessionHasVocab(sess) {
		t.Error("http_routes-only service must route to path coverage")
	}
}
```

Adapt the `Session`/`Config`/`Service`/`TestCase`/`StepResult`/`Evidence` literal shapes to the real structs in `internal/session` and `internal/head/agent` (check what existing tests in the file construct; `agent.StepResult` embeds `TestCase *agent.TestCase`). Fix `TestRequiredEdges_HTTPRoutes`'s double-trigger check to a single clean assertion:

```go
	e := req[0]
	if e.FromRole != "http_client" || e.ToRole != "api" ||
		e.Type != "POST /api/sessions" || e.Trigger != "http_request" {
		t.Fatalf("synthesized route edge wrong: %+v", e)
	}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/session/ -run 'TestRoutePatternMatches|TestRouteMethodMatches|TestRequiredEdges_HTTPRoutes|TestExercisedEdges_HTTP|TestSessionHasVocab_HTTPRoutesOnly' -v`
Expected: FAIL — `undefined: routePatternMatches` etc.

- [ ] **Step 3: Implement** — in `internal/session/coverage.go`:

Extend `sessionHasVocab`:
```go
		if svc.Vocabulary != nil && (len(svc.Vocabulary.Edges) > 0 || len(svc.Vocabulary.HTTPRoutes) > 0) {
			return true
		}
```

In `requiredEdges`, after the `svc.Protocol` http_triggers block, still inside the services loop:
```go
		// Mounted HTTP routes (Hono-extracted): one required edge per
		// non-exempt route, same synthesis pattern as http_triggers. Any
		// response status credits exercise at attribution time — 4xx/5xx
		// still prove the route was reached; auth semantics belong to the
		// violations layer.
		if svc.Vocabulary != nil {
			for _, r := range svc.Vocabulary.HTTPRoutes {
				if r.Partial || r.Unsupported {
					continue
				}
				out = append(out, project.VocabEdge{
					FromRole: "http_client",
					ToRole:   "api",
					Type:     r.Method + " " + r.Path,
					Trigger:  "http_request",
				})
			}
		}
```

Add matchers near `edgeKey`:
```go
// routePatternMatches reports whether a concrete request path matches a
// Hono route pattern: :param consumes exactly one segment; a lone trailing *
// consumes one-or-more. Paths are pre-stripped of query strings.
func routePatternMatches(pattern, p string) bool {
	ps := strings.Split(strings.Trim(pattern, "/"), "/")
	vs := strings.Split(strings.Trim(p, "/"), "/")
	for i, seg := range ps {
		if seg == "*" {
			return i < len(vs) // * is final; needs at least one segment
		}
		if i >= len(vs) {
			return false
		}
		if strings.HasPrefix(seg, ":") {
			continue
		}
		if seg != vs[i] {
			return false
		}
	}
	return len(ps) == len(vs)
}

// routeMethodMatches: ALL (Hono app.all) matches any method.
func routeMethodMatches(declared, actual string) bool {
	return declared == "ALL" || declared == actual
}
```

In `exercisedEdges`, add the http branch at the top of the evidence loop (before the ws_receive check):
```go
		for _, ev := range r.Evidence {
			// http_request steps credit routes directly (any status).
			if ev.Action == "http_request" {
				if ev.Method == "" || ev.URL == "" {
					continue
				}
				if u, perr := url.Parse(ev.URL); perr == nil {
					for _, e := range required {
						if e.Trigger != "http_request" || e.FromRole != "http_client" {
							continue
						}
						parts := strings.SplitN(e.Type, " ", 2)
						if len(parts) != 2 {
							continue
						}
						if routeMethodMatches(parts[0], ev.Method) && routePatternMatches(parts[1], u.Path) {
							exercised[edgeKey(e.FromRole, e.ToRole, e.Type)] = true
						}
					}
				}
				continue
			}
			// (existing ws_receive branch unchanged)
```
Add `"net/url"` and confirm `"strings"` in imports.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/session/ -v && go test ./... -count=1`
Expected: PASS everywhere.

- [ ] **Step 5: Full gate + Commit**

Run: `make check`
Expected: fmt + lint + all tests green.

```bash
git add internal/session/coverage.go internal/session/coverage_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com commit -m "feat(session): http routes in path-coverage denominator + http_request attribution; sessionHasVocab counts http_routes"
```

---

## Self-Review (done at plan time)

- **Spec coverage:** §1 model+marks (T1), §2 extractor incl. skipped_on/files/unmounted-exclusion/dup-merge (T2), §3 CLI merge+hashes+wording (T3), §4 evidence fields (T4) + synthesis/attribution/sessionHasVocab/gap-reuse (T5), §5 validation (T1), §6 tests distributed per task. Out-of-scope items listed in spec, none implemented.
- **Placeholders:** none; every code step carries verbatim code. The two "adapt literal shapes" notes are verification instructions against named files, not deferred work.
- **Type consistency:** `VocabHTTPRoute` field names used identically in T1/T2 (json tags method/path/mount)/T3/T5; `Evidence.Method/URL/StatusCode` defined T4, consumed T5; `Trigger: "http_request"`/`FromRole: "http_client"`/`ToRole: "api"` consistent T5 tests + impl.
