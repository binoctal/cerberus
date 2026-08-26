package agent

import (
	"testing"

	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/types"
)

func TestMatchBrowserRulesExpect(t *testing.T) {
	re := NewRuleEngine([]project.Service{{Name: "default", URL: "https://api.example.com"}}, nil, ".")
	tc := TestCase{Action: "browser_expect", Target: "text=Connected", Expectation: "text_present"}
	act, ok := re.matchBrowserRules(tc)
	if !ok {
		t.Fatal("browser_expect not matched")
	}
	be, is := act.(types.BrowserExpectAction)
	if !is {
		t.Fatalf("got %T want BrowserExpectAction", act)
	}
	// TestCase carries no Timeout field (only TestStep does): the atomic
	// mapping leaves it 0 and the executor default (10s) applies.
	if be.Selector != "text=Connected" || be.Expectation != "text_present" || be.Timeout != 0 {
		t.Errorf("mapping wrong: %+v", be)
	}
}
