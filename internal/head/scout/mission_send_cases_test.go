package scout

import (
	"encoding/json"
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
				"admin":  {CredentialRef: "admin-actor"},
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
	if len(cases) != 6 {
		t.Fatalf("expected 6 cases (start, start_task, pause, cancel, task_assign, session:send), got %d", len(cases))
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
	// task_assign: drives the real task session (progress receive), then
	// answer + guidance sends against the live session. No question receive:
	// the [QUESTION] marker only fires on the PTY fallback (mission case).
	a := byID["open-agents-wf-task-assign"]
	if a == nil {
		t.Fatal("task_assign case missing")
	}
	if !hasStep(a, "ws_receive", "workflow:task_progress") {
		t.Fatal("task_assign must receive task_progress")
	}
	if hasStep(a, "ws_receive", "workflow:task_question") {
		t.Fatal("task_assign must not await task_question (ACP path never echoes the prompt)")
	}
	if !hasStep(a, "ws_send", "") {
		t.Fatal("task_assign must send")
	}
	sends := map[string]int{}
	for _, st := range a.Steps {
		if st.Action == "ws_send" {
			var m struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal([]byte(st.Message), &m); err != nil {
				t.Fatalf("bad send message: %v", err)
			}
			sends[m.Type]++
		}
	}
	for _, typ := range []string{"workflow:task_assign", "workflow:task_answer", "workflow:task_guidance"} {
		if sends[typ] == 0 {
			t.Fatalf("task_assign case missing %s send", typ)
		}
	}
	// No ws-only case may ship an unresolvable {{case.*}} placeholder: case
	// params are only populated by http_request Capture steps, which none of
	// these cases run — the literal would reach the bridge verbatim.
	for _, c := range cases {
		for _, st := range c.Steps {
			if strings.Contains(st.Message, "{{case.") {
				t.Fatalf("%s ships unresolvable placeholder in %q", c.ID, st.Message)
			}
		}
	}
}

func TestMissionSendCases_NoBridgeReal_EmitsNothing(t *testing.T) {
	if got := missionSendCases(missionSendFixture(), nil); got != nil {
		t.Fatalf("emitted %d cases without a real bridge", len(got))
	}
}
