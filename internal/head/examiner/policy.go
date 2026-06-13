package examiner

import (
	"fmt"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/types"
)

// VerdictPolicy applies the Uncertain 3-level degradation chain:
// Level 1: Self-Refine (already done by Judge)
// Level 2: Checker-only (deterministic fallback)
// Level 3: Mark as pending_review
func VerdictPolicy(judgeResult *JudgeResult, stepResult agent.StepResult, confThreshold float64) FinalVerdict {
	v := FinalVerdict{
		Status:                judgeResult.Status,
		ExistenceConfidence:   judgeResult.ExistenceConfidence,
		CorrectnessConfidence: judgeResult.CorrectnessConfidence,
		Reasoning:             judgeResult.Reasoning,
		SelfCritique:          judgeResult.SelfCritique,
		CritiqueTriggered:     judgeResult.CritiqueTriggered,
		StepResult:            stepResult,
	}

	// Threshold filtering: downgrade low-confidence passes to uncertain.
	if judgeResult.Status == StatusPass && confThreshold > 0 {
		if judgeResult.CorrectnessConfidence < confThreshold {
			v.Status = StatusUncertain
			v.Reasoning = fmt.Sprintf("Degraded from pass: correctness confidence %.2f below threshold %.2f. %s",
				judgeResult.CorrectnessConfidence, confThreshold, judgeResult.Reasoning)
			v.DegradedLevel = 1
			return v
		}
	}

	// If not uncertain (and not downgraded), accept the judge result as-is.
	if judgeResult.Status != StatusUncertain {
		return v
	}

	// Level 2: Checker-only — deterministic fallback.
	// If the step passed at HTTP level (2xx) but judge is uncertain,
	// downgrade to "pass" with low confidence.
	if stepResult.Status == agent.StepPassed && stepResult.Result != nil {
		if httpRes, ok := stepResult.Result.(types.HTTPResult); ok && httpRes.StatusCode >= 200 && httpRes.StatusCode < 300 {
			v.Status = StatusPass
			v.CorrectnessConfidence = 0.5 // Low — passed HTTP but uncertain correctness
			v.Reasoning = "Degraded from uncertain: HTTP 2xx confirmed, correctness uncertain"
			v.DegradedLevel = 2
			return v
		}
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
