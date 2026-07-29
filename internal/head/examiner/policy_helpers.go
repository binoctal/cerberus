package examiner

import (
	"fmt"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/types"
)

// newFinalVerdict creates a new FinalVerdict from JudgeResult
func newFinalVerdict(judgeResult *JudgeResult, stepResult agent.StepResult) FinalVerdict {
	return FinalVerdict{
		Status:                judgeResult.Status,
		ExistenceConfidence:   judgeResult.ExistenceConfidence,
		CorrectnessConfidence: judgeResult.CorrectnessConfidence,
		Reasoning:             judgeResult.Reasoning,
		SelfCritique:          judgeResult.SelfCritique,
		CritiqueTriggered:     judgeResult.CritiqueTriggered,
		RedispatchHint:        judgeResult.RedispatchHint,
		StepResult:            stepResult,
	}
}

// applyThresholdDowngrade applies Level 1 degradation for low-confidence passes
func applyThresholdDowngrade(v FinalVerdict, judgeResult *JudgeResult, confThreshold float64) FinalVerdict {
	v.Status = StatusUncertain
	v.Reasoning = fmt.Sprintf("Degraded from pass: correctness confidence %.2f below threshold %.2f. %s",
		judgeResult.CorrectnessConfidence, confThreshold, judgeResult.Reasoning)
	v.DegradedLevel = 1
	return v
}

// isHTTP2xx checks if step result indicates HTTP 2xx success
func isHTTP2xx(stepResult agent.StepResult) bool {
	if stepResult.Status != agent.StepPassed || stepResult.Result == nil {
		return false
	}

	httpRes, ok := stepResult.Result.(types.HTTPResult)
	if !ok {
		return false
	}

	return httpRes.StatusCode >= 200 && httpRes.StatusCode < 300
}

// applyCheckerOnlyDowngrade applies Level 2 degradation (checker-only fallback)
func applyCheckerOnlyDowngrade(v FinalVerdict) FinalVerdict {
	v.Status = StatusPass
	v.CorrectnessConfidence = 0.5 // Low confidence
	v.Reasoning = "Degraded from uncertain: HTTP 2xx confirmed, correctness uncertain"
	v.DegradedLevel = 2
	return v
}

// applyPendingReviewDowngrade applies Level 3 degradation (pending review)
func applyPendingReviewDowngrade(v FinalVerdict) FinalVerdict {
	v.Status = StatusUncertain
	v.PendingReview = true
	v.DegradedLevel = 3
	v.Reasoning = "Degraded to Level 3 (pending review): unable to determine verdict"
	return v
}
