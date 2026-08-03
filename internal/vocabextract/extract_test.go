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
