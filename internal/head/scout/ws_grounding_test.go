package scout

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

func TestWsTypeGrounded(t *testing.T) {
	// web awaits device:online; bridge awaits devices:sync.
	proto := &project.Protocol{Roles: map[string]*project.ProtocolRole{
		"web":    {Handshake: &project.RoleHandshake{AwaitType: "device:online"}},
		"bridge": {Handshake: &project.RoleHandshake{AwaitType: "devices:sync"}},
	}}
	// Goal names permission:response (a receive type, no send verb before it).
	goal := "verify permission:response approved=true"

	tests := []struct {
		name    string
		typ     string
		aliases []string
		want    bool
	}{
		{"handshake await_type grounded", "device:online", nil, true},
		{"handshake grounded via dash-colon sanitize", "devices-sync", nil, true}, // == devices:sync
		{"goal-named type grounded", "permission:response", nil, true},
		{"invented type ungrounded", "message", nil, false},
		{"empty type no aliases ungrounded", "", nil, false},
		{"ungrounded type rescued by grounded alias", "bogus", []string{"device:online"}, true},
		{"ungrounded type with ungrounded alias", "bogus", []string{"other"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, wsTypeGrounded(tc.typ, tc.aliases, proto, goal))
		})
	}

	// nil proto: only goal-named types ground.
	assert.True(t, wsTypeGrounded("permission:response", nil, nil, goal),
		"goal type grounds without a proto")
	assert.False(t, wsTypeGrounded("device:online", nil, nil, goal),
		"handshake type does NOT ground without a proto")
}

func TestLLMWSFlowSound(t *testing.T) {
	proto := &project.Protocol{Roles: map[string]*project.ProtocolRole{
		"web": {Handshake: &project.RoleHandshake{AwaitType: "device:online"}},
	}}
	goal := "verify device:online"

	t.Run("connect-only is sound", func(t *testing.T) {
		tc := &agent.TestCase{Action: "ws_flow", Steps: []agent.TestStep{
			{Action: "ws_connect", Role: "web"},
		}}
		assert.True(t, llmWSFlowSound(tc, proto, goal))
	})

	t.Run("send-only is sound", func(t *testing.T) {
		tc := &agent.TestCase{Action: "ws_flow", Steps: []agent.TestStep{
			{Action: "ws_connect", Role: "web"},
			{Action: "ws_send", Message: `{"type":"device:command"}`},
		}}
		assert.True(t, llmWSFlowSound(tc, proto, goal))
	})

	t.Run("grounded receive is sound", func(t *testing.T) {
		tc := &agent.TestCase{Action: "ws_flow", Steps: []agent.TestStep{
			{Action: "ws_connect", Role: "web"},
			{Action: "ws_receive", Type: "device:online"},
		}}
		assert.True(t, llmWSFlowSound(tc, proto, goal))
	})

	t.Run("invented receive is unsound", func(t *testing.T) {
		tc := &agent.TestCase{Action: "ws_flow", Steps: []agent.TestStep{
			{Action: "ws_connect", Role: "web"},
			{Action: "ws_receive", Type: "message"},
		}}
		assert.False(t, llmWSFlowSound(tc, proto, goal))
	})

	t.Run("mixed grounded and invented is unsound", func(t *testing.T) {
		tc := &agent.TestCase{Action: "ws_flow", Steps: []agent.TestStep{
			{Action: "ws_receive", Type: "device:online"},
			{Action: "ws_receive", Type: "message"},
		}}
		assert.False(t, llmWSFlowSound(tc, proto, goal))
	})

	t.Run("ungrounded type grounded alias is sound", func(t *testing.T) {
		tc := &agent.TestCase{Action: "ws_flow", Steps: []agent.TestStep{
			{Action: "ws_receive", Type: "bogus", Aliases: []string{"device:online"}},
		}}
		assert.True(t, llmWSFlowSound(tc, proto, goal))
	})
}
