package scout

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/binoctal/cerberus/internal/project"
)

// restartFixture declares two real bridge roles (bridge → actor 1, bridge2 →
// actor 2, both with deviceId params) plus the web client, mirroring the
// dogfood protocol's sacrificial-second-bridge shape.
func restartFixture() project.Service {
	return project.Service{
		Name: "open-agents", URL: "ws://localhost:8989/ws",
		Protocol: &project.Protocol{
			TypePath: "type",
			Roles: map[string]*project.ProtocolRole{
				"web":     {Params: map[string]string{"type": "web"}},
				"bridge":  {CredentialRef: "bridge-pty-1", Params: map[string]string{"type": "bridge", "deviceId": "{deviceId}"}},
				"bridge2": {CredentialRef: "bridge-pty-2", Params: map[string]string{"type": "bridge", "deviceId": "{deviceId}"}},
			},
		},
		Vocabulary: workflowVocabFixture(),
	}
}

func TestDeviceRestartCases_VictimSelectionAndSteps(t *testing.T) {
	cases := deviceRestartCases(restartFixture(), map[string]bool{"bridge": true, "bridge2": true})
	if len(cases) != 1 {
		t.Fatalf("want exactly 1 restart case, got %d", len(cases))
	}
	tc := cases[0]
	var actions []string
	for _, s := range tc.Steps {
		actions = append(actions, s.Action)
	}
	want := "ws_connect,ws_send,process_restart,ws_receive"
	if strings.Join(actions, ",") != want {
		t.Fatalf("step sequence %v, want %s", actions, want)
	}
	// The restart targets the alphabetically-LAST bridge role; the send
	// routes at ITS deviceId (only that process matches and exits).
	var restartRole, recvType string
	var sendBody map[string]any
	for _, s := range tc.Steps {
		switch s.Action {
		case "ws_send":
			if err := json.Unmarshal([]byte(s.Message), &sendBody); err != nil {
				t.Fatalf("send message not JSON: %v", err)
			}
		case "process_restart":
			restartRole = s.Role
		case "ws_receive":
			recvType = s.Type
		}
	}
	if restartRole != "bridge2" {
		t.Fatalf("process_restart role %q, want bridge2", restartRole)
	}
	payload, _ := sendBody["payload"].(map[string]any)
	if payload["deviceId"] != "{{bridge2.deviceId}}" {
		t.Fatalf("device:restart must route at {{bridge2.deviceId}}, got %v", payload["deviceId"])
	}
	if sendBody["type"] != "device:restart" {
		t.Fatalf("send type %v, want device:restart", sendBody["type"])
	}
	if recvType != "device:online" {
		t.Fatalf("receive type %q, want device:online", recvType)
	}
}

func TestDeviceRestartCases_SingleBridgeNotSacrificed(t *testing.T) {
	fx := restartFixture()
	delete(fx.Protocol.Roles, "bridge2")
	if got := deviceRestartCases(fx, map[string]bool{"bridge": true}); got != nil {
		t.Fatal("single real bridge must not be restarted (every other case routes at it)")
	}
}

func TestDeviceRestartCases_NeedsVocabAndDeviceParam(t *testing.T) {
	fx := restartFixture()
	fx.Vocabulary = nil
	if got := deviceRestartCases(fx, map[string]bool{"bridge": true, "bridge2": true}); got != nil {
		t.Fatal("no vocabulary → no restart case")
	}
	fx.Vocabulary = workflowVocabFixture()
	fx.Protocol.Roles["bridge2"].Params = map[string]string{"type": "bridge"} // no deviceId
	if got := deviceRestartCases(fx, map[string]bool{"bridge": true, "bridge2": true}); got != nil {
		t.Fatal("roles without a deviceId param cannot be restart victims")
	}
}
