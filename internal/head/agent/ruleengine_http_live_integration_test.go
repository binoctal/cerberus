//go:build integration

package agent

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/types"
)

// TestRuleEngine_HTTPCase_LiveJWT proves the rule-engine HTTP path — the one
// tryRuleEngine runs first for Scout free-form cases (method + path target) —
// injects the http_login JWT, not the WS web-token, against a live server. The
// deterministic step path (http_request + AuthRole) is covered by
// TestHTTPTrigger_LiveDeviceRestart; this covers the rule-engine path that
// previously read only Credentials.Headers (the inject_as web-token) and 401'd
// on JWT-protected routes.
func TestRuleEngine_HTTPCase_LiveJWT(t *testing.T) {
	setupOpenAgents(t, false) // provisions a user; skips if the server is down
	jwt := devLogin(t, oaBase)

	// web-actor carries BOTH the WS web-token (inject_as → Headers) and the
	// http_login JWT (RawHTTPToken) — the real dogfood credential shape.
	actors := []project.Actor{{
		Name:    "web-actor",
		Service: "realtime",
		Credentials: project.CredentialRef{
			Headers:      map[string]string{"Authorization": "Bearer demo_token"},
			RawHTTPToken: jwt,
		},
	}}
	services := []project.Service{{Name: "realtime", URL: oaBase}}
	engine := NewRuleEngine(services, actors, "")

	// Scout free-form HTTP case shape: method + path → matchHTTPRules Rule 1.
	action, matched := engine.Match(TestCase{Method: "GET", Target: "/api/devices", Service: "realtime"})
	require.True(t, matched, "rule engine must match a method+path HTTP case")
	ha := action.(types.HTTPAction)
	require.Equal(t, "Bearer "+jwt, ha.Headers["Authorization"],
		"rule-engine HTTP case must inject the http_login JWT, not the WS web-token")

	// Drift symptom: the protected route must not 401 with the JWT the rule
	// engine now injects.
	resp := execHTTPAction(t, ha)
	defer resp.Body.Close()
	require.NotEqualf(t, http.StatusUnauthorized, resp.StatusCode,
		"protected route must not 401 with the http_login JWT (the drift symptom)")

	// Baseline: the WS web-token alone still 401s — proving the fix matters and
	// that without it the rule-engine path would fail identically.
	respBad := execHTTPAction(t, types.HTTPAction{
		Method:  http.MethodGet,
		URL:     oaBase + "/api/devices",
		Headers: map[string]string{"Authorization": "Bearer demo_token"},
	})
	defer respBad.Body.Close()
	require.Equal(t, http.StatusUnauthorized, respBad.StatusCode,
		"WS demo_token must 401 on the protected route (drift baseline)")
}

// execHTTPAction performs the HTTPAction's request and returns the response.
func execHTTPAction(t *testing.T, a types.HTTPAction) *http.Response {
	t.Helper()
	var body io.Reader
	if a.Body != "" {
		body = strings.NewReader(a.Body)
	}
	req, err := http.NewRequest(a.Method, a.URL, body)
	require.NoError(t, err)
	for k, v := range a.Headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}
