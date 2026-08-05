package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/binoctal/cerberus/internal/project"
)

// BuildEdgeSteps translates one vocabulary edge into the WS choreography that
// exercises it: connect both roles (plus a second web client when a web->web
// broadcast excludes the sender), send the edge's message, and receive it on
// the ToRole connection. outbound is the JSON message to ws_send, with
// payload.{field} populated from deviceID when the edge declares a RouteField.
//
// This is the single implementation of "how the Agent consumes a vocab edge";
// the integration test (vocabulary_driven_test.go) and unit tests both call it.
func BuildEdgeSteps(e project.VocabEdge, deviceID string) (steps []TestStep, outbound string) {
	steps = []TestStep{
		{Action: "ws_connect", Role: "web", ConnectionID: "c-web"},
		{Action: "ws_connect", Role: "bridge", ConnectionID: "c-bridge"},
		{Action: "ws_receive", ConnectionID: "c-web", Type: "device:online", Timeout: 3},
	}
	if e.FromRole == "web" && e.ToRole == "web" {
		steps = append(steps, TestStep{Action: "ws_connect", Role: "web", ConnectionID: "c-web-2"})
	}
	sender := "c-" + e.FromRole
	receiver := "c-" + e.ToRole
	if e.FromRole == "web" && e.ToRole == "web" {
		receiver = "c-web-2"
	}

	msg := fmt.Sprintf(`{"type":%q}`, e.Type)
	if e.RouteField != "" {
		field := strings.TrimPrefix(e.RouteField, "payload.")
		body, err := json.Marshal(map[string]any{
			"type":    e.Type,
			"payload": map[string]any{field: deviceID},
		})
		if err != nil {
			// Marshal of a string-keyed map of strings cannot fail; fall back
			// to the bare type so the edge is still exercisable.
			msg = fmt.Sprintf(`{"type":%q}`, e.Type)
		} else {
			msg = string(body)
		}
	}
	outbound = msg

	steps = append(steps,
		TestStep{Action: "ws_send", ConnectionID: sender, Message: msg},
		TestStep{Action: "ws_receive", ConnectionID: receiver, Type: e.Type, Timeout: 3},
	)
	return steps, outbound
}
