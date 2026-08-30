package project

import "testing"

// TestDogfoodVocabMinQueryLoads sanity-loads the dogfood vocabulary with the
// new min_query field populated (hand-curated marks for the run37 400 family).
func TestDogfoodVocabMinQueryLoads(t *testing.T) {
	v, err := LoadVocabulary("../../dogfood/realtime-e2e/.cerberus/vocab/open-agents.vocab.yaml")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]map[string]string{
		"GET|/api/admin/ai-comparison/compare": {"models": "gpt-4o,claude-3", "metrics": "accuracy,latency"},
		"GET|/api/admin/blacklist/check":       {"type": "ip", "value": "192.0.2.1"},
	}
	found := 0
	for _, r := range v.HTTPRoutes {
		w, ok := want[r.Method+"|"+r.Path]
		if !ok {
			if len(r.MinQuery) > 0 {
				t.Errorf("%s %s: unexpected min_query %v", r.Method, r.Path, r.MinQuery)
			}
			continue
		}
		found++
		if len(r.MinQuery) != len(w) {
			t.Errorf("%s %s: min_query = %v, want %v", r.Method, r.Path, r.MinQuery, w)
		}
		for k, vv := range w {
			if r.MinQuery[k] != vv {
				t.Errorf("%s %s: min_query[%s] = %q, want %q", r.Method, r.Path, k, r.MinQuery[k], vv)
			}
		}
	}
	if found != len(want) {
		t.Fatalf("marked routes found = %d, want %d", found, len(want))
	}
	// The hand-curated role-map addition survived in the same file.
	for _, rr := range v.HTTPRoleRoutes {
		if rr.Prefix == "/api/sessions/admin" && rr.Role == "admin" {
			return
		}
	}
	t.Errorf("role map lost the /api/sessions/admin entry: %+v", v.HTTPRoleRoutes)
}
