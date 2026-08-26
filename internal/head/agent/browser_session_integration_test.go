//go:build integration

// Full-chain browser-leg validation: session injection (UI login →
// localStorage auth blob) → vocab-page navigation → display-promise
// assertions. Requires the live dogfood stack (wrangler :8989, vite preview
// :5183) — run via `make integration-openagents`-style environments.
package agent

import (
	"testing"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/types"
)

func TestBrowserLegFullChainIntegration(t *testing.T) {
	be, err := NewBrowserExecutor(t.TempDir(), zap.NewNop())
	if err != nil {
		t.Fatalf("executor (is chromium installed?): %v", err)
	}
	defer be.Close()

	ui := &project.VocabUI{BaseURL: "http://localhost:5183", Locale: "en", AuthActor: "web-actor"}
	actor := project.Actor{Name: "web-actor"}
	actor.Credentials.Email = "dev@openagents.local"
	actor.Credentials.Password = "dev123456"
	if err := be.InitBrowserSession(t.Context(), ui, actor, "http://localhost:8989"); err != nil {
		t.Fatalf("session injection: %v", err)
	}

	r := be.Execute(t.Context(), types.BrowserGotoAction{URL: "http://localhost:5183/dashboard/missions"})
	if !r.Success() {
		t.Fatalf("goto missions: %s", r.Summary())
	}

	// missions-conn-status is the #18 regression guard: before open-agents
	// 660d41f the sidebar said "Connecting..." forever.
	assertions := []struct {
		name       string
		selector   string
		comparator string
	}{
		{"missions-conn-status", "text=Connected", "text_present"},
		{"missions-device-counter", "text=devices online", "text_present"},
		{"missions-list-renders", "css=aside", "element_visible"},
	}
	for _, a := range assertions {
		res := be.Execute(t.Context(), types.BrowserExpectAction{
			Selector: a.selector, Expectation: a.comparator, Timeout: 15})
		br, ok := res.(types.BrowserResult)
		if !ok || !br.OK {
			t.Errorf("%s failed: %+v", a.name, res)
			if p, serr := be.ScreenshotToFile("integration", a.name); serr == nil {
				t.Logf("failure screenshot: %s", p)
			}
		}
	}
}
