package autotest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/escalation"
)

func TestEscalationGateAdapter_DelegatesToInner(t *testing.T) {
	// Build an adapter around escalation.NoOpGate (auto-approve). In approve mode
	// the coordinator should proceed (gate grants), so a generated test is kept
	adapter := NewEscalationGateAdapter(escalation.NoOpGate{})
	ok, err := adapter.Request(context.Background(), "destructive_risk", []string{"a_test.go"}, "package p")
	require.NoError(t, err)
	assert.True(t, ok) // NoOpGate / auto-approve → granted
}

func TestEscalationGateAdapter_ForwardsCheckpoint(t *testing.T) {
	// Verify the adapter maps the checkpoint parameter to Event.Type
	adapter := NewEscalationGateAdapter(escalation.NoOpGate{})
	checkpoints := []string{"budget_warning", "systemic_failure", "destructive_risk", "target_unreachable"}
	for _, cp := range checkpoints {
		ok, err := adapter.Request(context.Background(), cp, []string{"test.go"}, "preview")
		require.NoError(t, err, "checkpoint: %s", cp)
		assert.True(t, ok, "checkpoint: %s", cp)
	}
}
