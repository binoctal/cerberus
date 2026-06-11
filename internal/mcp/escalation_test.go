package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/binoctal/cerberus/internal/escalation"
	"github.com/stretchr/testify/assert"
)

func TestMCPGate_BlocksUntilDecision(t *testing.T) {
	gate := NewMCPGate()

	done := make(chan escalation.Decision, 1)
	go func() {
		d := gate.Check(context.Background(), escalation.Event{
			Type:    "budget_warning",
			Message: "80% budget used",
		})
		done <- d
	}()

	// Gate should block — no decision yet.
	select {
	case <-done:
		t.Fatal("gate should block until decision is sent")
	case <-time.After(50 * time.Millisecond):
	}

	// Send decision.
	gate.SendDecision(escalation.Decision{Action: escalation.DecisionAbort})

	// Now it should unblock.
	select {
	case d := <-done:
		assert.Equal(t, escalation.DecisionAbort, d.Action)
	case <-time.After(1 * time.Second):
		t.Fatal("gate should unblock after decision is sent")
	}
}

func TestMCPGate_PendingEvent(t *testing.T) {
	gate := NewMCPGate()
	assert.Nil(t, gate.PendingEvent())

	go gate.Check(context.Background(), escalation.Event{Type: "systemic_failure", Message: "5 consecutive failures"})

	// Wait for event to be pending.
	time.Sleep(50 * time.Millisecond)

	evt := gate.PendingEvent()
	assert.NotNil(t, evt)
	assert.Equal(t, "systemic_failure", evt.Type)

	// Clean up.
	gate.SendDecision(escalation.EscalationContinue)
}

func TestMCPGate_ContinueByDefault(t *testing.T) {
	gate := NewMCPGate()
	gate.SendDecision(escalation.EscalationContinue)
	// Verify it doesn't panic.
}
