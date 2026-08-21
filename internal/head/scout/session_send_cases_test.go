package scout

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/binoctal/cerberus/internal/head/agent"
)

func TestSessionSendCases_LifecycleSteps(t *testing.T) {
	cases := sessionSendCases(missionSendFixture(), map[string]bool{"bridge": true})
	if len(cases) != 1 {
		t.Fatalf("want exactly 1 session-family case, got %d", len(cases))
	}
	tc := cases[0]
	// Send order follows handler semantics: start (await started), chat
	// (await output), then the three silent/no-ack sends.
	var sends []string
	for _, s := range tc.Steps {
		switch s.Action {
		case "ws_connect":
		case "ws_receive":
		case "ws_send":
			var msg map[string]any
			if err := json.Unmarshal([]byte(s.Message), &msg); err != nil {
				t.Fatalf("ws_send message not JSON: %v", err)
			}
			typ, _ := msg["type"].(string)
			sends = append(sends, typ)
			payload, _ := msg["payload"].(map[string]any)
			if payload["deviceId"] != "{{bridge.deviceId}}" {
				t.Fatalf("%s payload missing deviceId route: %v", typ, payload)
			}
			if typ != "session:start" && payload["sessionId"] != "cerberus-session-family" {
				t.Fatalf("%s payload missing sessionId: %v", typ, payload)
			}
		default:
			t.Fatalf("unexpected action %q", s.Action)
		}
	}
	want := []string{"session:start", "chat:send", "session:resize", "control:takeover", "session:resume", "session:cancel"}
	if strings.Join(sends, ",") != strings.Join(want, ",") {
		t.Fatalf("send sequence %v, want %v", sends, want)
	}
	// Four receivables: session:started, chat:response (with the output-batch
	// alias), then the resume/cancel acks — both DO-whitelisted since the
	// known-issue #1 alignment.
	var recvs []string
	for _, s := range tc.Steps {
		if s.Action == "ws_receive" {
			recvs = append(recvs, s.Type)
		}
	}
	if strings.Join(recvs, ",") != "session:started,chat:response,session:resumed,session:cancelled" {
		t.Fatalf("receive sequence %v, want [session:started chat:response session:resumed session:cancelled]", recvs)
	}
}

func TestSessionSendCases_Gating(t *testing.T) {
	fx := missionSendFixture()
	if got := sessionSendCases(fx, nil); got != nil {
		t.Fatal("no real bridge roles → no cases")
	}
	if got := sessionSendCases(fx, map[string]bool{"web": true}); got != nil {
		t.Fatal("real web role only → no cases")
	}
	noVocab := fx
	noVocab.Vocabulary = nil
	if got := sessionSendCases(noVocab, map[string]bool{"bridge": true}); got != nil {
		t.Fatal("no vocabulary → no cases")
	}
}

func TestSessionSendCases_RequestPayloadOverrides(t *testing.T) {
	fx := missionSendFixture()
	fx.Protocol.Roles["bridge"].RequestPayload = map[string]map[string]string{
		"session:start": {"cliType": "claude-shim-v2", "workDir": "/tmp/cerberus"},
	}
	cases := sessionSendCases(fx, map[string]bool{"bridge": true})
	var start agent.TestStep
	for _, s := range cases[0].Steps {
		if s.Action == "ws_send" && strings.Contains(s.Message, "session:start") {
			start = s
		}
	}
	var msg map[string]any
	if err := json.Unmarshal([]byte(start.Message), &msg); err != nil {
		t.Fatalf("start message not JSON: %v", err)
	}
	payload, _ := msg["payload"].(map[string]any)
	if payload["cliType"] != "claude-shim-v2" || payload["workDir"] != "/tmp/cerberus" {
		t.Fatalf("role request_payload overrides not applied: %v", payload)
	}
}
