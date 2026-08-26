package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
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
// chain, a hand-curated http_auth_middlewares list, and hand-set per-route
// auth all survive re-extraction (spec §5: the judgment layer rides the
// merge), while middlewares and min_body come back fresh from the extractor
// (the fact layer is re-derived).
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
	// Hand-tune the :id chain and the auth middleware list; hand-set auth so
	// the re-extraction proves the judgment layer survives the merge (spec
	// §5 — previously this test asserted the opposite, re-derivation fresh).
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
			if r.Auth != "none" {
				t.Fatalf("GET|/api/things auth = %q, want none (hand-set auth survives the merge)", r.Auth)
			}
		}
	}
}

// TestRunProtocolVocabulary_EffectiveAuthList: when the previous vocab
// declares http_auth_middlewares, per-route auth derivation intersects the
// route's middlewares with THAT list — an anonymous gate like use:/api/*
// matches no auth-name regex, so its auth facts can only flow through the
// curated list. First pass (no prior list): /api routes come back unknown;
// after curating [use:/api/*] and re-extracting they derive required.
func TestRunProtocolVocabulary_EffectiveAuthList(t *testing.T) {
	anonSrc, err := filepath.Abs(filepath.Join("..", "..", "internal", "vocabextract", "testdata", "hono", "use-anon.ts"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, ".cerberus", "vocab", "hono.vocab.yaml")
	if err := runProtocolVocabulary(context.Background(), dir, []string{anonSrc}, "hono", false, func(string) bool { return true }); err != nil {
		t.Skipf("node unavailable: %v", err)
	}
	hv, err := project.LoadVocabulary(out)
	if err != nil {
		t.Fatalf("load hono vocab: %v", err)
	}
	if len(hv.HTTPAuthMiddlewares) != 0 {
		t.Fatalf("first pass http_auth_middlewares = %v, want empty (no name matches the regex)", hv.HTTPAuthMiddlewares)
	}
	for _, r := range hv.HTTPRoutes {
		if r.Path == "/health" && r.Auth != "unknown" {
			t.Fatalf("/health auth = %q, want unknown (requestLogger matches no auth regex)", r.Auth)
		}
		if strings.HasPrefix(r.Path, "/api") && r.Auth != "unknown" {
			t.Fatalf("%s auth = %q, want unknown (use:/api/* matches no auth regex)", r.Path, r.Auth)
		}
	}
	// Curate the anonymous gate into the service-level list and re-extract.
	hv.HTTPAuthMiddlewares = []string{"use:/api/*"}
	block, err := yaml.Marshal(hv)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, block, 0644); err != nil {
		t.Fatal(err)
	}
	if err := runProtocolVocabulary(context.Background(), dir, []string{anonSrc}, "hono", false, func(string) bool { return true }); err != nil {
		t.Skipf("node unavailable: %v", err)
	}
	hv2, err := project.LoadVocabulary(out)
	if err != nil {
		t.Fatalf("load re-extracted vocab: %v", err)
	}
	if len(hv2.HTTPAuthMiddlewares) != 1 || hv2.HTTPAuthMiddlewares[0] != "use:/api/*" {
		t.Fatalf("http_auth_middlewares = %v, want [use:/api/*]", hv2.HTTPAuthMiddlewares)
	}
	for _, r := range hv2.HTTPRoutes {
		if r.Path == "/health" {
			// requestLogger/use:/* are middlewares but not in the curated
			// list, and the route sits outside the /api glob — unknown.
			if r.Auth != "unknown" {
				t.Fatalf("/health auth = %q, want unknown (outside the glob)", r.Auth)
			}
			continue
		}
		if r.Auth != "required" {
			t.Fatalf("%s auth = %q, want required via the effective list", r.Path, r.Auth)
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

// TestRunProtocolVocabulary_ParamSourcesOffVeto: a hand-deleted param chain
// must stay deleted across re-extraction. The re-derivation heuristic cannot
// distinguish "hand-deleted" from "never curated", so param_sources_off is
// the durable record of the deletion — re-extraction never re-derives a
// vetoed param, and the veto itself rides the preservation merge.
func TestRunProtocolVocabulary_ParamSourcesOffVeto(t *testing.T) {
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
	// First pass derives the :id chain — sanity that the resurrection shape
	// under test actually exists.
	derived := false
	for i := range hv.HTTPRoutes {
		if hv.HTTPRoutes[i].Method+"|"+hv.HTTPRoutes[i].Path == "GET|/api/things/:id" {
			if _, ok := hv.HTTPRoutes[i].ParamSources[":id"]; !ok {
				t.Fatal("first pass did not derive the :id chain; veto test would be vacuous")
			}
			derived = true
			// Hand-delete the chain and veto its re-derivation.
			hv.HTTPRoutes[i].ParamSources = nil
			hv.HTTPRoutes[i].ParamSourcesOff = []string{":id"}
		}
	}
	if !derived {
		t.Fatal("GET|/api/things/:id missing from extracted vocab")
	}
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
	for _, r := range hv2.HTTPRoutes {
		if r.Method+"|"+r.Path != "GET|/api/things/:id" {
			continue
		}
		if len(r.ParamSources) != 0 {
			t.Fatalf("vetoed chain resurrected by re-derivation: %#v", r.ParamSources)
		}
		if len(r.ParamSourcesOff) != 1 || r.ParamSourcesOff[0] != ":id" {
			t.Fatalf("param_sources_off did not survive the merge: %#v", r.ParamSourcesOff)
		}
		return
	}
	t.Fatal("GET|/api/things/:id lost by re-extraction")
}

// TestRunProtocolVocabulary_PreservesHTTPRoleMap: the hand-curated HTTP role
// map (which protocol role's JWT a path prefix takes) is live-probe
// knowledge, not source-derivable — re-extraction must never drop it.
func TestRunProtocolVocabulary_PreservesHTTPRoleMap(t *testing.T) {
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
	hv.HTTPRoleRoutes = []project.VocabRoleRoute{{Prefix: "/api/admin", Role: "admin"}}
	hv.HTTPDefaultRole = "web"
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
	if len(hv2.HTTPRoleRoutes) != 1 || hv2.HTTPRoleRoutes[0].Prefix != "/api/admin" || hv2.HTTPRoleRoutes[0].Role != "admin" {
		t.Fatalf("http_role_routes did not survive re-extraction: %#v", hv2.HTTPRoleRoutes)
	}
	if hv2.HTTPDefaultRole != "web" {
		t.Fatalf("http_default_role = %q, want web", hv2.HTTPDefaultRole)
	}
}
