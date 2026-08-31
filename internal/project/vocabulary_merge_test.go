package project

import "testing"

func TestMergeVocabJudgmentPreservesCrossMarks(t *testing.T) {
	prev := &Vocabulary{
		HTTPCrossRole:  "web-rival",
		HTTPRoleRoutes: []VocabRoleRoute{{Prefix: "/api/admin", Role: "admin"}},
		HTTPRoutes: []VocabHTTPRoute{
			{Method: "GET", Path: "/api/teams/:id", CrossExempt: true, MinQuery: map[string]string{"x": "1"}},
		},
	}
	fresh := &Vocabulary{
		HTTPRoutes: []VocabHTTPRoute{
			{Method: "GET", Path: "/api/teams/:id"}, // re-derived, no marks
			{Method: "GET", Path: "/api/sessions/:id"},
		},
	}
	MergeVocabJudgment(prev, fresh)
	if fresh.HTTPCrossRole != "web-rival" {
		t.Fatalf("http_cross_role = %q, want preserved web-rival", fresh.HTTPCrossRole)
	}
	if fresh.HTTPRoleRoutes == nil || len(fresh.HTTPRoleRoutes) != 1 {
		t.Fatalf("role routes not preserved: %+v", fresh.HTTPRoleRoutes)
	}
	if !fresh.HTTPRoutes[0].CrossExempt {
		t.Fatal("cross_exempt not preserved on re-derived route")
	}
	if fresh.HTTPRoutes[1].CrossExempt {
		t.Fatal("cross_exempt must not leak onto routes the prev vocab never marked")
	}
}
