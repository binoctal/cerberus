//go:build integration

package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/project"
)

// TestRunStepsMultiConnectionOpenAgents dogfoods cerberus's multi-connection
// orchestration against a live open-agents target. Build-tagged out of `make
// test`. To run:
//
//	fnm use 22 && cd ../open-agents/apps/api && npm run dev   # serves :8989
//	go test -tags integration -run TestRunStepsMultiConnectionOpenAgents ./internal/head/agent/
//
// Hard asserts are capability-level: cerberus must establish two real sockets
// (web + bridge) to the SAME /ws/<userId> DO (both connects succeed). Exact
// protocol matching (devices:sync push, session:start->session:created relay)
// is BEST-EFFORT: open-agents' relay vocabulary is discovered at run time, so a
// mismatch is a dogfood finding, not a cerberus regression (the deterministic
// TestRunStepsMultiConnection is the mechanical proof).
func TestRunStepsMultiConnectionOpenAgents(t *testing.T) {
	const base = "http://localhost:8989"
	if !reachable(base) {
		t.Skipf("open-agents not reachable at %s; bring up `npm run dev` (apps/api)", base)
	}

	// Provision a user + bridge device. demo_token (dev backdoor) authenticates
	// the web socket for any userId; the device token authenticates bridge.
	// Both connect to /ws/<userId> so they share one UserRoom DO (the relay).
	userId, deviceToken, err := devSetup(base)
	require.NoError(t, err, "POST /api/dev/setup")
	t.Logf("provisioned userId=%s deviceToken=%s", userId, deviceToken)

	p := &project.Protocol{
		TypePath: "type",
		Auth:     &project.ProtocolAuth{Strategy: "query", Param: "token", CredentialRef: "web-actor"},
		Roles: map[string]*project.ProtocolRole{
			"web": {
				CredentialRef: "web-actor",
				Params:        map[string]string{"type": "web"},
				Handshake:     &project.RoleHandshake{AwaitType: "devices:sync", Optional: true, Timeout: 2},
			},
			"bridge": {
				CredentialRef: "bridge-actor",
				Params:        map[string]string{"type": "bridge"},
			},
		},
	}
	wsIdx := &WSProtocolIndex{
		ByHost: map[string]*project.Protocol{"localhost:8989": p},
		ActorTokens: map[string]string{
			"web-actor":    "demo_token",
			"bridge-actor": deviceToken,
		},
	}

	tc := &TestCase{
		ID:     "tc-openagents-relay",
		Target: "ws://localhost:8989/ws/" + userId,
		Steps: []TestStep{
			{Action: "ws_connect", Role: "web", ConnectionID: "c-web"},
			{Action: "ws_connect", Role: "bridge", ConnectionID: "c-bridge"},
			// Relay signal: the DO pushes devices:sync to web once bridge joins.
			{Action: "ws_receive", ConnectionID: "c-web", Type: "devices:sync", Timeout: 3},
			// Best-effort request/reply across the relay.
			{Action: "ws_send", ConnectionID: "c-web", Message: `{"type":"session:start"}`},
			{Action: "ws_receive", ConnectionID: "c-web", Type: "session:created", Timeout: 3},
		},
	}

	se := newStepExecutionWithIdx(t, tc, wsIdx)
	result := se.runSteps()
	for _, ev := range result.Evidence {
		t.Logf("step evidence: %s", ev.Content)
	}

	// HARD capability assertion: both connects succeeded. Evidence is appended
	// per step before the success check, and the case short-circuits on failure,
	// so reaching the 3rd step (index 2) means steps 0 and 1 (the two connects)
	// both succeeded => cerberus opened two real sockets to the same DO.
	require.GreaterOrEqual(t, len(result.Evidence), 3,
		"both connects must succeed (web + bridge); evidence=%d", len(result.Evidence))

	// BEST-EFFORT: the full relay. Log the outcome; do not fail the test on a
	// protocol mismatch (that is a dogfood finding about open-agents).
	if result.Status == StepPassed {
		t.Logf("relay case fully passed: status=%s", result.Status)
	} else {
		t.Logf("relay case did not fully pass (dogfood finding): status=%s", result.Status)
	}
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

// devSetup POSTs /api/dev/setup and returns the provisioned userId + deviceToken
// (response.config.{userId,deviceToken}).
func devSetup(base string) (userId, deviceToken string, err error) {
	resp, err := http.Post(base+"/api/dev/setup", "application/json", strings.NewReader(`{}`))
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	var out struct {
		Config struct {
			UserId      string `json:"userId"`
			DeviceToken string `json:"deviceToken"`
		} `json:"config"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", err
	}
	return out.Config.UserId, out.Config.DeviceToken, nil
}
