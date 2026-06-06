package examiner

import "github.com/binoctal/cerberus/internal/head/agent"

// VerdictPolicy applies the Uncertain 3-level degradation chain:
// Level 1: Self-Refine (already done by Judge)
// Level 2: Checker-only (deterministic fallback)
// Level 3: Mark as pending_review
func VerdictPolicy(judgeResult *JudgeResult, stepResult agent.StepResult) FinalVerdict {
	v := FinalVerdict{
		Status:                judgeResult.Status,
		ExistenceConfidence:   judgeResult.ExistenceConfidence,
		CorrectnessConfidence: judgeResult.CorrectnessConfidence,
		Reasoning:             judgeResult.Reasoning,
		SelfCritique:          judgeResult.SelfCritique,
		CritiqueTriggered:     judgeResult.CritiqueTriggered,
		StepResult:            stepResult,
	}

	// If not uncertain, accept the judge result as-is.
	if judgeResult.Status != StatusUncertain {
		return v
	}

	// Level 2: Checker-only — deterministic fallback.
	// If the step passed at HTTP level (2xx) but judge is uncertain,
	// downgrade to "pass" with low confidence.
	if stepResult.Status == agent.StepPassed && stepResult.LastObs.StatusCode >= 200 &&
		stepResult.LastObs.StatusCode < 300 {
		v.Status = StatusPass
		v.CorrectnessConfidence = 0.5 // Low — passed HTTP but uncertain correctness
		v.Reasoning = "Degraded from uncertain: HTTP 2xx confirmed, correctness uncertain"
		v.DegradedLevel = 2
		return v
	}

	// Level 3: Pending review.
	v.Status = StatusUncertain
	v.PendingReview = true
	v.DegradedLevel = 3
	v.Reasoning = "Degraded to Level 3 (pending review): unable to determine verdict"
	return v
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
}

// NeedsReview returns true if this verdict requires human review.
func (v FinalVerdict) NeedsReview() bool {
	return v.PendingReview
}
