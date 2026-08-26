package scout

import (
	"testing"

	"github.com/binoctal/cerberus/internal/project"
)

func TestUIVocabCases(t *testing.T) {
	svc := project.Service{Name: "open-agents", URL: "ws://localhost:8989/ws"}
	svc.Vocabulary = &project.Vocabulary{UI: &project.VocabUI{
		BaseURL: "http://localhost:5183", Locale: "en",
		Assertions: []project.VocabUIAssertion{
			{ID: "missions-conn-status", Route: "/dashboard/missions", Target: "text=Connected", Expectation: "text_present", Timeout: 15},
			{ID: "devices-table", Route: "/dashboard/devices", Target: "css=table tbody tr", Expectation: "element_count>=1"},
			{ID: "exempt", Route: "/x", Target: "text=y", Expectation: "text_present", Unsupported: true, Reason: "plan-gated"},
		},
	}}
	cases := uiVocabCases(svc)
	if len(cases) != 2 {
		t.Fatalf("want 2 cases (unsupported skipped), got %d", len(cases))
	}
	c := cases[0]
	if c.ID != "ui-vocab-missions-conn-status" || c.Action != "browser_flow" || c.Target != "http://localhost:5183" {
		t.Fatalf("case shape: %+v", c)
	}
	if len(c.Steps) != 2 || c.Steps[0].Action != "browser_goto" || c.Steps[0].URL != "/dashboard/missions" {
		t.Fatalf("steps: %+v", c.Steps)
	}
	e := c.Steps[1]
	if e.Action != "browser_expect" || e.Target != "text=Connected" || e.Type != "missions-conn-status" || e.Timeout != 15 {
		t.Fatalf("expect step: %+v", e)
	}
	if e.Asserts["expectation"] != "text_present" {
		t.Fatalf("comparator: %+v", e.Asserts)
	}
	// Timeout defaulting: 0 -> 10.
	if cases[1].Steps[1].Timeout != 10 {
		t.Errorf("default timeout: %+v", cases[1].Steps[1])
	}
}

// A protocol-coupled assertion (from_api) compiles to a 3-step case: fetch
// the protocol truth (captured), goto the route, wait-assert the templated
// selector — the captured value rides {{case.<name>}} into the browser step.
func TestUIVocabCasesCoupled(t *testing.T) {
	svc := project.Service{Name: "open-agents", URL: "http://localhost:8989/ws/{userId}"}
	svc.Vocabulary = &project.Vocabulary{UI: &project.VocabUI{
		BaseURL: "http://localhost:5183", Locale: "en",
		Assertions: []project.VocabUIAssertion{{
			ID: "missions-device-selector-count", Route: "/dashboard/missions",
			Target: "text={{case.onlineCount}} devices online", Expectation: "text_present", Timeout: 15,
			FromAPI: &project.VocabUIFromAPI{
				Method: "GET", Path: "/api/missions/online-devices", AuthRole: "web",
				Capture: map[string]string{"length:devices": "onlineCount"},
			},
		}},
	}}
	cases := uiVocabCases(svc)
	if len(cases) != 1 {
		t.Fatalf("want 1 case, got %d", len(cases))
	}
	c := cases[0]
	if len(c.Steps) != 3 {
		t.Fatalf("coupled case must have 3 steps, got %d: %+v", len(c.Steps), c.Steps)
	}
	f := c.Steps[0]
	if f.Action != "http_request" || f.Method != "GET" {
		t.Fatalf("fetch step: %+v", f)
	}
	if f.URL != "http://localhost:8989/api/missions/online-devices" {
		t.Fatalf("fetch URL must join the service host: %q", f.URL)
	}
	if f.AuthRole != "web" || f.ExpectStatusClass != "2xx" || f.Capture["length:devices"] != "onlineCount" {
		t.Fatalf("fetch step fields: %+v", f)
	}
	if c.Steps[1].Action != "browser_goto" || c.Steps[1].URL != "/dashboard/missions" {
		t.Fatalf("goto step: %+v", c.Steps[1])
	}
	if c.Steps[2].Action != "browser_expect" || c.Steps[2].Target != "text={{case.onlineCount}} devices online" {
		t.Fatalf("expect step must carry the placeholder verbatim (runtime substitutes): %+v", c.Steps[2])
	}
	// Default auth role when from_api declares none.
	svc.Vocabulary.UI.Assertions[0].FromAPI.AuthRole = ""
	cases = uiVocabCases(svc)
	if cases[0].Steps[0].AuthRole != "web" {
		t.Fatalf("default auth role must be web: %+v", cases[0].Steps[0])
	}
}

func TestUIVocabCasesNil(t *testing.T) {
	svc := project.Service{Name: "open-agents"}
	if got := uiVocabCases(svc); got != nil {
		t.Errorf("nil vocabulary must yield nil, got %d", len(got))
	}
	svc.Vocabulary = &project.Vocabulary{}
	if got := uiVocabCases(svc); got != nil {
		t.Errorf("nil ui must yield nil, got %d", len(got))
	}
}
