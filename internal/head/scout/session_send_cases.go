package scout

import (
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// sessionSendCases emits the web-origin session-lifecycle sends routed at the
// REAL bridge via payload.deviceId: chat:send, session:resize,
// control:takeover, session:resume, session:cancel. All need a live session
// (and the deviceId route) — the payload-less generic steps cannot reach their
// handlers (MISSING_DEVICE_ID), which is why they stayed gaps through the
// send-credit burn-down.
//
// Reply expectations follow the bridge's actual behavior (bridge.go):
//   - chat:send injects into the live session and its output returns as
//     chat:response (DO-whitelisted bridge→web) — asserted.
//   - session:resume on the live session acks session:resumed; session:cancel
//     acks session:cancelled (both DO-whitelisted since the known-issue #1
//     whitelist alignment) — asserted.
//   - session:resize (handleSessionResize) and control:takeover
//     (handleControlTakeover, a bare logDebug) are silent — send-side credit.
//
// The session rides the deterministic claude shim (zero-LLM), like
// realE2ECases; sessionId is a literal (ws-only cases ship {{case.*}}
// placeholders verbatim — see mission_send_cases.go).
func sessionSendCases(svc project.Service, realRoles map[string]bool) []agent.TestCase {
	if !realRoles["bridge"] || svc.Protocol == nil || svc.Vocabulary == nil {
		return nil
	}
	role := svc.Protocol.Roles["bridge"]
	if role == nil {
		return nil
	}
	deviceID := "{{bridge.deviceId}}"
	sessionID := "cerberus-session-family"
	startPayload := map[string]any{
		"sessionId": sessionID,
		"cliType":   "claude-pty",
		"workDir":   "/tmp",
		"cols":      80,
		"rows":      24,
		"deviceId":  deviceID,
	}
	for k, v := range role.RequestPayload["session:start"] {
		startPayload[k] = v
	}
	return []agent.TestCase{{
		ID:      wsCaseID(svc.Name, "session-family", "lifecycle"),
		Name:    svc.Name + " web drives the session lifecycle at the real bridge (chat/resize/takeover/resume/cancel)",
		Service: svc.Name, Target: svc.URL, Action: "ws_flow",
		Expectation: svc.Name + ": web starts a session on the REAL bridge via deviceId routing, chats into it (chat:response observed), resizes it, takes over control, resumes it (session:resumed ack), and cancels it (session:cancelled ack; resize/takeover are send-side credit — the bridge acks nothing)",
		Priority:    0.6,
		Steps: []agent.TestStep{
			{Action: "ws_connect", ConnectionID: "web", Role: "web"},
			{Action: "ws_send", ConnectionID: "web", Message: wsSendBodyAny("session:start", startPayload)},
			{Action: "ws_receive", ConnectionID: "web", Type: "session:started", Timeout: 15},
			{Action: "ws_send", ConnectionID: "web", Message: wsSendBodyAny("chat:send", map[string]any{
				"sessionId": sessionID, "deviceId": deviceID, "content": "printf CERBERUS_SF_CHAT_OK\n",
			})},
			{Action: "ws_receive", ConnectionID: "web", Type: "chat:response", Aliases: []string{"session:output-batch"}, Timeout: 15},
			{Action: "ws_send", ConnectionID: "web", Message: wsSendBodyAny("session:resize", map[string]any{
				"sessionId": sessionID, "deviceId": deviceID, "cols": 120, "rows": 30,
			})},
			{Action: "ws_send", ConnectionID: "web", Message: wsSendBodyAny("control:takeover", map[string]any{
				"sessionId": sessionID, "deviceId": deviceID,
			})},
			{Action: "ws_send", ConnectionID: "web", Message: wsSendBodyAny("session:resume", map[string]any{
				"sessionId": sessionID, "deviceId": deviceID,
			})},
			{Action: "ws_receive", ConnectionID: "web", Type: "session:resumed", Timeout: 15},
			{Action: "ws_send", ConnectionID: "web", Message: wsSendBodyAny("session:cancel", map[string]any{
				"sessionId": sessionID, "deviceId": deviceID,
			})},
			{Action: "ws_receive", ConnectionID: "web", Type: "session:cancelled", Timeout: 15},
		},
	}}
}
