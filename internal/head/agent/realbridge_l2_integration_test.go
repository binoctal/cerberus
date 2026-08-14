//go:build integration

package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/types"
)

// TestRealBridge_L2_RealCLIScheduled — L2 of the fidelity ladder, the core
// promise itself: open-agents schedules a REAL AI CLI. The bridge's ACP
// adapter (npx @agentclientprotocol/claude-agent-acp) spawns the real claude
// binary with the ambient GLM credentials, the web prompt flows through
// ACP session/new + prompt, and the REAL model output streams back to the web
// side. Cost-bounded: exactly one prompt, skipped without credentials.
//
// OPEN-AGENTS FINDING (2026-08-15): the ACP adapter rejects a RELATIVE
// workDir ("cwd must be an absolute path"), so an absolute path is used here;
// mission-dispatched tasks pass a relative worktree path and currently fail
// at this same spot (see M1).
func TestRealBridge_L2_RealCLIScheduled(t *testing.T) {
	if !reachable(oaBase) {
		t.Skipf("open-agents not reachable at %s", oaBase)
	}
	if os.Getenv("ANTHROPIC_AUTH_TOKEN") == "" || os.Getenv("ANTHROPIC_BASE_URL") == "" {
		t.Skip("ANTHROPIC_AUTH_TOKEN / ANTHROPIC_BASE_URL not set; skipping the real-CLI run")
	}

	// No shim: the REAL claude binary must be what the ACP adapter spawns.
	bridges := launchRealBridges(t, false, "l2-real")
	b1 := bridges[0]

	userId, _, _, err := devSetup(oaBase)
	require.NoError(t, err)

	workDir, err := os.MkdirTemp("", "cerberus-l2-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(workDir) })

	wsURL := "ws://localhost:8989/ws/" + userId
	sessionID := "e2e-l2-real-claude"
	mustJSON := func(m map[string]any) string {
		b, _ := json.Marshal(m)
		return string(b)
	}

	// Case 1: start the ACP session and drive one bounded prompt. The output
	// receive is LAST so its matched frame text is on the step result.
	tc := &TestCase{
		ID:     "real-bridge-l2-real-cli",
		Target: wsURL,
		Action: "ws_flow",
		Steps: []TestStep{
			{Action: "ws_connect", Role: "web", ConnectionID: "c-web"},
			{Action: "ws_send", ConnectionID: "c-web", Message: mustJSON(map[string]any{
				"type": "session:start",
				"payload": map[string]any{
					"sessionId": sessionID, "cliType": "claude",
					// Absolute: the ACP adapter rejects relative cwd paths.
					"workDir": workDir, "cols": 80, "rows": 24, "deviceId": b1.deviceId,
				},
			})},
			// ACP initialize + session/new can take ~30s (npx cold start).
			{Action: "ws_receive", ConnectionID: "c-web", Type: "session:started", Timeout: 90},
			{Action: "ws_send", ConnectionID: "c-web", Message: mustJSON(map[string]any{
				"type": "chat:send",
				"payload": map[string]any{
					"sessionId": sessionID, "content": "Reply with exactly the word READY and nothing else.", "deviceId": b1.deviceId,
				},
			})},
			// Real model output streams back as chat:response content.
			{Action: "ws_receive", ConnectionID: "c-web", Type: "chat:response", Timeout: 120},
		},
	}
	se := newStepExecutionWithIdx(t, tc, wsIdxForReal(userId))
	result := se.runSteps()
	for _, ev := range result.Evidence {
		t.Logf("l2 evidence: %s", ev.Content)
	}
	require.Equal(t, StepPassed, result.Status, "real CLI session case must pass")

	fr, ok := result.Result.(types.WSResult)
	require.True(t, ok)
	require.NotEmpty(t, fr.MatchedMessage, "the real model output frame must be captured")

	// Stop the session (bounded cost — no further prompts).
	seStop := newStepExecutionWithIdx(t, &TestCase{
		ID: "real-bridge-l2-stop", Target: wsURL, Action: "ws_flow",
		Steps: []TestStep{
			{Action: "ws_connect", Role: "web", ConnectionID: "c-web"},
			{Action: "ws_send", ConnectionID: "c-web", Message: mustJSON(map[string]any{
				"type":    "session:stop",
				"payload": map[string]any{"sessionId": sessionID, "deviceId": b1.deviceId},
			})},
			{Action: "ws_receive", ConnectionID: "c-web", Type: "session:stopped", Timeout: 90},
		},
	}, wsIdxForReal(userId))
	require.Equal(t, StepPassed, seStop.runSteps().Status, "stop case must pass")

	fmt.Println("L2 OK: real claude CLI scheduled via ACP, output frame:",
		truncateFor(fr.MatchedMessage, 200))
}

func truncateFor(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
