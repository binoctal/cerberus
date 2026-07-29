package examiner

import (
	"github.com/binoctal/cerberus/internal/head/agent"
)

// VerdictPolicy applies the Uncertain 3-level degradation chain:
// Level 1: Self-Refine (already done by Judge)
// Level 2: Checker-only (deterministic fallback)
// Level 3: Mark as pending_review
func VerdictPolicy(judgeResult *JudgeResult, stepResult agent.StepResult, confThreshold float64) FinalVerdict {
	// Phase 1: Create initial verdict
	v := newFinalVerdict(judgeResult, stepResult)

	// Phase 2: Threshold filtering (Level 1 degradation)
	if judgeResult.Status == StatusPass && confThreshold > 0 && judgeResult.CorrectnessConfidence < confThreshold {
		return applyThresholdDowngrade(v, judgeResult, confThreshold)
	}

	// Phase 3: If not uncertain, accept judge result as-is
	if judgeResult.Status != StatusUncertain {
		return v
	}

	// Phase 4: Level 2 degradation (checker-only fallback)
	if isHTTP2xx(stepResult) {
		return applyCheckerOnlyDowngrade(v)
	}

	// Phase 5: Level 3 degradation (pending review)
	return applyPendingReviewDowngrade(v)
}

// FinalVerdict is the final assessment after all degradation levels.
type FinalVerdict struct {
	Status                JudgeStatus
	ExistenceConfidence   float64
	CorrectnessConfidence float64
	Reasoning             string
	SelfCritique          string
	CritiqueTriggered     bool
	PendingReview         bool
	DegradedLevel         int // 0 = not degraded, 1 = Self-Refine, 2 = checker-only, 3 = pending
	StepResult            agent.StepResult
	RedispatchHint        agent.RedispatchHint
}

// NeedsReview returns true if this verdict requires human review.
func (v FinalVerdict) NeedsReview() bool {
	return v.PendingReview
}

// ShouldAutoFix returns true if the failed verdict qualifies for auto-fix
// based on the mode and invariant severity.
func ShouldAutoFix(verdict FinalVerdict, mode string, severity string) bool {
	if mode == "off" || mode == "" {
		return false
	}
	if verdict.Status != StatusFail {
		return false
	}
	switch mode {
	case "low_only":
		return severity == "low"
	case "aggressive":
		return severity == "low" || severity == "medium"
	default:
		return false
	}
}
