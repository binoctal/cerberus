package session

import (
	"testing"

	"github.com/binoctal/cerberus/internal/head/agent"
)

func TestBrowserFlowNotRepairable(t *testing.T) {
	tc := agent.TestCase{Action: "browser_flow", Target: "http://localhost:5183",
		Steps: []agent.TestStep{{Action: "browser_goto"}}}
	if isRepairable(&tc) {
		t.Error("browser_flow must not be repairable — repair_case emits HTTP/WS shapes only")
	}
}
