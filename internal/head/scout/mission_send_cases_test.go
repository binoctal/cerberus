package scout

import (
	"strings"
	"testing"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// One service with web + bridge roles (bridge real), workflow vocab edges.
func missionSendFixture() project.Service {
	return project.Service{
		Name: "open-agents", URL: "ws://localhost:8989/ws",
		Protocol: &project.Protocol{
			TypePath: "type",
			Roles: map[string]*project.ProtocolRole{
				"web":    {Params: map[string]string{"type": "web"}},
				"bridge": {Params: map[string]string{"type": "bridge"}},
			},
		},
		Vocabulary: workflowVocabFixture(), // edges: web->bridge workflow:start/pause/cancel/start_task + web->web session:send
	}
}

// workflowVocabFixture declares the web-origin workflow send edges plus the
// web->web session:send pair. Edge directions follow the dogfood vocab's
// from_role/to_role convention, but the generator is gated on vocab/protocol
// roles, not on these directions.
func workflowVocabFixture() *project.Vocabulary {
	return &project.Vocabulary{
		Edges: []project.VocabEdge{
			{FromRole: "web", ToRole: "bridge", Type: "workflow:start", Trigger: "message_handled"},
			{FromRole: "web", ToRole: "bridge", Type: "workflow:pause", Trigger: "message_handled"},
			{FromRole: "web", ToRole: "bridge", Type: "workflow:cancel", Trigger: "message_handled"},
			{FromRole: "web", ToRole: "bridge", Type: "workflow:start_task", Trigger: "message_handled"},
			{FromRole: "web", ToRole: "web", Type: "session:send", Trigger: "message_handled",
				Delivery: project.VocabDelivery{Mode: "broadcast_web", ExcludeSender: true}},
		},
	}
}

// caseByID indexes cases by ID with the leading "ws-" prefix stripped, so
// tests can key on the human-readable "<svc>-<role>-<id>" form.
func caseByID(cases []agent.TestCase) map[string]*agent.TestCase {
	out := map[string]*agent.TestCase{}
	for i := range cases {
		out[strings.TrimPrefix(cases[i].ID, "ws-")] = &cases[i]
	}
	return out
}

// hasStep scans c.Steps for a step matching action plus (Type for ws_receive
// or ConnectionID for ws_connect). An empty typeOrConn matches any value.
func hasStep(c *agent.TestCase, action, typeOrConn string) bool {
	for _, s := range c.Steps {
		if s.Action != action {
			continue
		}
		switch action {
		case "ws_receive":
			if typeOrConn == "" || s.Type == typeOrConn {
				return true
			}
		case "ws_connect":
			if typeOrConn == "" || s.ConnectionID == typeOrConn {
				return true
			}
		default:
			return true
		}
	}
	return false
}

func TestMissionSendCases_Assembly(t *testing.T) {
	cases := missionSendCases(missionSendFixture(), map[string]bool{"bridge": true})
	byID := caseByID(cases)
	if len(cases) != 5 {
		t.Fatalf("expected 5 cases (start, start_task, pause, cancel, session:send), got %d", len(cases))
	}
	// start_task: send + hard receive of the deterministic echo.
	c := byID["open-agents-wf-start-task"]
	if c == nil {
		t.Fatal("start_task case missing")
	}
	if !hasStep(c, "ws_receive", "workflow:task_started") {
		t.Fatal("start_task must hard-receive workflow:task_started")
	}
	// pause: send-only, NO receive (bridge only logs — spec §8).
	p := byID["open-agents-wf-workflow-pause"]
	if p == nil || hasStep(p, "ws_receive", "") {
		t.Fatal("pause must be send-only")
	}
	// session:send: two web connections, receive on the second.
	s := byID["open-agents-wf-session-send-web"]
	if s == nil || !hasStep(s, "ws_connect", "web-2") {
		t.Fatal("session:send needs a second web connection")
	}
}

func TestMissionSendCases_NoBridgeReal_EmitsNothing(t *testing.T) {
	if got := missionSendCases(missionSendFixture(), nil); got != nil {
		t.Fatalf("emitted %d cases without a real bridge", len(got))
	}
}
