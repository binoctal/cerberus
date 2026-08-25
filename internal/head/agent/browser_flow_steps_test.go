package agent

import (
	"testing"

	"github.com/binoctal/cerberus/internal/types"
)

func TestResolveBrowserStep(t *testing.T) {
	tc := &TestCase{Target: "http://localhost:5183"}

	got, err := resolveBrowserStep(tc, TestStep{Action: "browser_goto", URL: "/dashboard/missions"})
	if err != nil {
		t.Fatal(err)
	}
	if g, ok := got.(types.BrowserGotoAction); !ok || g.URL != "http://localhost:5183/dashboard/missions" {
		t.Errorf("goto: got %+v", got)
	}

	got, err = resolveBrowserStep(tc, TestStep{Action: "browser_goto", URL: "http://other:1/x"})
	if err != nil {
		t.Fatal(err)
	}
	if g, ok := got.(types.BrowserGotoAction); !ok || g.URL != "http://other:1/x" {
		t.Errorf("absolute URL must win: %+v", got)
	}

	got, err = resolveBrowserStep(tc, TestStep{Action: "browser_goto"})
	if err != nil {
		t.Fatal(err)
	}
	if g, ok := got.(types.BrowserGotoAction); !ok || g.URL != "http://localhost:5183" {
		t.Errorf("empty URL falls back to base: %+v", got)
	}

	got, err = resolveBrowserStep(tc, TestStep{Action: "browser_click", Target: "text=Run"})
	if g, ok := got.(types.BrowserClickAction); err != nil || !ok || g.Selector != "text=Run" {
		t.Errorf("click: got %+v err %v", got, err)
	}

	got, err = resolveBrowserStep(tc, TestStep{Action: "browser_fill", Target: "css=input", Message: "hello"})
	if g, ok := got.(types.BrowserFillAction); err != nil || !ok || g.Value != "hello" {
		t.Errorf("fill: got %+v err %v", got, err)
	}

	got, err = resolveBrowserStep(tc, TestStep{Action: "browser_expect", Target: "text=Connected",
		Type: "missions-conn-status", Asserts: map[string]any{"expectation": "text_present"}, Timeout: 15})
	if err != nil {
		t.Fatal(err)
	}
	be, ok := got.(types.BrowserExpectAction)
	if !ok || be.Selector != "text=Connected" || be.Expectation != "text_present" || be.Timeout != 15 {
		t.Errorf("expect: got %+v", got)
	}

	// Comparator defaults to text_present when asserts omit it.
	got, err = resolveBrowserStep(tc, TestStep{Action: "browser_expect", Target: "css=aside"})
	if err != nil {
		t.Fatal(err)
	}
	if be, ok := got.(types.BrowserExpectAction); !ok || be.Expectation != "text_present" {
		t.Errorf("default comparator: %+v", got)
	}

	if _, err = resolveBrowserStep(tc, TestStep{Action: "browser_nope"}); err == nil {
		t.Error("unknown verb must error")
	}
}

func TestStepEvidenceBrowserExpect(t *testing.T) {
	s := TestStep{Action: "browser_expect", Type: "missions-conn-status", Timeout: 15}
	pass := types.BrowserResult{OK: true, URL: "http://x", Selector: "text=Connected",
		Expectation: "text_present", Observed: "Connected"}
	ev := stepEvidence(s, pass)
	if !ev.Matched || ev.MatchedType != "missions-conn-status" {
		t.Errorf("pass evidence: %+v", ev)
	}
	fail := types.BrowserResult{OK: false, URL: "http://x", Selector: "text=Connected",
		Expectation: "text_present", Err: "expect text_present \"text=Connected\": not satisfied"}
	ev = stepEvidence(s, fail)
	if ev.Matched || ev.MatchedType != "missions-conn-status" {
		t.Errorf("fail evidence: %+v", ev)
	}
}
