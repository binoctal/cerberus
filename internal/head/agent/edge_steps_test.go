package agent

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/project"
)

func TestBuildEdgeSteps_WebToBridgeRouteField(t *testing.T) {
	edge := project.VocabEdge{
		FromRole: "web", ToRole: "bridge", Type: "session:start",
		Trigger: "message_handled", Delivery: project.VocabDelivery{Mode: "send_bridge_by_device"},
		RouteField: "payload.deviceId",
	}
	steps, outbound := BuildEdgeSteps(edge, "dev-42")
	require.NotEmpty(t, steps)
	assert.Contains(t, outbound, `"type":"session:start"`)
	assert.Contains(t, outbound, `"deviceId":"dev-42"`)
}

func TestBuildEdgeSteps_NoRouteFieldBareType(t *testing.T) {
	edge := project.VocabEdge{
		FromRole: "bridge", ToRole: "web", Type: "workflow:task_progress",
		Trigger: "message_handled", Delivery: project.VocabDelivery{Mode: "broadcast_web"},
	}
	steps, outbound := BuildEdgeSteps(edge, "ignored")
	require.NotEmpty(t, steps)
	var msg map[string]any
	require.NoError(t, json.Unmarshal([]byte(outbound), &msg))
	assert.Equal(t, "workflow:task_progress", msg["type"])
	assert.Len(t, msg, 1, "no route_field → bare type object, no payload")
}

func TestBuildEdgeSteps_WebToWebAddsSecondClient(t *testing.T) {
	edge := project.VocabEdge{
		FromRole: "web", ToRole: "web", Type: "session:send",
		Trigger:    "message_handled",
		Delivery:   project.VocabDelivery{Mode: "broadcast_web", ExcludeSender: true},
		RouteField: "payload.deviceId",
	}
	steps, _ := BuildEdgeSteps(edge, "dev-1")
	// A web->web broadcast excludes the sender, so a second web connection
	// must be present to observe the relay.
	conns := map[string]bool{}
	for _, s := range steps {
		conns[s.ConnectionID] = true
	}
	assert.True(t, conns["c-web"], "sender connection present")
	assert.True(t, conns["c-web-2"], "second web client present as observer")
}

func TestBuildEdgeSteps_ReceiveOnCorrectConnection(t *testing.T) {
	edge := project.VocabEdge{
		FromRole: "web", ToRole: "bridge", Type: "session:start",
		Trigger: "message_handled", Delivery: project.VocabDelivery{Mode: "send_bridge_by_device"},
		RouteField: "payload.deviceId",
	}
	steps, _ := BuildEdgeSteps(edge, "dev-9")
	var receive *TestStep
	for i := range steps {
		if steps[i].Action == "ws_receive" && steps[i].Type == "session:start" {
			receive = &steps[i]
			break
		}
	}
	require.NotNil(t, receive, "must have a ws_receive for the edge type")
	assert.Equal(t, "c-bridge", receive.ConnectionID, "receive on the ToRole connection")
}
