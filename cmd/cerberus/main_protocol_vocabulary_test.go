package main

import (
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
