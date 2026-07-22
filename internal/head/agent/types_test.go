package agent

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCaseServiceFieldRoundTrip(t *testing.T) {
	src := `{
		"id": "tc-001",
		"name": "Test Create User",
		"target": "/api/v1/users",
		"method": "POST",
		"action": "create user",
		"service": "api-gateway"
	}`
	var tc TestCase
	err := json.Unmarshal([]byte(src), &tc)
	require.NoError(t, err)
	require.Equal(t, "api-gateway", tc.Service)
}

func TestTestCaseStepsRoundTrip(t *testing.T) {
	in := TestCase{
		ID: "ws-rt-web-flow", Target: "http://x", Action: "ws_flow",
		Steps: []TestStep{
			{Action: "ws_connect", ConnectionID: "c1", Role: "web"},
			{Action: "ws_send", ConnectionID: "c1", Message: `{"type":"device:command"}`},
			{Action: "ws_receive", ConnectionID: "c1", Type: "device:ack",
				Asserts: map[string]any{"payload.approved": true}},
		},
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var out TestCase
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, in.Steps, out.Steps)

	// Steps is optional: a case without Steps round-trips with no steps field.
	bare, err := json.Marshal(TestCase{ID: "x", Action: "api_request"})
	require.NoError(t, err)
	assert.NotContains(t, string(bare), `"steps"`)
}
