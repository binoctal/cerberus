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
