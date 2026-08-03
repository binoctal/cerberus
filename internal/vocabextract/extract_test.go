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

func TestExtract_FallThrough(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns node")
	}
	check := func(t *testing.T, out []byte, want ...string) {
		t.Helper()
		var got struct{ Edges []map[string]any }
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, e := range got.Edges {
			if v, _ := e["type"].(string); v != "" {
				seen[v] = true
			}
		}
		for _, w := range want {
			if !seen[w] {
				t.Errorf("missing edge type %q in %d edges", w, len(got.Edges))
			}
		}
	}
	out, err := Extract(context.Background(), filepath.Join("testdata", "switch-fallthrough.ts"))
	if err != nil {
		t.Skipf("node: %v", err)
	}
	check(t, out, "encrypted", "session:created", "session:started", "workflow:task_progress")

	out, err = Extract(context.Background(), filepath.Join("testdata", "sendtobridge.ts"))
	if err != nil {
		t.Skipf("node: %v", err)
	}
	var got struct{ Edges []map[string]any }
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range got.Edges {
		if e["type"] == "session:start" &&
			e["from_role"] == "web" && e["to_role"] == "bridge" &&
			e["trigger"] == "message_handled" &&
			e["guard"] == "meta.type === 'web'" {
			found = true
		}
	}
	if !found {
		t.Errorf("no web->bridge session:start edge: %+v", got.Edges)
	}
}

func TestExtract_SideEffectsAndBatch(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns node")
	}
	out, err := Extract(context.Background(), filepath.Join("testdata", "sideeffect.ts"))
	if err != nil {
		t.Skipf("node: %v", err)
	}
	var got struct {
		Edges []struct {
			Type        string `json:"type"`
			SideEffects []struct {
				Kind      string   `json:"kind"`
				WhenTypes []string `json:"when_types"`
			} `json:"side_effects"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	var progress *struct {
		Type        string `json:"type"`
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
	if progress == nil {
		t.Fatal("no workflow:task_progress edge")
	}
	if len(progress.SideEffects) != 1 || progress.SideEffects[0].Kind != "notify_orchestrator" {
		t.Errorf("side_effects = %+v", progress.SideEffects)
	}

	out, err = Extract(context.Background(), filepath.Join("testdata", "batch.ts"))
	if err != nil {
		t.Skipf("node: %v", err)
	}
	var b struct {
		Edges []struct {
			Type    string `json:"type"`
			Partial bool   `json:"partial"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(out, &b); err != nil {
		t.Fatal(err)
	}
	var anyPartial bool
	for _, e := range b.Edges {
		if e.Partial {
			anyPartial = true
		}
	}
	if !anyPartial {
		t.Errorf("no partial edge emitted for batch fixture: %+v", b.Edges)
	}
}
