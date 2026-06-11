package escalation

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNoOpGate_AlwaysContinues(t *testing.T) {
	gate := NoOpGate{}
	decision := gate.Check(context.Background(), Event{
		Type:    "budget_warning",
		Message: "80% budget used",
	})
	assert.Equal(t, EscalationContinue, decision)
}

func TestNoOpGate_AllEventTypes(t *testing.T) {
	gate := NoOpGate{}
	types := []string{"budget_warning", "systemic_failure", "destructive_risk", "target_unreachable"}
	for _, et := range types {
		decision := gate.Check(context.Background(), Event{Type: et})
		assert.Equal(t, EscalationContinue, decision, "event type: %s", et)
	}
}
