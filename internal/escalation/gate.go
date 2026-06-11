// Package escalation defines the escalation interface for critical event handling.
package escalation

import "context"

// Decision actions returned by an EscalationGate.
const (
	DecisionContinue = "continue"
	DecisionAbort    = "abort"
	DecisionSkipCase = "skip_case"
)

// Gate is called at critical points during session execution.
// Implementations may block (e.g. wait for user input via MCP) or return immediately.
type Gate interface {
	Check(ctx context.Context, event Event) Decision
}

// Event describes a critical situation that the AI cannot safely decide on its own.
type Event struct {
	Type      string         `json:"type"`
	Message   string         `json:"message"`
	SessionID string         `json:"session_id"`
	Data      map[string]any `json:"data,omitempty"`
}

// Decision is the user's (or default) response to an escalation event.
type Decision struct {
	Action  string `json:"action"`
	Payload string `json:"payload,omitempty"`
}

// EscalationContinue is the default "proceed autonomously" decision.
var EscalationContinue = Decision{Action: DecisionContinue}

// NoOpGate always returns "continue" — used in CLI mode where no MCP is active.
type NoOpGate struct{}

func (NoOpGate) Check(_ context.Context, _ Event) Decision {
	return EscalationContinue
}
