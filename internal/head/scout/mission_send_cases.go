package scout

import (
	"fmt"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// missionSendCases emits the web-origin workflow sends the room relay routes
// at the REAL bridge (web→bridge via payload.deviceId) plus the web→web
// session:send pair. Reply expectations follow the bridge's actual behavior
// (spec §8): start/start_task echo task_started deterministically; pause and
// cancel only log — send-only steps whose coverage is send-side credit
// (coverage.go:286-293 credits a send whose connection maps the declared
// FromRole and whose ToRole is real). task_answer/task_guidance are NOT
// emitted here: their effect is conditional (pendingQuestion / live session)
// and they stay vocab partial marks.
//
// jobId is the literal "cerberus-wf-seed", NOT a {{case.*}} placeholder:
// case params are only populated by http_request Capture steps, and these
// ws-only cases would ship the placeholder verbatim. Routing is
// payload.deviceId-only (room.ts:431-441); jobId is bridge-internal, so the
// literal does not affect send-side credit or the task_started echo.
const wfSeedJobID = "cerberus-wf-seed"

func missionSendCases(svc project.Service, realRoles map[string]bool) []agent.TestCase {
	if !realRoles["bridge"] || svc.Protocol == nil || svc.Vocabulary == nil {
		return nil
	}
	deviceID := "{{bridge.deviceId}}"
	newCase := func(id, name string, steps []agent.TestStep) agent.TestCase {
		return agent.TestCase{ID: wsCaseID(svc.Name, "wf", id), Name: name,
			Service: svc.Name, Target: svc.URL, Action: "ws_flow",
			Expectation: name, Priority: 0.6, Steps: steps}
	}
	connect := agent.TestStep{Action: "ws_connect", ConnectionID: "web", Role: "web"}
	var cases []agent.TestCase
	sendWithEcho := []struct{ send, echo, id, name string }{
		{"workflow:start_task", "workflow:task_started", "start-task",
			"web sends workflow:start_task, real bridge echoes workflow:task_started"},
		{"workflow:start", "workflow:task_started", "start",
			"web sends workflow:start with tasks[], real bridge echoes workflow:task_started per task"},
	}
	for _, e := range sendWithEcho {
		payload := map[string]any{"deviceId": deviceID}
		if e.send == "workflow:start" {
			// bridge.go:2601-2627 emits task_started only per payload.tasks item.
			payload["jobId"] = wfSeedJobID
			payload["tasks"] = []any{map[string]any{"id": "t-seed"}}
		}
		cases = append(cases, newCase(e.id, e.name, []agent.TestStep{
			connect,
			{Action: "ws_send", ConnectionID: "web", Message: wsSendBodyAny(e.send, payload)},
			{Action: "ws_receive", ConnectionID: "web", Type: e.echo, Timeout: 30},
		}))
	}
	for _, send := range []string{"workflow:pause", "workflow:cancel"} {
		cases = append(cases, newCase(sendTypeToID(send),
			fmt.Sprintf("web sends %s at the real bridge (send-side credit; bridge logs only)", send),
			[]agent.TestStep{
				connect,
				{Action: "ws_send", ConnectionID: "web", Message: wsSendBodyAny(send, map[string]any{"deviceId": deviceID, "jobId": wfSeedJobID})},
			}))
	}
	// web→web session:send — broadcast excludes the sender (room.ts:449-460),
	// so a second web connection receives it.
	cases = append(cases, newCase("session-send-web",
		"web session:send relayed to a second web connection",
		[]agent.TestStep{
			connect,
			{Action: "ws_connect", ConnectionID: "web-2", Role: "web"},
			{Action: "ws_send", ConnectionID: "web", Message: wsSendBodyAny("session:send", map[string]any{"deviceId": deviceID, "message": "hello from cerberus"})},
			{Action: "ws_receive", ConnectionID: "web-2", Type: "session:send", Timeout: 15},
		}))
	return cases
}

func sendTypeToID(typ string) string { return sanitizeTypeID(typ) }
