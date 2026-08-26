package project

import (
	"os"
	"path/filepath"
	"strings"
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

func TestValidateVocabulary_HTTPRoutes(t *testing.T) {
	ok := &Vocabulary{HTTPRoutes: []VocabHTTPRoute{
		{Method: "POST", Path: "/api/sessions"},
		{Method: "GET", Path: "/api/sessions/:id"},
		{Method: "ALL", Path: "/api/workflows/jobs/*"},
	}}
	if err := ValidateVocabulary(ok); err != nil {
		t.Fatalf("valid routes rejected: %v", err)
	}
	bad := []VocabHTTPRoute{
		{Method: "FETCH", Path: "/x"},   // method not in enum
		{Method: "GET", Path: "x"},      // no leading slash
		{Method: "GET", Path: "/a//b"},  // double slash
		{Method: "GET", Path: "/a/*/b"}, // * must be the final segment
		{Method: "GET", Path: "/a/:/b"}, // empty param name
	}
	for _, r := range bad {
		if err := ValidateVocabulary(&Vocabulary{HTTPRoutes: []VocabHTTPRoute{r}}); err == nil {
			t.Errorf("route %+v: want validation error, got nil", r)
		}
	}
}

func TestLoadVocabulary_RejectsBadRoute(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "v.vocab.yaml")
	body := "source:\n  files: []\nhttp_routes:\n  - method: FETCH\n    path: /x\n"
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadVocabulary(p); err == nil {
		t.Fatal("LoadVocabulary accepted an invalid http_route (broken denominator must not pass silently)")
	}
}

func TestValidateVocabularyRouteFacts(t *testing.T) {
	base := func(mut func(*Vocabulary)) *Vocabulary {
		v := &Vocabulary{HTTPRoutes: []VocabHTTPRoute{
			{
				Method: "GET", Path: "/api/devices/:id",
				Middlewares: []string{"authMiddleware"}, Auth: "required",
				ParamSources: map[string]VocabParamSource{
					":id": {Route: "GET /api/devices", Pick: "0.id"},
				},
			},
			// The list route param_sources resolve against: it must be part of
			// the vocabulary for routeSet to resolve "GET /api/devices".
			{Method: "GET", Path: "/api/devices"},
		}}
		mut(v)
		return v
	}
	cases := []struct {
		name string
		mut  func(*Vocabulary)
		want string
	}{
		{"valid", func(*Vocabulary) {}, ""},
		{"bad auth enum", func(v *Vocabulary) {
			v.HTTPRoutes[0].Auth = "maybe"
		}, `auth "maybe" not in enum`},
		{"param source key not in path", func(v *Vocabulary) {
			v.HTTPRoutes[0].ParamSources = map[string]VocabParamSource{
				":other": {Route: "GET /api/devices", Pick: "0.id"}}
		}, ":other"},
		{"param source route unresolved", func(v *Vocabulary) {
			v.HTTPRoutes[0].ParamSources = map[string]VocabParamSource{
				":id": {Route: "GET /api/nope", Pick: "0.id"}}
		}, `unresolved list route "GET /api/nope"`},
		{"param route method mismatch", func(v *Vocabulary) {
			v.HTTPRoutes[0].ParamSources[":id"] = VocabParamSource{
				Route: "POST /api/devices", Pick: "0.id"}
		}, `unresolved list route "POST /api/devices"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateVocabulary(base(tc.mut))
			if tc.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

// TestValidateVocabulary_ParamSourcesOffAndRoleMap: param_sources_off
// entries follow the same :param containment rule as param_sources keys and
// must not overlap a live source; the declarable HTTP role map is
// shape-validated (prefix starts with /, role non-empty) — role existence
// is a generation-time check where the protocol is available.
func TestValidateVocabulary_ParamSourcesOffAndRoleMap(t *testing.T) {
	base := func() *Vocabulary {
		return &Vocabulary{HTTPRoutes: []VocabHTTPRoute{
			{
				Method: "GET", Path: "/api/export/:type", Auth: "required",
				ParamSourcesOff: []string{":type"},
			},
			{Method: "GET", Path: "/api/export"},
		}}
	}
	if err := ValidateVocabulary(base()); err != nil {
		t.Fatalf("valid veto rejected: %v", err)
	}
	bad := []struct {
		name string
		mut  func(*Vocabulary)
		want string
	}{
		{"veto entry not a :param", func(v *Vocabulary) {
			v.HTTPRoutes[0].ParamSourcesOff = []string{":nope"}
		}, ":nope"},
		{"veto overlaps a source", func(v *Vocabulary) {
			v.HTTPRoutes[0].ParamSources = map[string]VocabParamSource{
				":type": {Route: "GET /api/export", Pick: "0.id"}}
		}, "both vetoed"},
		{"role route prefix without slash", func(v *Vocabulary) {
			v.HTTPRoleRoutes = []VocabRoleRoute{{Prefix: "api/admin", Role: "admin"}}
		}, "must start with /"},
		{"role route empty role", func(v *Vocabulary) {
			v.HTTPRoleRoutes = []VocabRoleRoute{{Prefix: "/api/admin"}}
		}, "role is required"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			v := base()
			tc.mut(v)
			err := ValidateVocabulary(v)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
	// A declared map with a default is the sanctioned shape.
	v := base()
	v.HTTPRoleRoutes = []VocabRoleRoute{{Prefix: "/api/admin", Role: "admin"}}
	v.HTTPDefaultRole = "web"
	if err := ValidateVocabulary(v); err != nil {
		t.Fatalf("valid role map rejected: %v", err)
	}
}
