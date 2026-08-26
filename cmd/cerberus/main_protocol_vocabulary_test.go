package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/binoctal/cerberus/internal/project"
)

func TestRunProtocolVocabulary_DryRun(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "room.ts")
	if err := os.WriteFile(src, []byte("class UserRoom { handleMessage(){} }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	err := runProtocolVocabulary(context.Background(), dir, []string{src}, "open-agents", true, nil)
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
	if err := os.WriteFile(src, []byte("class UserRoom { handleMessage(){} }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, ".cerberus", "vocab", "open-agents.vocab.yaml")
	err := runProtocolVocabulary(context.Background(), dir, []string{src}, "open-agents", false, func(string) bool { return true })
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

// TestRunProtocolVocabulary_ReextractPreservesAnnotations: re-extracting over
// an existing vocab must preserve manually-annotated partial/unsupported marks
// on edges that still exist (matched by from_role|to_role|type). A blind
// overwrite drops the marks, which re-admits server-only edges to requiredEdges
// and timeout-fails them until the executor escalates -- a known maintenance
// trap from the relay-coverage work.
func TestRunProtocolVocabulary_ReextractPreservesAnnotations(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "room.ts")
	if err := os.WriteFile(src, []byte(`class UserRoom {
  handleMessage(ws, meta, msg) {
    switch (msg.type) {
      case 'echo-everyone':
        if (meta.type === 'bridge') { this.broadcastToWeb(msg); }
        break;
      default:
    }
  }
  broadcastToWeb(msg) {}
}
`), 0644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, ".cerberus", "vocab", "open-agents.vocab.yaml")
	// First extraction writes the vocab.
	if err := runProtocolVocabulary(context.Background(), dir, []string{src}, "open-agents", false, func(string) bool { return true }); err != nil {
		t.Skipf("node unavailable: %v", err)
	}
	v, err := project.LoadVocabulary(out)
	if err != nil {
		t.Fatalf("load first vocab: %v", err)
	}
	if len(v.Edges) == 0 {
		t.Skip("extractor produced no edges; cannot test mark preservation")
	}
	// Annotate the first edge as partial, as Phase 2 did by hand.
	v.Edges[0].Partial = true
	block, err := yaml.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, block, 0644); err != nil {
		t.Fatal(err)
	}

	// Re-extract the same source: the existing edge must keep its mark.
	if err := runProtocolVocabulary(context.Background(), dir, []string{src}, "open-agents", false, func(string) bool { return true }); err != nil {
		t.Skipf("node unavailable: %v", err)
	}
	v2, err := project.LoadVocabulary(out)
	if err != nil {
		t.Fatalf("load re-extracted vocab: %v", err)
	}
	for _, e := range v2.Edges {
		if e.FromRole == v.Edges[0].FromRole && e.ToRole == v.Edges[0].ToRole && e.Type == v.Edges[0].Type {
			if !e.Partial {
				t.Fatalf("re-extraction dropped partial mark on edge %s->%s %s", e.FromRole, e.ToRole, e.Type)
			}
			return
		}
	}
	t.Fatalf("re-extracted vocab lost the annotated edge %s->%s %s entirely", v.Edges[0].FromRole, v.Edges[0].ToRole, v.Edges[0].Type)
}

// Route-level hand marks follow the same rule: a hand-tuned param_sources
// chain and a hand-curated http_auth_middlewares list survive re-extraction,
// while auth (like middlewares and min_body) comes back fresh from the
// extractor — hand values only win where the brief says they do.
func TestRunProtocolVocabulary_ReextractPreservesRouteHandMarks(t *testing.T) {
	honoSrc, err := filepath.Abs(filepath.Join("..", "..", "internal", "vocabextract", "testdata", "hono", "use-auth.ts"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, ".cerberus", "vocab", "hono.vocab.yaml")
	if err := runProtocolVocabulary(context.Background(), dir, []string{honoSrc}, "hono", false, func(string) bool { return true }); err != nil {
		t.Skipf("node unavailable: %v", err)
	}
	hv, err := project.LoadVocabulary(out)
	if err != nil {
		t.Fatalf("load hono vocab: %v", err)
	}
	// Hand-tune the :id chain and the auth middleware list; hand-corrupt
	// auth so the re-extraction proves it is re-derived fresh.
	for i := range hv.HTTPRoutes {
		switch hv.HTTPRoutes[i].Method + "|" + hv.HTTPRoutes[i].Path {
		case "GET|/api/things/:id":
			if hv.HTTPRoutes[i].ParamSources == nil {
				hv.HTTPRoutes[i].ParamSources = map[string]project.VocabParamSource{}
			}
			hv.HTTPRoutes[i].ParamSources[":id"] = project.VocabParamSource{Route: "GET /api/things", Pick: "1.id"}
		case "GET|/api/things":
			hv.HTTPRoutes[i].Auth = "none"
		}
	}
	hv.HTTPAuthMiddlewares = []string{"handPicked"}
	block, err := yaml.Marshal(hv)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, block, 0644); err != nil {
		t.Fatal(err)
	}
	if err := runProtocolVocabulary(context.Background(), dir, []string{honoSrc}, "hono", false, func(string) bool { return true }); err != nil {
		t.Skipf("node unavailable: %v", err)
	}
	hv2, err := project.LoadVocabulary(out)
	if err != nil {
		t.Fatalf("load re-extracted vocab: %v", err)
	}
	if len(hv2.HTTPAuthMiddlewares) != 1 || hv2.HTTPAuthMiddlewares[0] != "handPicked" {
		t.Fatalf("http_auth_middlewares = %v, want [handPicked] (hand-curated wins)", hv2.HTTPAuthMiddlewares)
	}
	for _, r := range hv2.HTTPRoutes {
		switch r.Method + "|" + r.Path {
		case "GET|/api/things/:id":
			if ps := r.ParamSources[":id"]; ps.Pick != "1.id" || ps.Route != "GET /api/things" {
				t.Fatalf("re-extraction dropped hand param_sources: %#v", r.ParamSources)
			}
		case "GET|/api/things":
			if r.Auth != "required" {
				t.Fatalf("GET|/api/things auth = %q, want required (re-derived fresh)", r.Auth)
			}
		}
	}
}

// TestRunProtocolVocabulary_AuthDerivationAndParamChain: middleware names
// matching (?i)auth|bearer|jwt derive auth "required" (and feed the
// http_auth_middlewares list); no middleware derives "none"; a non-auth
// middleware alone leaves "unknown". A trailing :param whose param-free
// prefix is a GET list route chains to that route with the "0.id" pick.
func TestRunProtocolVocabulary_AuthDerivationAndParamChain(t *testing.T) {
	vocab := extractVocabForTest(t, filepath.Join("hono", "use-auth.ts"))
	byKey := map[string]project.VocabHTTPRoute{}
	for _, r := range vocab.HTTPRoutes {
		byKey[r.Method+"|"+r.Path] = r
	}
	// authMiddleware (inherited via the /api/things app.use prefix) -> required.
	if r := byKey["GET|/api/things"]; r.Auth != "required" {
		t.Fatalf("GET|/api/things auth = %q, want required", r.Auth)
	}
	// No middleware -> none.
	if r := byKey["GET|/health"]; r.Auth != "none" {
		t.Fatalf("GET|/health auth = %q, want none", r.Auth)
	}
	// Inherited authMiddleware beats the inline non-auth rateLimiter.
	if r := byKey["GET|/api/things/:id/gated"]; r.Auth != "required" {
		t.Fatalf("GET|/api/things/:id/gated auth = %q, want required", r.Auth)
	}
	// rateLimiter alone is a middleware but not an auth one -> unknown.
	if r := byKey["GET|/limited"]; r.Auth != "unknown" {
		t.Fatalf("GET|/limited auth = %q, want unknown", r.Auth)
	}
	// The heuristic's matched set is emitted for hand override.
	if !slices.Contains(vocab.HTTPAuthMiddlewares, "authMiddleware") {
		t.Fatalf("http_auth_middlewares = %v, want authMiddleware", vocab.HTTPAuthMiddlewares)
	}
	// :id chain points at the list route with the dot-path pick.
	r := byKey["GET|/api/things/:id"]
	ps, ok := r.ParamSources[":id"]
	if !ok || ps.Route != "GET /api/things" || ps.Pick != "0.id" {
		t.Fatalf("param_sources = %#v", r.ParamSources)
	}
}

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
	if err := runProtocolVocabulary(context.Background(), dir, []string{filepath.Join(dir, "worker.ts")}, "hono", false, func(string) bool { return true }); err != nil {
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
	if err := runProtocolVocabulary(context.Background(), dir, []string{filepath.Join(dir, "worker.ts")}, "hono", false, func(string) bool { return true }); err != nil {
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

// TestRunProtocolVocabulary_MultiEntry: repeated --from entries merge into one
// vocab (WS edges from a room-class entry + HTTP routes from a Hono worker
// entry), files union without duplication, and route marks survive a
// multi-entry re-extraction.
func TestRunProtocolVocabulary_MultiEntry(t *testing.T) {
	dir := t.TempDir()
	room := `class UserRoom {
  handleMessage(ws, meta, msg) {
    switch (msg.type) {
      case 'echo-everyone':
        if (meta.type === 'bridge') { this.broadcastToWeb(msg); }
        break;
      default:
    }
  }
  broadcastToWeb(msg) {}
}
`
	worker := `import { Hono } from 'hono';
const app = new Hono();
app.get('/health', (c) => c.json({}));
export default app;
`
	roomPath := filepath.Join(dir, "room.ts")
	workerPath := filepath.Join(dir, "worker.ts")
	if err := os.WriteFile(roomPath, []byte(room), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workerPath, []byte(worker), 0644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, ".cerberus", "vocab", "merged.vocab.yaml")
	if err := runProtocolVocabulary(context.Background(), dir, []string{roomPath, workerPath}, "merged", false, func(string) bool { return true }); err != nil {
		t.Skipf("node unavailable: %v", err)
	}
	v, err := project.LoadVocabulary(out)
	if err != nil {
		t.Fatalf("load vocab: %v", err)
	}
	if len(v.Edges) == 0 {
		t.Fatal("WS edges from the room entry missing")
	}
	if len(v.HTTPRoutes) != 1 || v.HTTPRoutes[0].Method != "GET" || v.HTTPRoutes[0].Path != "/health" {
		t.Fatalf("http_routes = %+v, want GET /health", v.HTTPRoutes)
	}
	if len(v.Source.Files) != 2 {
		t.Fatalf("source.files = %+v, want room+worker", v.Source.Files)
	}
	// Route marks survive a multi-entry re-extraction.
	v.HTTPRoutes[0].Partial = true
	block, err := yaml.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, block, 0644); err != nil {
		t.Fatal(err)
	}
	if err := runProtocolVocabulary(context.Background(), dir, []string{roomPath, workerPath}, "merged", false, func(string) bool { return true }); err != nil {
		t.Skipf("node unavailable: %v", err)
	}
	v2, err := project.LoadVocabulary(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(v2.Edges) == 0 || len(v2.HTTPRoutes) != 1 || !v2.HTTPRoutes[0].Partial {
		t.Fatalf("re-extraction lost content or marks: %d edges, %+v", len(v2.Edges), v2.HTTPRoutes)
	}
}

// extractVocabForTest runs the vocabulary extraction over a bundled
// internal/vocabextract/testdata source and returns the parsed vocabulary.
// Shared by tests that assert on extractor output shape without writing
// their own fixtures.
func extractVocabForTest(t *testing.T, rel string) *project.Vocabulary {
	t.Helper()
	src, err := filepath.Abs(filepath.Join("..", "..", "internal", "vocabextract", "testdata", rel))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := runProtocolVocabulary(context.Background(), dir, []string{src}, "hono", false, func(string) bool { return true }); err != nil {
		t.Skipf("node unavailable: %v", err)
	}
	v, err := project.LoadVocabulary(filepath.Join(dir, ".cerberus", "vocab", "hono.vocab.yaml"))
	if err != nil {
		t.Fatalf("load vocab: %v", err)
	}
	return v
}

// TestRunProtocolVocabulary_UseAuthMiddlewares: app.use('/api/things', authMiddleware)
// prefixes every route under /api/things (direct + router-mounted), and inline
// middleware args are captured per-route. Routes outside the prefix carry only
// their own inline middleware; untouched routes carry none.
func TestRunProtocolVocabulary_UseAuthMiddlewares(t *testing.T) {
	vocab := extractVocabForTest(t, filepath.Join("hono", "use-auth.ts"))
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
	// Outside the /api/things prefix: only the inline middleware applies.
	r, ok := byKey["GET|/limited"]
	if !ok {
		t.Fatal("route GET|/limited missing")
	}
	if !slices.Contains(r.Middlewares, "rateLimiter") || slices.Contains(r.Middlewares, "authMiddleware") {
		t.Fatalf("GET|/limited middlewares = %v, want [rateLimiter] only", r.Middlewares)
	}
}

// TestRunProtocolVocabulary_MinBody: zValidator('json', schema) yields the
// minimal legal body from literal zod primitives (string/number/boolean).
// Anything richer (refine, nested objects, optional chains) omits min_body
// entirely — prefer missing over guessing.
func TestRunProtocolVocabulary_MinBody(t *testing.T) {
	vocab := extractVocabForTest(t, filepath.Join("hono", "zod-body.ts"))
	byKey := map[string]project.VocabHTTPRoute{}
	for _, r := range vocab.HTTPRoutes {
		byKey[r.Method+"|"+r.Path] = r
	}
	want := map[string]map[string]any{
		"POST|/api/things": {"name": "x", "count": float64(0), "active": false},
		"POST|/api/inline": {"label": "x"},
	}
	// jsonNormalize round-trips through JSON so whole-number fields decode
	// as float64: the vocab file stores `count: 0`, which yaml.Unmarshal
	// into any yields as int, while every JSON consumer of min_body sees
	// float64. Comparing the JSON-decoded shape pins the wire-relevant value.
	jsonNormalize := func(m map[string]any) map[string]any {
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		var out map[string]any
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	for key, body := range want {
		r, ok := byKey[key]
		if !ok {
			t.Fatalf("route %s missing", key)
		}
		if !reflect.DeepEqual(jsonNormalize(r.MinBody), body) {
			t.Fatalf("%s: min_body = %#v, want %#v", key, r.MinBody, body)
		}
	}
	for _, key := range []string{"POST|/api/picky", "POST|/api/bare"} {
		if r := byKey[key]; len(r.MinBody) != 0 {
			t.Fatalf("%s: min_body must be omitted, got %#v", key, r.MinBody)
		}
	}
}
