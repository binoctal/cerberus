//go:build integration

// Full-chain browser-leg validation: session injection (UI login →
// localStorage auth blob) → vocab-page navigation → display-promise
// assertions. Requires the live dogfood stack (wrangler :8989, vite preview
// :5183) — run via `make integration-openagents`-style environments.
package agent

import (
	"io"
	"net/http"
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

	// missions-device-selector-count: the first protocol-coupled promise.
	// The API's online-device count must equal the count the device
	// selector renders — a display that disagrees with the protocol truth
	// fails here (this leg mirrors the vocab from_api compilation).
	coupledLogin, err := sendLogin(t.Context(), "http://localhost:8989", project.AuthLogin{
		Method: "POST", Path: "/api/auth/login",
		Body:    map[string]string{"email": "{email}", "password": "{password}"},
		Headers: map[string]string{"Origin": "http://localhost:8989"},
	}, map[string]string{"{email}": "dev@openagents.local", "{password}": "dev123456"})
	if err != nil {
		t.Fatalf("coupled login: %v", err)
	}
	apiToken, _ := extractByDotPath(coupledLogin, "token")

	req, _ := http.NewRequest("GET", "http://localhost:8989/api/missions/online-devices", nil)
	req.Header.Set("Authorization", "Bearer "+apiToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("online-devices: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	captured, err := captureFromHTTPBody(string(body), map[string]string{"length:devices": "onlineCount"})
	if err != nil {
		t.Fatalf("capture: %v (body: %s)", err, body)
	}
	res := be.Execute(t.Context(), types.BrowserExpectAction{
		Selector:    "text=" + captured["onlineCount"] + " devices online",
		Expectation: "text_present", Timeout: 15})
	if br, ok := res.(types.BrowserResult); !ok || !br.OK {
		t.Errorf("missions-device-selector-count failed: API says %s, display: %+v",
			captured["onlineCount"], res)
		if p, serr := be.ScreenshotToFile("integration", "coupled-count"); serr == nil {
			t.Logf("failure screenshot: %s", p)
		}
	}
}
