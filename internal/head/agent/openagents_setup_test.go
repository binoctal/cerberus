//go:build integration

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/project"
)

const oaBase = "http://localhost:8989"

// oaFixture carries the provisioned open-agents connection wiring shared by
// every open-agents integration test. wsIdx is bound to the dynamic user/device
// from /api/dev/setup; each test builds its own TestCase and binds it via
// newStepExecutionWithIdx(t, tc, f.wsIdx).
type oaFixture struct {
	wsIdx    *WSProtocolIndex
	userId   string
	deviceId string
	capture  *captureServer // nil when withCapture is false
}

// setupOpenAgents provisions a user + bridge device and wires the web/bridge
// protocol (web awaits device:online, optional). Skips when open-agents is not
// reachable. withCapture also starts a capture server on port 9099 (skips if the
// port is unavailable — gap E prerequisite not met).
func setupOpenAgents(t *testing.T, withCapture bool) oaFixture {
	if !reachable(oaBase) {
		t.Skipf("open-agents not reachable at %s; bring up `npm run dev` (apps/api)", oaBase)
	}
	userId, deviceId, deviceToken, err := devSetup(oaBase)
	require.NoError(t, err, "POST /api/dev/setup")

	p := &project.Protocol{
		TypePath: "type",
		Auth:     &project.ProtocolAuth{Strategy: "query", Param: "token", CredentialRef: "web-actor"},
		Roles: map[string]*project.ProtocolRole{
			"web": {
				CredentialRef: "web-actor",
				Params:        map[string]string{"type": "web"},
				Handshake:     &project.RoleHandshake{AwaitType: "device:online", Optional: true, Timeout: 2},
			},
			"bridge": {
				CredentialRef: "bridge-actor",
				Params:        map[string]string{"type": "bridge", "deviceId": deviceId},
			},
		},
	}
	f := oaFixture{
		wsIdx: &WSProtocolIndex{
			ByHost: map[string]*project.Protocol{"localhost:8989": p},
			ActorTokens: map[string]string{
				"web-actor":    "demo_token",
				"bridge-actor": deviceToken,
			},
		},
		userId:   userId,
		deviceId: deviceId,
	}
	if withCapture {
		f.capture = newCaptureServer(t, 9099)
		t.Cleanup(f.capture.stop)
	}
	return f
}

// reachable reports whether base responds to a GET within 2 s.
func reachable(base string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}

// devSetup POSTs /api/dev/setup and returns the provisioned userId, deviceId,
// and deviceToken. The dev server's CSRF middleware requires an Origin header.
func devSetup(base string) (userId, deviceId, deviceToken string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/dev/setup", strings.NewReader(`{}`))
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", base)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", "", "", fmt.Errorf("dev setup: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		Config struct {
			UserId      string `json:"userId"`
			DeviceId    string `json:"deviceId"`
			DeviceToken string `json:"deviceToken"`
		} `json:"config"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", "", err
	}
	return out.Config.UserId, out.Config.DeviceId, out.Config.DeviceToken, nil
}
