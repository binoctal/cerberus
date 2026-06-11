package mcp

import (
	"context"
	"sync"

	"github.com/binoctal/cerberus/internal/escalation"
)

// MCPGate implements escalation.Gate for MCP mode.
// It blocks on Check() until SendDecision() is called from the MCP tool handler.
type MCPGate struct {
	mu       sync.Mutex
	pending  *escalation.Event
	decideCh chan escalation.Decision
}

// NewMCPGate creates an escalation gate for MCP mode.
func NewMCPGate() *MCPGate {
	return &MCPGate{
		decideCh: make(chan escalation.Decision, 1),
	}
}

// Check blocks until a decision is sent via SendDecision.
func (g *MCPGate) Check(ctx context.Context, event escalation.Event) escalation.Decision {
	g.mu.Lock()
	g.pending = &event
	g.mu.Unlock()

	select {
	case d := <-g.decideCh:
		g.mu.Lock()
		g.pending = nil
		g.mu.Unlock()
		return d
	case <-ctx.Done():
		g.mu.Lock()
		g.pending = nil
		g.mu.Unlock()
		return escalation.EscalationContinue
	}
}

// SendDecision sends a user decision to unblock a pending Check().
func (g *MCPGate) SendDecision(d escalation.Decision) {
	g.decideCh <- d
}

// PendingEvent returns the current escalation event waiting for a decision, or nil.
func (g *MCPGate) PendingEvent() *escalation.Event {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.pending
}
