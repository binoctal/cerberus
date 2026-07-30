package session

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/head/agent"
)

// TestIsRepairable: only HTTP (Method set, plain) and WS (Steps) cases are
// repairable — the repair_case tool can only emit those two shapes. Other
// executor types (process_exec/build, code_*, browser, file, mcp_call, wait,
// navigate) cannot be correctly repaired and must be skipped to avoid broken
// HTTP-shaped replacements.
func TestIsRepairable(t *testing.T) {
	cases := []struct {
		name string
		tc   agent.TestCase
		want bool
	}{
		{"HTTP plain", agent.TestCase{Method: "GET", Target: "/u"}, true},
		{"HTTP post", agent.TestCase{Method: "POST", Target: "/u", Body: `{"a":1}`}, true},
		{"WS flow", agent.TestCase{Action: "ws_flow", Target: "wss://x",
			Steps: []agent.TestStep{{Action: "ws_connect"}}}, true},
		{"process_exec", agent.TestCase{Action: "process_exec", Target: "go build ./..."}, false},
		{"process_build", agent.TestCase{Action: "process_build", Target: "go test ./..."}, false},
		{"code_analyze", agent.TestCase{Action: "code_analyze"}, false},
		{"navigate", agent.TestCase{Action: "navigate", Target: "/page"}, false},
		{"browser click", agent.TestCase{Action: "click"}, false},
		{"mcp_call", agent.TestCase{Action: "mcp_call"}, false},
		{"wait", agent.TestCase{Action: "wait"}, false},
		{"no method, no steps, no action", agent.TestCase{Target: "/u"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, isRepairable(&c.tc))
		})
	}
}
