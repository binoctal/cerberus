//go:build integration

package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// devLogin POSTs /api/dev/login and returns the dev JWT (same JWT_SECRET the
// protected /api/devices routes verify). The dev server's CSRF middleware passes
// any Bearer POST, so no Origin is strictly required, but Origin is sent to match
// devSetup. The login uses the demo default credentials the setup created.
func devLogin(t *testing.T, base string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/dev/login", strings.NewReader(`{}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", base)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "POST /api/dev/login")
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	require.Equalf(t, http.StatusOK, resp.StatusCode, "dev login status: body=%s", strings.TrimSpace(string(body)))
	var out struct {
		Token string `json:"token"`
	}
	require.NoErrorf(t, json.Unmarshal(body, &out), "dev login decode: body=%s", strings.TrimSpace(string(body)))
	require.NotEmpty(t, out.Token, "dev login returned empty token")
	return out.Token
}

// TestHTTPTrigger_LiveDeviceRestart proves the http_request step end-to-end
// against a live open-agents dev server: an authenticated POST
// /api/devices/<bridgeDeviceId>/restart (run THROUGH the deterministic Steps
// runner, so resolveHTTPStep's {{bridge.deviceId}} placeholder + AuthRole JWT
// injection + ExpectStatus gate are all exercised) causes the DO to
// broadcastToWeb a device:restart frame the web connection then receives.
func TestHTTPTrigger_LiveDeviceRestart(t *testing.T) {
	f := setupOpenAgents(t, false)

	// The fixture populates ActorTokens but neither ActorPathParams nor
	// ActorHTTPTokens; seed both — the bridge deviceId (for the URL placeholder)
	// and the web JWT (for the Authorization injection).
	jwt := devLogin(t, oaBase)
	f.wsIdx.ActorPathParams = map[string]map[string]string{
		"web-actor":    {"userId": f.userId},
		"bridge-actor": {"deviceId": f.deviceId, "userId": f.userId},
	}
	f.wsIdx.ActorHTTPTokens = map[string]string{"web-actor": jwt}

	tc := &TestCase{
		ID:     "tc-http-device-restart",
		Target: "ws://localhost:8989/ws/" + f.userId,
		Steps: []TestStep{
			{Action: "ws_connect", Role: "web", ConnectionID: "web"},
			{Action: "http_request", Method: "POST",
				URL:      "http://localhost:8989/api/devices/{{bridge.deviceId}}/restart",
				AuthRole: "web", ExpectStatus: 200},
			{Action: "ws_receive", ConnectionID: "web", Type: "device:restart", Timeout: 5},
		},
	}
	se := newStepExecutionWithIdx(t, tc, f.wsIdx)
	result := se.runSteps()
	require.Equal(t, StepPassed, result.Status,
		"device-restart HTTP trigger must push device:restart to web; evidence=%v", result.Evidence)
}
