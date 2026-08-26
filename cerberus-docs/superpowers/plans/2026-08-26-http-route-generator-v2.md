# HTTP Route Case Generator v2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace bare reachability smokes with vocab-fact-driven semantic cases: auth-shape assertions, minimal request bodies, generalized role JWT injection, and list-route-chained real `:param` values.

**Architecture:** Grounded extractors (ts-morph, bundled `extractor.mjs`) emit per-route facts (`middlewares`, `min_body`, `param_sources`) into the vocab file; a service-level `http_auth_middlewares` list (heuristic default, hand-overridable) derives `auth` per route; the scout generator consumes only these facts and stays SUT-generic. The executor already supports Body/Capture/placeholders — the only executor change is one new status class.

**Tech Stack:** Go 1.25, ts-morph extractor subprocess (Node), YAML vocab, existing `agent.TestCase`/`TestStep` model.

**Spec:** `cerberus-docs/superpowers/specs/2026-08-26-http-route-generator-v2-design.md`

## Global Constraints

- Commit author MUST be `binoctal <binoctal@gmail.com>`, NEVER any `Co-Authored-By` trailer.
- Code comments and commit messages in English.
- No CGo; pure Go SQLite unchanged.
- SUT facts live in vocab/protocol files, NEVER in generator source (`internal/head/scout`) — the generator may not contain open-agents knowledge (no path literals beyond the existing admin-prefix idiom already there).
- All docs go to `cerberus-docs/`, never `docs/`.
- Never run whole-repo `go test` in the open-agents bridge repo; this plan never runs tests there anyway.
- `make check` runs `gofmt -w .` — check `git status` after it; unrelated dirty files must not ride along in commits.

---

### Task 1: `2xx_4xx` status class

**Files:**
- Modify: `internal/head/agent/execute_phases_steps.go` (`statusInClass`, ~line 113; `statusClassError` message)
- Test: `internal/head/agent/execute_phases_steps_test.go` (existing table at ~line 760)

**Interfaces:**
- Produces: `expect_status_class: "2xx_4xx"` accepted by the executor (Task 6 relies on it).

- [ ] **Step 1: Write the failing test** — add rows to the existing table-driven `statusInClass` test:

```go
{class: "2xx_4xx", code: 200, want: true},
{class: "2xx_4xx", code: 404, want: true},
{class: "2xx_4xx", code: 302, want: false},
{class: "2xx_4xx", code: 500, want: false},
{class: "2xx_4xx", code: 0, want: false}, // transport error in no class
```

Match the surrounding struct's field names exactly (read the table first; the existing rows use `class`, `code`, and a want/err convention — follow it).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/agent/ -run TestStatusInClass -v` (or the actual test name found in step 1)
Expected: FAIL — `unknown class "2xx_4xx"`

- [ ] **Step 3: Implement** — in `statusInClass`, before the `default:` arm:

```go
case "2xx_4xx":
	// Compound class: success OR client error — "no server error, no
	// routing error" (authed mutations accept legitimate 4xx rejections).
	return (code >= 200 && code < 300) || (code >= 400 && code < 500), nil
```

Update the error message string `(want 2xx|3xx|4xx|5xx|any)` to include `2xx_4xx` in both `statusInClass` and any doc comment listing the enum (also `internal/head/agent/types.go` `ExpectStatusClass` field comment).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/head/agent/ -run 'TestStatusInClass|TestRunSteps' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/execute_phases_steps.go internal/head/agent/execute_phases_steps_test.go internal/head/agent/types.go
git commit -m "feat(agent): compound 2xx_4xx status class — no-server-error gate for authed mutations"
```

---

### Task 2: Vocab data model + validation

**Files:**
- Modify: `internal/project/vocabulary.go` (`VocabHTTPRoute` ~line 140, `Vocabulary` ~line 22, `ValidateVocabulary` ~line 157)
- Test: `internal/project/vocabulary_test.go` (create if absent; else append)

**Interfaces:**
- Produces (Task 3–6 consume):

```go
type VocabParamSource struct {
	Route string `yaml:"route" json:"route"` // e.g. "GET /api/devices"
	Pick  string `yaml:"pick" json:"pick"`   // dot-path, e.g. "0.id"
}
```

`VocabHTTPRoute` gains: `Middlewares []string` (`middlewares`), `Auth string` (`auth`), `MinBody map[string]any` (`min_body`), `ParamSources map[string]VocabParamSource` (`param_sources`).
`Vocabulary` gains: `HTTPAuthMiddlewares []string` (`http_auth_middlewares`).

- [ ] **Step 1: Write the failing tests**

```go
func TestValidateVocabularyRouteFacts(t *testing.T) {
	base := func(mut func(*Vocabulary)) *Vocabulary {
		v := &Vocabulary{HTTPRoutes: []VocabHTTPRoute{{
			Method: "GET", Path: "/api/devices/:id",
			Middlewares: []string{"authMiddleware"}, Auth: "required",
			ParamSources: map[string]VocabParamSource{
				":id": {Route: "GET /api/devices", Pick: "0.id"},
			},
		}}}
		mut(v)
		return v
	}
	cases := []struct {
		name string
		mut  func(*Vocabulary)
		want string
	}{
		{"valid", func(*Vocabulary) {""}, ""},
		{"bad auth enum", func(v *Vocabulary) {
			v.HTTPRoutes[0].Auth = "maybe"
		}, `auth "maybe" not in enum`},
		{"param source key not in path", func(v *Vocabulary) {
			v.HTTPRoutes[0].ParamSources = map[string]VocabParamSource{
				":other": {Route: "GET /api/devices", Pick: "0.id"}}
		}, ":other"},
		{"param source route unresolved", func(v *Vocabulary) {
			v.HTTPRoutes[0].ParamSources = map[string]VocabParamSource{
				":id": {Route: "GET /api/nope", Pick: "0.id"}}
		}, `unresolved list route "GET /api/nope"`},
		{"param route method mismatch", func(v *Vocabulary) {
			v.HTTPRoutes[0].ParamSources[":id"] = VocabParamSource{
				Route: "POST /api/devices", Pick: "0.id"}
		}, `unresolved list route "POST /api/devices"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateVocabulary(base(tc.mut))
			if tc.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}
```

(`{"valid", func(*Vocabulary) {""}, ""}` — the no-op body is a comment-free empty block in real Go; adjust to `func(*Vocabulary) {}`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/project/ -run TestValidateVocabulary -v`
Expected: compile FAIL — `VocabParamSource` undefined

- [ ] **Step 3: Implement**

Add `VocabParamSource` (above), the four fields to `VocabHTTPRoute` (yaml/json tags exactly as in the Interfaces block, `omitempty` on all four), and `HTTPAuthMiddlewares []string \`yaml:"http_auth_middlewares,omitempty" json:"http_auth_middlewares,omitempty"\`` to `Vocabulary`. In `ValidateVocabulary`'s route loop add:

```go
switch r.Auth {
case "", "required", "none", "unknown":
default:
	return fmt.Errorf("http_routes[%d]: auth %q not in enum (required|none|unknown)", i, r.Auth)
}
for name, ps := range r.ParamSources {
	if !strings.Contains(r.Path, "/"+name+"/") && !strings.HasSuffix(r.Path, name) {
		return fmt.Errorf("http_routes[%d]: param_sources key %q not a :param of %q", i, name, r.Path)
	}
	m, rp, ok := strings.Cut(ps.Route, " ")
	if !ok || m != "GET" || !routeSet[rp] {
		return fmt.Errorf("http_routes[%d]: param_sources[%q]: unresolved list route %q", i, name, ps.Route)
	}
}
```

Build `routeSet map[string]bool` from all `v.HTTPRoutes` paths BEFORE the validation loop.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/project/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/project/vocabulary.go internal/project/vocabulary_test.go
git commit -m "feat(project): vocab route facts — middlewares/auth/min_body/param_sources + validation"
```

---

### Task 3: Extractor — auth middleware chain (`app.use` + inline args)

**Files:**
- Modify: `internal/vocabextract/extractor.mjs` (`walkFile` ~line 390, `addRoute` ~line 380)
- Test: `internal/vocabextract/testdata/hono/use-auth.ts` (create), `internal/vocabextract/testdata/hono/routes/things.ts` (extend), `cmd/cerberus/main_protocol_vocabulary_test.go` (append)

**Interfaces:**
- Produces: extractor JSON route objects gain `middlewares: ["authMiddleware", ...]` (flows straight into `VocabHTTPRoute.Middlewares` — the cmd unmarshals directly into that struct).

- [ ] **Step 1: Write the failing fixture + test**

Fixture `testdata/hono/use-auth.ts`:

```ts
import { Hono } from 'hono';
import { thingRoutes } from './routes/things';

const app = new Hono();
const authMiddleware = async (c: any, next: any) => { await next(); };

app.get('/health', (c) => c.json({ ok: true }));
app.use('/api/things', authMiddleware);
app.get('/api/things', (c) => c.json([]));
app.route('/api/things', thingRoutes);
export default app;
```

Extend `testdata/hono/routes/things.ts` with one inline-middleware route (keep existing content):

```ts
thingsRoutes.get('/:id/gated', rateLimiter, (c) => c.json({}));
```

Test (append to `cmd/cerberus/main_protocol_vocabulary_test.go`; mirror `TestRunProtocolVocabulary_HonoRoutes`'s setup — same workdir/`--from` invocation convention):

```go
func TestRunProtocolVocabulary_UseAuthMiddlewares(t *testing.T) {
	// Asserts: app.use('/api/things', authMiddleware) prefixes every route
	// under /api/things (direct + router-mounted), and inline middleware
	// args are captured per-route.
	vocab := extractVocabForTest(t, "hono/use-auth.ts") // helper from the HonoRoutes test; reuse or extract it
	byKey := map[string]project.VocabHTTPRoute{}
	for _, r := range vocab.HTTPRoutes {
		byKey[r.Method+"|"+r.Path] = r
	}
	if r := byKey["GET|/health"]; len(r.Middlewares) != 0 {
		t.Fatalf("/health must carry no middlewares, got %v", r.Middlewares)
	}
	for _, key := range []string{"GET|/api/things", "GET|/api/things/:id", "GET|/api/things/:id/gated"} {
		r, ok := byKey[key]
		if !ok {
			t.Fatalf("route %s missing", key)
		}
		if !slices.Contains(r.Middlewares, "authMiddleware") {
			t.Fatalf("%s must inherit authMiddleware from app.use, got %v", key, r.Middlewares)
		}
	}
	if r := byKey["GET|/api/things/:id/gated"]; !slices.Contains(r.Middlewares, "rateLimiter") {
		t.Fatalf("inline rateLimiter not captured: %v", r.Middlewares)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/cerberus/ -run TestRunProtocolVocabulary_UseAuthMiddlewares -v`
Expected: FAIL — `/api/things` routes have no `middlewares` (the extractor never reads `app.use` or inline args).

- [ ] **Step 3: Implement in `extractor.mjs`**

1. `addRoute(method, fullPath, mount, line, middlewares)` — store `middlewares` on the route object (`middlewares: middlewares.length ? middlewares : undefined`).
2. In `walkFile`, thread a `useMws` array (entries `{prefix, name}`) as a new parameter; at each `app.use(...)` call with a string-literal first arg, push `{prefix: joinPath(prefix, lit0), name}` for EACH remaining argument that is a bare Identifier; a path-less `app.use(mw)` records prefix `/`.
3. Inline capture: for `HTTP_METHODS` calls, collect `call.getArguments().slice(1)` entries that are bare Identifiers (skip arrow/function args — those are the handler) into `inlineMw`.
4. At `addRoute` time: `const mws = [...inlineMw, ...useMws.filter(u => routeHasPrefix(fullPath, u.prefix)).map(u => u.name)]` where `routeHasPrefix(p, pre)` is `pre === '/' || p === pre || p.startsWith(pre + '/')`.
5. `app.route('/p', router)` traversal passes `useMws` down unchanged (prefix middlewares declared in the parent file still apply).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/cerberus/ -run TestRunProtocolVocabulary -v`
Expected: ALL PASS including pre-existing Hono/MultiEntry tests (no regression: routes without middleware carry no new field).

- [ ] **Step 5: Commit**

```bash
git add internal/vocabextract/extractor.mjs internal/vocabextract/testdata/hono/use-auth.ts internal/vocabextract/testdata/hono/routes/things.ts cmd/cerberus/main_protocol_vocabulary_test.go
git commit -m "feat(vocabextract): per-route middleware chain — app.use prefix mounts and inline middleware args"
```

---

### Task 4: Extractor — zod `min_body`

**Files:**
- Modify: `internal/vocabextract/extractor.mjs`
- Test: `internal/vocabextract/testdata/hono/zod-body.ts` (create), `cmd/cerberus/main_protocol_vocabulary_test.go` (append)

**Interfaces:**
- Produces: route objects gain `min_body: {field: literal}` — literal values: string→`"x"`, number→`0`, boolean→`false`. Absent when not extractable.

- [ ] **Step 1: Write the failing fixture + test**

Fixture `testdata/hono/zod-body.ts`:

```ts
import { Hono } from 'hono';
import { z } from 'zod';
import { zValidator } from '@hono/zod-validator';

const app = new Hono();

// Declared schema referenced by zValidator (open-agents' dominant form).
const CreateThing = z.object({
	name: z.string(),
	count: z.number(),
	active: z.boolean(),
});
app.post('/api/things', zValidator('json', CreateThing), (c) => c.json({}));

// Inline schema.
app.post('/api/inline', zValidator('json', z.object({ label: z.string() })), (c) => c.json({}));

// Unextractable: refine + nested object — must OMIT min_body, never guess.
const Picky = z.object({ a: z.string().refine(() => true), nested: z.object({ b: z.string() }) });
app.post('/api/picky', zValidator('json', Picky), (c) => c.json({}));

// No schema at all.
app.post('/api/bare', (c) => c.json({}));
export default app;
```

Test (append; same helper as Task 3):

```go
func TestRunProtocolVocabulary_MinBody(t *testing.T) {
	vocab := extractVocabForTest(t, "hono/zod-body.ts")
	byKey := map[string]project.VocabHTTPRoute{}
	for _, r := range vocab.HTTPRoutes {
		byKey[r.Method+"|"+r.Path] = r
	}
	want := map[string]map[string]any{
		"POST|/api/things": {"name": "x", "count": float64(0), "active": false},
		"POST|/api/inline": {"label": "x"},
	}
	for key, body := range want {
		r, ok := byKey[key]
		if !ok {
			t.Fatalf("route %s missing", key)
		}
		if !reflect.DeepEqual(r.MinBody, body) {
			t.Fatalf("%s: min_body = %#v, want %#v", key, r.MinBody, body)
		}
	}
	for _, key := range []string{"POST|/api/picky", "POST|/api/bare"} {
		if r := byKey[key]; len(r.MinBody) != 0 {
			t.Fatalf("%s: min_body must be omitted, got %#v", key, r.MinBody)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/cerberus/ -run TestRunProtocolVocabulary_MinBody -v`
Expected: FAIL — no `min_body` anywhere.

- [ ] **Step 3: Implement in `extractor.mjs`**

1. Before the statement loop, build `schemaMap`: for each top-level `const X = z.object({...})` declaration, record `X` → the object-literal node (skip when any property is not a plain `PropertyAssignment` of a `z.<primitive>()` call — i.e. store `null` for unextractable).
2. Property mapping: `z.string()`→`'x'`, `z.number()`→`0`, `z.boolean()`→`false`; anything else (`.refine`, nested `z.object`, `.optional()`-chained, spread) marks the WHOLE schema unextractable (omit `min_body`).
3. In the route-args scan: recognize `zValidator('json', <arg2>)` calls (PropertyAccess name `zValidator`); `<arg2>` is either an Identifier resolved through `schemaMap` or an inline `z.object({...})` literal parsed by the same mapper.
4. `addRoute` gains the extracted body: `min_body` key on the route object, dropped when `undefined`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/cerberus/ -run TestRunProtocolVocabulary -v`
Expected: ALL PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/vocabextract/extractor.mjs internal/vocabextract/testdata/hono/zod-body.ts cmd/cerberus/main_protocol_vocabulary_test.go
git commit -m "feat(vocabextract): minimal legal request bodies from zod schemas (literal primitives only, omit-not-guess)"
```

---

### Task 5: CLI — auth derivation, param-chain inference, re-extraction preservation

**Files:**
- Modify: `cmd/cerberus/main_protocol.go` (`runProtocolVocabulary`, after the extraction loop ~line 200, and the preservation block ~line 240-262)
- Test: `cmd/cerberus/main_protocol_vocabulary_test.go` (append + extend `TestRunProtocolVocabulary_ReextractPreservesAnnotations`)

**Interfaces:**
- Consumes: Task 2's `Auth`/`ParamSources`/`HTTPAuthMiddlewares` fields; Tasks 3–4's extractor output (`middlewares`, `min_body`).
- Produces: vocab files whose routes carry final `auth` + `param_sources`; hand-set values survive re-extraction (Task 6 consumes).

- [ ] **Step 1: Write the failing tests**

```go
func TestRunProtocolVocabulary_AuthDerivationAndParamChain(t *testing.T) {
	vocab := extractVocabForTest(t, "hono/use-auth.ts")
	byKey := map[string]project.VocabHTTPRoute{}
	for _, r := range vocab.HTTPRoutes {
		byKey[r.Method+"|"+r.Path] = r
	}
	// authMiddleware matches the /auth|bearer|jwt/i heuristic -> required.
	if r := byKey["GET|/api/things"]; r.Auth != "required" {
		t.Fatalf("auth = %q, want required", r.Auth)
	}
	// No middleware -> none.
	if r := byKey["GET|/health"]; r.Auth != "none" {
		t.Fatalf("auth = %q, want none", r.Auth)
	}
	// rateLimiter is a middleware but not an auth one -> unknown.
	if r := byKey["GET|/api/things/:id/gated"]; r.Auth != "unknown" {
		t.Fatalf("auth = %q, want unknown (non-auth middleware present)", r.Auth)
	}
	// http_auth_middlewares list is emitted.
	if !slices.Contains(vocab.HTTPAuthMiddlewares, "authMiddleware") {
		t.Fatalf("http_auth_middlewares = %v", vocab.HTTPAuthMiddlewares)
	}
	// :id chain points at the list route with dot-path pick.
	r := byKey["GET|/api/things/:id"]
	ps, ok := r.ParamSources[":id"]
	if !ok || ps.Route != "GET /api/things" || ps.Pick != "0.id" {
		t.Fatalf("param_sources = %#v", r.ParamSources)
	}
}
```

Extend `TestRunProtocolVocabulary_ReextractPreservesAnnotations`: after its existing scenario, add a route-level case — first extraction on a fixture with `use-auth.ts`, hand-edit the output vocab to set `param_sources[":id"].pick: "1.id"` and `http_auth_middlewares: [handPicked]`, re-run extraction, assert both hand values survived (auth/middlewares/min_body re-derived fresh).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/cerberus/ -run 'TestRunProtocolVocabulary_AuthDerivationAndParamChain|TestRunProtocolVocabulary_ReextractPreserves' -v`
Expected: FAIL — no derivation logic exists.

- [ ] **Step 3: Implement in `runProtocolVocabulary`**

After the extraction merge loops, before building `vocab :=`:

```go
// Auth middleware heuristic: name-based judgment, emitted for hand override.
authMw := map[string]bool{}
var authMwRe = regexp.MustCompile(`(?i)auth|bearer|jwt`)
for _, r := range routes {
	for _, mw := range r.Middlewares {
		if authMwRe.MatchString(mw) {
			authMw[mw] = true
		}
	}
}
for i := range routes {
	switch {
	case len(routes[i].Middlewares) == 0:
		routes[i].Auth = "none"
	default:
		routes[i].Auth = "unknown"
		for _, mw := range routes[i].Middlewares {
			if authMw[mw] {
				routes[i].Auth = "required"
				break
			}
		}
	}
	// Param-chain inference: same-prefix GET list route, :param tail only.
	for _, p := range pathParams(routes[i].Path) { // []string of ":name" segments
		if _, hand := routes[i].ParamSources[p]; hand {
			continue
		}
		list := strings.TrimSuffix(routes[i].Path, "/"+p)
		if !strings.Contains(list, ":") {
			if listOK := routeMethodPath(routes, "GET", list); listOK {
				if routes[i].ParamSources == nil {
					routes[i].ParamSources = map[string]project.VocabParamSource{}
				}
				routes[i].ParamSources[p] = project.VocabParamSource{Route: "GET " + list, Pick: "0.id"}
			}
		}
	}
}
```

(`pathParams` and `routeMethodPath` are small local helpers — write them.) Set `vocab.HTTPAuthMiddlewares` from the sorted map keys. In the preservation block extend the route-marks loop:

```go
vocab.HTTPRoutes[i].Partial = old.Partial
vocab.HTTPRoutes[i].Unsupported = old.Unsupported
for p, ps := range old.ParamSources { // hand-tuned chains win
	if vocab.HTTPRoutes[i].ParamSources == nil {
		vocab.HTTPRoutes[i].ParamSources = map[string]project.VocabParamSource{}
	}
	vocab.HTTPRoutes[i].ParamSources[p] = ps
}
```

And above it: `if len(prev.HTTPAuthMiddlewares) > 0 { vocab.HTTPAuthMiddlewares = prev.HTTPAuthMiddlewares }` (hand-curated list wins over heuristic).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/cerberus/ -v`
Expected: ALL PASS (whole cmd package — the ReextractPreserves extension proves the merge).

- [ ] **Step 5: Commit**

```bash
git add cmd/cerberus/main_protocol.go cmd/cerberus/main_protocol_vocabulary_test.go
git commit -m "feat(protocol): auth derivation + param-chain inference + hand-mark preservation on re-extraction"
```

---

### Task 6: Generator v2 — rewrite `httpRouteCases`

**Files:**
- Modify: `internal/head/scout/http_route_cases.go` (full rewrite)
- Test: `internal/head/scout/http_route_cases_test.go` (extend; keep existing cases passing — they pin v1 reachability behavior for `auth: none/unknown`)

**Interfaces:**
- Consumes: Task 1's `2xx_4xx`; Task 2's `Auth`/`MinBody`/`ParamSources`; existing `agent.TestStep` fields (`Body`, `Capture map[string]string`, `AuthRole`, `ExpectStatusClass`), `{{case.<name>}}` placeholder syntax.
- Produces: `httpRouteCases(svc project.Service) []agent.TestCase` (same signature — call sites in `ws_cases.go:53,387` untouched).

- [ ] **Step 1: Write the failing tests** (append; use a `project.Service` fixture mirroring the existing test's construction):

```go
func TestHTTPRouteCasesV2(t *testing.T) {
	svc := routeV2Fixture() // Service with Vocabulary.HTTPRoutes:
	//  1) GET /api/things, auth required, no params
	//  2) POST /api/things, auth required, min_body {name: x}
	//  3) GET /api/things/:id, auth required, param_sources :id -> GET /api/things (0.id)
	//  4) GET /health, auth none
	//  5) GET /api/mystery/:id, auth required, NO param_sources (degradation)
	//  6) GET /api/admin/stats, auth required (admin prefix)
	// Protocol roles: admin + web, both with CredentialRef.
	cases := httpRouteCases(svc)
	byID := map[string]agent.TestCase{}
	for _, c := range cases {
		byID[c.ID] = c
	}
	// authed GET, param-free: single step, 2xx.
	c := byID[caseID(svc, "GET", "/api/things", "authed")]
	if len(c.Steps) != 1 || c.Steps[0].ExpectStatusClass != "2xx" || c.Steps[0].AuthRole != "web" {
		t.Fatalf("authed GET shape wrong: %+v", c.Steps)
	}
	// unauth probe on the same route: 4xx, no AuthRole.
	c = byID[caseID(svc, "GET", "/api/things", "unauth")]
	if c.Steps[0].ExpectStatusClass != "4xx" || c.Steps[0].AuthRole != "" {
		t.Fatalf("unauth shape wrong: %+v", c.Steps)
	}
	// authed mutation: body + 2xx_4xx.
	c = byID[caseID(svc, "POST", "/api/things", "authed")]
	if c.Steps[0].ExpectStatusClass != "2xx_4xx" || c.Steps[0].Body != `{"name":"x"}` {
		t.Fatalf("mutation shape wrong: %+v", c.Steps)
	}
	// param chain: two steps — capture then assert.
	c = byID[caseID(svc, "GET", "/api/things/:id", "authed")]
	if len(c.Steps) != 2 {
		t.Fatalf("param chain must be 2 steps, got %d", len(c.Steps))
	}
	if c.Steps[0].Capture["0.id"] == "" || !strings.Contains(c.Steps[1].URL, "{{case.") {
		t.Fatalf("capture/placeholder wiring wrong: %+v", c.Steps)
	}
	// degradation: no param_sources -> reachability tier only (any, placeholder 1).
	if id := caseID(svc, "GET", "/api/mystery/:id", "authed"); _, ok := byID[id]; ok {
		t.Fatalf("unresolvable param route must NOT emit an authed case: %s", id)
	}
	if c := byID[caseID(svc, "GET", "/api/mystery/:id", "")]; c.Steps[0].ExpectStatusClass != "any" {
		t.Fatalf("degraded route must stay reachability: %+v", c.Steps)
	}
	// auth none: single reachability case, no unauth twin.
	if id := caseID(svc, "GET", "/health", "unauth"); _, ok := byID[id]; ok {
		t.Fatalf("auth:none route must not get an unauth twin")
	}
	// admin prefix -> admin role.
	if c := byID[caseID(svc, "GET", "/api/admin/stats", "authed")]; c.Steps[0].AuthRole != "admin" {
		t.Fatalf("admin route must inject admin role, got %q", c.Steps[0].AuthRole)
	}
}
```

`caseID(svc, method, path, suffix)` mirrors the v1 ID scheme: `http-route-<svc>-<method-lower>-<trimmed-path>` + (`-unauth` | `-authed` | ``). `routeV2Fixture` builds the Service/Vocabulary/Protocol structs with real field names — read the existing `http_route_cases_test.go` fixtures for the exact construction idiom and copy it.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/scout/ -run TestHTTPRouteCases -v`
Expected: FAIL — v1 emits reachability-only cases.

- [ ] **Step 3: Implement (rewrite `httpRouteCases`)**

Keep: exempt-route skip, sort, `fillRouteParams`, `isAdminPath`. Replace the emit loop:

```go
for _, r := range routes {
	method := r.Method
	if method == "ALL" {
		method = "GET"
	}
	authRole := roleForRoute(svc, r.Path) // "" | "admin" | "web"
	resolvable := paramResolvable(r)      // no :params, or every :param has a param_source
	switch r.Auth {
	case "required":
		cases = append(cases,
			unauthCase(svc, r, method), // creds "" + ExpectStatusClass "4xx"
			reachabilityCase(svc, r, method)) // v1 shape, honesty fallback
		if authRole != "" && (resolvable || !strings.Contains(r.Path, ":")) {
			cases = append(cases, authedCase(svc, r, method, authRole))
		}
	default: // none | unknown | "" (pre-v2 vocab)
		cases = append(cases, reachabilityCase(svc, r, method))
	}
}
```

`authedCase` builds steps: for each `:param` in order, a `http_request` GET on `param_sources[":p"].Route`'s path with `Capture: {ps.Pick: "p_" + name}`; then the target step with URL placeholders `{{case.p_:name}}`-substituted path (`fillRouteParams` extended to replace `:name` with the placeholder instead of `1` when a source exists), `Body: json.Marshal(r.MinBody)` when present, `ExpectStatusClass`: `"2xx"` for body-less GET/DELETE, `"2xx_4xx"` when `MinBody` is set. Expectation text per honesty tier:

- unauth: `route rejects unauthenticated requests (4xx) — auth middleware present in vocab`
- authed (no body): `authenticated request succeeds (2xx); real :param values from list-route chaining`
- authed (body): `authenticated mutation returns success or client-error, never 5xx; minimal legal body from zod vocab`
- reachability: v1 text unchanged.

`roleForRoute`: `isAdminPath` → `admin` if the admin role has a CredentialRef; else `web` if that role has one; else `""` — the v1 `adminRole` block generalized, zero new SUT knowledge.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/head/scout/ -v`
Expected: ALL PASS including pre-existing v1 tests (routes without v2 facts fall to reachability — old fixtures keep passing).

- [ ] **Step 5: Commit**

```bash
git add internal/head/scout/http_route_cases.go internal/head/scout/http_route_cases_test.go
git commit -m "feat(scout): HTTP route generator v2 — unauth 4xx probes, authed 2xx with real params and minimal bodies"
```

---

### Task 7: Real-source integration test (open-agents)

**Files:**
- Create: `internal/vocabextract/http_facts_openagents_integration_test.go`

**Interfaces:**
- Consumes: `vocabextract.Extract` + sibling `../open-agents` checkout (skip-if-absent convention from `ui_titles_openagents_integration_test.go`). Node on PATH required; skip if absent.

- [ ] **Step 1: Write the test**

```go
//go:build integration

// Proves the v2 extractors recover real facts from the actual open-agents
// worker (not fixtures). Sibling checkout required (../open-agents), same
// convention as the UI-titles integration test.
package vocabextract

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractHTTPFactsAgainstRealOpenAgents(t *testing.T) {
	worker := "../../../open-agents/apps/web-worker/src/worker.ts" // ADJUST: locate the real entry used by the dogfood vocab (dogfood/realtime-e2e env or .cerberus/vocab source.files records the exact path — read it and pin it here)
	if _, err := os.Stat(worker); err != nil {
		t.Skipf("sibling open-agents checkout not available: %v", err)
	}
	raw, err := Extract(context.Background(), worker)
	if err != nil {
		t.Skipf("node unavailable or extraction failed: %v", err)
	}
	var out struct {
		HTTPRoutes []struct {
			Method      string         `json:"method"`
			Path        string         `json:"path"`
			Middlewares []string       `json:"middlewares"`
			MinBody     map[string]any `json:"min_body"`
		} `json:"http_routes"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.HTTPRoutes) < 300 {
		t.Fatalf("expected 300+ routes (dogfood vocab has 337), got %d", len(out.HTTPRoutes))
	}
	authed, withBody := 0, 0
	for _, r := range out.HTTPRoutes {
		for _, mw := range r.Middlewares {
			if mw == "authMiddleware" || len(mw) > 0 && strings.Contains(strings.ToLower(mw), "auth") {
				authed++
				break
			}
		}
		if len(r.MinBody) > 0 {
			withBody++
		}
	}
	// Thresholds pinned from the first live run of this test; loosen ONLY
	// with a written justification referencing the observed counts.
	if authed < 30 {
		t.Fatalf("auth-required routes suspiciously low: %d", authed)
	}
	if withBody < 5 {
		t.Fatalf("min_body routes suspiciously low: %d", withBody)
	}
}
```

Before finalizing the thresholds and the worker path: run the extractor once against the real repo (`go test ./internal/vocabextract/ -tags integration -run RealOpenAgents -v`), read the observed counts, and pin thresholds at ≥60% of observed (slack for repo drift). Record the observed numbers in a comment next to the thresholds. Add the missing `strings` import.

- [ ] **Step 2: Run it**

Run: `go test ./internal/vocabextract/ -tags integration -run TestExtractHTTPFactsAgainstRealOpenAgents -v`
Expected: PASS with observed counts printed via `t.Logf` (add one) — then pin thresholds per above and re-run.

- [ ] **Step 3: Commit**

```bash
git add internal/vocabextract/http_facts_openagents_integration_test.go
git commit -m "test(vocabextract): real-source integration test for auth/body facts against open-agents"
```

---

### Task 8: Full gate + live break test

**Files:**
- No repo files (verification only). Temporary edit to the open-agents parent repo's worker auth mount, RESTORED afterwards.

**Interfaces:**
- Consumes: everything above; `make check`, `make integration-openagents`.

- [ ] **Step 1: Static gate**

Run: `make check`
Expected: fmt + lint + test all green; `git status` clean of unexpected fmt fallout.

- [ ] **Step 2: Live suite green**

Run: `make integration-openagents`
Expected: green; coverage not lower than the current baseline (run 29: 99.5% on its vocab — the suite's own report is the reference).

- [ ] **Step 3: Break test — teeth proof (part 1)**

In the open-agents parent repo (NOT the bridge submodule), temporarily comment out the auth-middleware mount for `/api` in the worker entry (the exact line located in Task 7). Do NOT commit this change.

Run: `make integration-openagents`
Expected: **RED — the new `-unauth` cases fail with 2xx-instead-of-4xx**. If they stay green, the assertions have no teeth: STOP and file the failure.

- [ ] **Step 4: Restore and re-verify**

Run: `git -C ../open-agents checkout -- <worker file>` (restore), then `make integration-openagents`
Expected: green again.

- [ ] **Step 5: Report**

Report the four outcomes (check, live green, break red, restore green) in the task summary — this is the acceptance gate the user chose; all four must be stated explicitly with evidence.

---

## Self-Review (done)

- Spec coverage: §1 data model → Task 2; §2 extractors → Tasks 3–5; §3 generator + degradation + role generalization + `2xx_4xx` → Tasks 1+6; §4 executor (one enum) → Task 1; §5 merge/denominator/preservation → Task 5 (+6 keeps call sites/denominator untouched); §6 testing/acceptance → Tasks 1–7 + Task 8 break test. Out-of-scope items absent.
- Placeholder scan: Task 7 contains one flagged ADJUST (worker entry path must be pinned from the dogfood vocab's `source.files` on first run — an observable fact, not a design gap); thresholds are explicitly pinned-with-justification steps. No TBDs elsewhere.
- Type consistency: `VocabParamSource{Route,Pick}` consistent across Tasks 2/5/6; `middlewares`/`min_body`/`param_sources` JSON keys consistent between extractor (3/4) and struct tags (2); `caseID` suffix scheme (`-unauth`/`-authed`) defined in Task 6 only.
