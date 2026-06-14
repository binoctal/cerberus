package autotest

import (
	"context"
	"fmt"
	"strings"

	"github.com/binoctal/cerberus/internal/escalation"
)

// EscalationGateAdapter adapts internal/escalation.Gate to autotest.RequestGate.
type EscalationGateAdapter struct {
	Inner escalation.Gate
}

func NewEscalationGateAdapter(inner escalation.Gate) EscalationGateAdapter {
	return EscalationGateAdapter{Inner: inner}
}

func (a EscalationGateAdapter) Request(ctx context.Context, checkpoint string, files []string, preview string) (bool, error) {
	summary := fmt.Sprintf("AutoTest wants to write %d test file(s): %s\nPreview:\n%s",
		len(files), strings.Join(files, ", "), preview)

	// Delegate to escalation.Gate.Check, mapping RequestGate params to Event
	decision := a.Inner.Check(ctx, escalation.Event{
		Type:    checkpoint,
		Message: summary,
	})

	// Map escalation.Decision to RequestGate's (bool, error) return
	// DecisionContinue → proceed (true, nil)
	// DecisionAbort → stop (false, nil)
	// DecisionSkipCase → stop (false, nil)
	switch decision.Action {
	case escalation.DecisionContinue:
		return true, nil
	case escalation.DecisionAbort, escalation.DecisionSkipCase:
		return false, nil
	default:
		// Unknown action → conservative: deny
		return false, nil
	}
}
