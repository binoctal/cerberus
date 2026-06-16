package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/types"
)

// recordEvidence stores an observation as evidence linked to the trace.
func (r *ReActLoop) recordEvidence(ctx context.Context, traceID int64, phase string, action types.TypedAction, result types.ExecutorResult) {
	content, _ := json.Marshal(map[string]any{
		"phase":    phase,
		"type":     string(action.GetActionType()),
		"target":   action.Target(),
		"success":  result.Success(),
		"summary":  result.Summary(),
		"evidence": result.Evidence(),
	})
	_, err := r.store.CreateEvidence(ctx, traceID, "agent_observation", string(content))
	if err != nil {
		r.logger.Warn("record evidence", zap.Error(err))
	}
}

// formatResultContext builds context for the Steer prompt.
func formatResultContext(tc *TestCase, result types.ExecutorResult, attempt int) string {
	if attempt == 1 {
		return fmt.Sprintf("Target: %s\nMethod: %s", tc.Target, tc.Method)
	}
	if result != nil {
		return fmt.Sprintf("Target: %s\nMethod: %s\nPrevious: %s",
			tc.Target, tc.Method, result.Summary())
	}
	return fmt.Sprintf("Target: %s\nMethod: %s", tc.Target, tc.Method)
}

func evidenceType(result types.ExecutorResult) string {
	if result == nil {
		return "none"
	}
	return result.Evidence().Type
}
