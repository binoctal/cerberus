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

func TestMCPGate_ContextCancellation(t *testing.T) {
	gate := NewMCPGate()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan escalation.Decision, 1)
	go func() {
		d := gate.Check(ctx, escalation.Event{
			Type:    "budget_warning",
			Message: "50% used",
		})
		done <- d
	}()

	// Wait for pending event to be set.
	requireEventual(t, func() bool { return gate.PendingEvent() != nil }, time.Second)

	// Cancel context instead of sending decision.
	cancel()

	select {
	case d := <-done:
		assert.Equal(t, escalation.DecisionContinue, d.Action, "cancelled context should return EscalationContinue")
	case <-time.After(time.Second):
		t.Fatal("gate should unblock on context cancellation")
	}

	// Pending event should be cleared.
	assert.Nil(t, gate.PendingEvent())
}

func TestMCPGate_PendingEventClearedAfterDecision(t *testing.T) {
	gate := NewMCPGate()

	done := make(chan struct{})
	go func() {
		gate.Check(context.Background(), escalation.Event{Type: "destructive_risk", Message: "drop table"})
		close(done)
	}()

	requireEventual(t, func() bool { return gate.PendingEvent() != nil }, time.Second)

	gate.SendDecision(escalation.Decision{Action: escalation.DecisionSkipCase})
	<-done // ensure Check returns

	// Pending event should be nil after decision is processed.
	assert.Nil(t, gate.PendingEvent())
}

func TestMCPGate_SendDecisionBeforeCheck(t *testing.T) {
	gate := NewMCPGate()

	// Send decision before Check is called — channel has buffer of 1.
	gate.SendDecision(escalation.Decision{Action: escalation.DecisionAbort, Payload: "test"})

	// Check should return immediately with the buffered decision.
	d := gate.Check(context.Background(), escalation.Event{Type: "test", Message: "pre-sent"})
	assert.Equal(t, "abort", d.Action)
	assert.Equal(t, "test", d.Payload)
}

func TestMCPGate_MultipleEventsSequentially(t *testing.T) {
	gate := NewMCPGate()

	for i := 0; i < 3; i++ {
		done := make(chan escalation.Decision, 1)
		go func() {
			d := gate.Check(context.Background(), escalation.Event{
				Type:    "test",
				Message: "event",
			})
			done <- d
		}()

		requireEventual(t, func() bool { return gate.PendingEvent() != nil }, time.Second)
		gate.SendDecision(escalation.Decision{Action: escalation.DecisionContinue})

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("Check should complete")
		}
	}
}

// requireEventual polls fn until it returns true or timeout elapses.
func requireEventual(t *testing.T, fn func() bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}
