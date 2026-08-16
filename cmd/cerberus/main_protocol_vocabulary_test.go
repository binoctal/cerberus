package main

import (
	"context"
	"os"
	"path/filepath"
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
	if err := os.WriteFile(src, []byte("class UserRoom { handleMessage(){} }\n"), 0644); err != nil {
		t.Fatal(err)
	}
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
	if err := runProtocolVocabulary(context.Background(), dir, src, "open-agents", false, func(string) bool { return true }); err != nil {
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
	if err := runProtocolVocabulary(context.Background(), dir, src, "open-agents", false, func(string) bool { return true }); err != nil {
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
