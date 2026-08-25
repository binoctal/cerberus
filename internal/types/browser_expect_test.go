package types

import "testing"

func TestEvaluateBrowserExpectation(t *testing.T) {
	cases := []struct {
		name       string
		comparator string
		text       string
		count      int
		pass       bool
	}{
		{"text_present hit", "text_present", "Connected", 1, true},
		// A text= locator that resolves can only yield the matched text; a
		// miss is observed as "" (element not found).
		{"text_present miss", "text_present", "", 0, false},
		{"text_present empty miss", "text_present", "", 0, false},
		{"text_absent clean", "text_absent", "", 0, true},
		{"text_absent violation", "text_absent", "Error: boom", 1, false},
		{"element_visible present", "element_visible", "", 1, true},
		{"element_visible absent", "element_visible", "", 0, false},
		{"count ok", "element_count>=2", "", 2, true},
		{"count exact", "element_count>=2", "", 2, true},
		{"count under", "element_count>=2", "", 1, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pass, _, err := EvaluateBrowserExpectation(c.comparator, c.text, c.count)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if pass != c.pass {
				t.Errorf("comparator %q text=%q count=%d: got pass=%v want %v", c.comparator, c.text, c.count, pass, c.pass)
			}
		})
	}
	t.Run("unknown comparator errors", func(t *testing.T) {
		if _, _, err := EvaluateBrowserExpectation("bogus", "", 0); err == nil {
			t.Error("expected error for unknown comparator")
		}
	})
	t.Run("bad count suffix errors", func(t *testing.T) {
		if _, _, err := EvaluateBrowserExpectation("element_count>=x", "", 0); err == nil {
			t.Error("expected error for non-numeric count")
		}
	})
}

func TestBrowserExpectActionValidate(t *testing.T) {
	if err := (BrowserExpectAction{Selector: "text=x", Expectation: "text_present"}).Validate(); err != nil {
		t.Errorf("valid action rejected: %v", err)
	}
	if err := (BrowserExpectAction{Expectation: "text_present"}).Validate(); err == nil {
		t.Error("empty selector must fail Validate")
	}
	if err := (BrowserExpectAction{Selector: "text=x"}).Validate(); err == nil {
		t.Error("empty expectation must fail Validate")
	}
}
