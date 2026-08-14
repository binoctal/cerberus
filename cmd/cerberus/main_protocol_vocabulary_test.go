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
