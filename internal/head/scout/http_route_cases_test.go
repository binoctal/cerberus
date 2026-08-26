package scout

import (
	"fmt"
	"strings"
	"testing"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// TestHTTPRouteCases_TiersAndOrdering: one case per non-exempt route, sorted
// by path then method, with honesty-tier expectations and placeholder params.
func TestHTTPRouteCases_TiersAndOrdering(t *testing.T) {
	svc := project.Service{
		Name: "realtime",
		URL:  "http://localhost:8989/ws/{userId}",
		Vocabulary: &project.Vocabulary{HTTPRoutes: []project.VocabHTTPRoute{
			{Method: "GET", Path: "/api/health"},
			{Method: "POST", Path: "/api/sessions"},
			{Method: "GET", Path: "/api/admin/stats"},
			{Method: "DELETE", Path: "/api/sessions/:id"},
			{Method: "ALL", Path: "/api/workflows/jobs/*"},
			{Method: "GET", Path: "/api/skipped", Unsupported: true},
			{Method: "PUT", Path: "/api/skipped2", Partial: true},
		}},
	}
	cases := httpRouteCases(svc)
	if len(cases) != 5 {
		t.Fatalf("cases = %d, want 5 (exempt routes skipped): %+v", len(cases), cases)
	}
	// Sorted: /api/admin/stats, /api/health, /api/sessions, /api/sessions/:id, /api/workflows/jobs/*
	wantOrder := []string{"GET /api/admin/stats", "GET /api/health", "POST /api/sessions", "DELETE /api/sessions/1", "GET /api/workflows/jobs/x"}
	for i, w := range wantOrder {
		got := cases[i].Steps[0].Method + " " + cases[i].Steps[0].URL[len("http://localhost:8989"):]
		if got != w {
			t.Errorf("case[%d] = %q, want %q", i, got, w)
		}
	}
	if cases[0].Steps[0].URL != "http://localhost:8989/api/admin/stats" {
		t.Errorf("admin url = %q", cases[0].Steps[0].URL)
	}
	if cases[3].Steps[0].URL != "http://localhost:8989/api/sessions/1" {
		t.Errorf("param url = %q (placeholder fill)", cases[3].Steps[0].URL)
	}
	// Honesty tiers.
	if cases[1].Expectation != "route reachable over HTTP (any status response; no transport error)" {
		t.Errorf("flat expectation = %q", cases[1].Expectation)
	}
	if cases[0].Expectation == cases[1].Expectation {
		t.Errorf("admin route must carry an admin tier distinct from flat: %q", cases[0].Expectation)
	}
	if cases[0].Steps[0].AuthRole != "" {
		t.Errorf("no admin protocol role declared: AuthRole must stay empty, got %q", cases[0].Steps[0].AuthRole)
	}
	if cases[3].Expectation == cases[1].Expectation {
		t.Errorf("param route must carry the placeholder tier: %q", cases[3].Expectation)
	}
	for _, c := range cases {
		if c.Steps[0].ExpectStatusClass != "any" {
			t.Errorf("%s: class = %q, want any", c.ID, c.Steps[0].ExpectStatusClass)
		}
		if c.Steps[0].AuthRole != "" {
			t.Errorf("%s: bare client expected (no AuthRole)", c.ID)
		}
		if len(c.Claims) != 0 {
			t.Errorf("%s: route smokes must not bind claims", c.ID)
		}
	}
	// ALL collapses to GET.
	if cases[4].Steps[0].Method != "GET" || !contains(cases[4].ID, "get") {
		t.Errorf("ALL route: method/ID = %q", cases[4].Steps[0].Method)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestHTTPRouteCases_AdminRoleInjection: with an "admin" protocol role
// declared, admin-path routes carry AuthRole=admin (JWT injection) while
// flat routes stay bare.
func TestHTTPRouteCases_AdminRoleInjection(t *testing.T) {
	svc := project.Service{
		Name: "realtime",
		URL:  "http://localhost:8989/ws/{userId}",
		Protocol: &project.Protocol{Roles: map[string]*project.ProtocolRole{
			"admin": {CredentialRef: "admin-actor"},
		}},
		Vocabulary: &project.Vocabulary{HTTPRoutes: []project.VocabHTTPRoute{
			{Method: "GET", Path: "/api/admin/stats"},
			{Method: "GET", Path: "/api/health"},
		}},
	}
	cases := httpRouteCases(svc)
	if len(cases) != 2 {
		t.Fatalf("cases = %d", len(cases))
	}
	var admin, flat *agent.TestCase
	for i := range cases {
		if isAdminPath(cases[i].Steps[0].URL) {
			admin = &cases[i]
		} else {
			flat = &cases[i]
		}
	}
	if admin.Steps[0].AuthRole != "admin" {
		t.Errorf("admin route AuthRole = %q, want admin", admin.Steps[0].AuthRole)
	}
	if flat.Steps[0].AuthRole != "" {
		t.Errorf("flat route AuthRole = %q, want empty", flat.Steps[0].AuthRole)
	}
	if !contains(admin.Expectation, "admin JWT injected") {
		t.Errorf("admin expectation = %q", admin.Expectation)
	}
}

// TestHTTPRouteCases_NoVocab: services without http_routes emit nothing.
func TestHTTPRouteCases_NoVocab(t *testing.T) {
	if got := httpRouteCases(project.Service{Name: "x", URL: "http://h"}); got != nil {
		t.Errorf("no-vocab service emitted %d cases", len(got))
	}
}

// routeV2Fixture builds a service whose vocab carries the v2 facts (auth,
// min_body, param_sources) plus both degradation shapes: an auth:none route
// and a :param route with no param source.
func routeV2Fixture() project.Service {
	return project.Service{
		Name: "realtime",
		URL:  "http://localhost:8989/ws/{userId}",
		Protocol: &project.Protocol{Roles: map[string]*project.ProtocolRole{
			"admin": {CredentialRef: "admin-actor"},
			"web":   {CredentialRef: "web-actor"},
		}},
		Vocabulary: &project.Vocabulary{HTTPRoutes: []project.VocabHTTPRoute{
			{Method: "GET", Path: "/api/things", Auth: "required"},
			{Method: "POST", Path: "/api/things", Auth: "required",
				MinBody: map[string]any{"name": "x"}},
			{Method: "GET", Path: "/api/things/:id", Auth: "required",
				ParamSources: map[string]project.VocabParamSource{
					":id": {Route: "GET /api/things", Pick: "0.id"},
				}},
			{Method: "GET", Path: "/health", Auth: "none"},
			{Method: "GET", Path: "/api/mystery/:id", Auth: "required"},
			{Method: "GET", Path: "/api/admin/stats", Auth: "required"},
		}},
	}
}

// caseID mirrors the generator's ID scheme: the v1 reachability base ID plus
// a v2 tier suffix ("-unauth" | "-authed" | "" for reachability).
func caseID(svc project.Service, method, path, suffix string) string {
	id := fmt.Sprintf("http-route-%s-%s-%s", svc.Name, strings.ToLower(method), trimPathForID(fillRouteParams(path)))
	if suffix != "" {
		id += "-" + suffix
	}
	return id
}

// TestHTTPRouteCasesV2: auth-fact-driven tiers — unauth 4xx probes, authed
// 2xx with param chaining and minimal bodies, reachability degradation for
// unresolvable :param routes and pre-v2 auth shapes.
func TestHTTPRouteCasesV2(t *testing.T) {
	svc := routeV2Fixture()
	cases := httpRouteCases(svc)
	byID := map[string]agent.TestCase{}
	for _, c := range cases {
		byID[c.ID] = c
	}
	// authed GET, param-free: single step, 2xx.
	c := byID[caseID(svc, "GET", "/api/things", "authed")]
	if len(c.Steps) != 1 || c.Steps[0].ExpectStatusClass != "2xx" || c.Steps[0].AuthRole != "web" {
		t.Fatalf("authed GET shape wrong: %+v", c.Steps)
	}
	// unauth probe on the same route: 4xx, no AuthRole.
	c = byID[caseID(svc, "GET", "/api/things", "unauth")]
	if len(c.Steps) != 1 || c.Steps[0].ExpectStatusClass != "4xx" || c.Steps[0].AuthRole != "" {
		t.Fatalf("unauth shape wrong: %+v", c.Steps)
	}
	// authed mutation: body + 2xx_4xx.
	c = byID[caseID(svc, "POST", "/api/things", "authed")]
	if len(c.Steps) != 1 || c.Steps[0].ExpectStatusClass != "2xx_4xx" || c.Steps[0].Body != `{"name":"x"}` {
		t.Fatalf("mutation shape wrong: %+v", c.Steps)
	}
	// param chain: two steps — capture then assert.
	c = byID[caseID(svc, "GET", "/api/things/:id", "authed")]
	if len(c.Steps) != 2 {
		t.Fatalf("param chain must be 2 steps, got %d", len(c.Steps))
	}
	if c.Steps[0].Capture["0.id"] == "" || !strings.Contains(c.Steps[1].URL, "{{case.") {
		t.Fatalf("capture/placeholder wiring wrong: %+v", c.Steps)
	}
	if c.Steps[1].URL != "http://localhost:8989/api/things/{{case.p_id}}" {
		t.Fatalf("param placeholder url = %q", c.Steps[1].URL)
	}
	// degradation: no param_sources -> reachability tier only (any, placeholder 1).
	if _, ok := byID[caseID(svc, "GET", "/api/mystery/:id", "authed")]; ok {
		t.Fatalf("unresolvable param route must NOT emit an authed case")
	}
	if c := byID[caseID(svc, "GET", "/api/mystery/:id", "")]; c.Steps[0].ExpectStatusClass != "any" {
		t.Fatalf("degraded route must stay reachability: %+v", c.Steps)
	}
	// auth none: single reachability case, no unauth twin.
	if _, ok := byID[caseID(svc, "GET", "/health", "unauth")]; ok {
		t.Fatalf("auth:none route must not get an unauth twin")
	}
	if _, ok := byID[caseID(svc, "GET", "/health", "authed")]; ok {
		t.Fatalf("auth:none route must not get an authed twin")
	}
	// admin prefix -> admin role.
	if c := byID[caseID(svc, "GET", "/api/admin/stats", "authed")]; c.Steps[0].AuthRole != "admin" {
		t.Fatalf("admin route must inject admin role, got %q", c.Steps[0].AuthRole)
	}
}

// TestHTTPRouteCases_WiredIntoWSCases: the generator rides WSCases for a
// service with routes (violations+routes both present).
func TestHTTPRouteCases_WiredIntoWSCases(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{
		Name: "realtime",
		URL:  "http://localhost:8989/ws/{userId}",
		Vocabulary: &project.Vocabulary{HTTPRoutes: []project.VocabHTTPRoute{
			{Method: "GET", Path: "/api/health"},
		}},
	}}}
	all := WSCases(cfg, "")
	var found *agent.TestCase
	for i := range all {
		if all[i].ID == "http-route-realtime-get-api-health" {
			found = &all[i]
		}
	}
	if found == nil {
		t.Fatalf("route case not emitted by WSCases; got %d cases", len(all))
	}
	if found.Steps[0].ExpectStatusClass != "any" {
		t.Errorf("wired case class = %q", found.Steps[0].ExpectStatusClass)
	}
}
