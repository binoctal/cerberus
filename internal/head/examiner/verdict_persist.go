package examiner

import (
	"context"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/store"
)

// ClassifyFailureReason determines the root cause of a failure based on the error and result.
func ClassifyFailureReason(status string, stepResult agent.StepResult, reasoning string) store.FailureReason {
	// Phase 1: Check pass/skip cases
	if isPass, reason := checkPassOrSkip(status); isPass {
		return reason
	}

	// Phase 2: Check for policy rejection
	if isRejected, reason := checkPolicyRejection(stepResult); isRejected {
		return reason
	}

	// Phase 3: Check for dependency/build issues
	if hasDepIssue, reason := checkDependencyIssues(stepResult); hasDepIssue {
		return reason
	}

	// Phase 4: Check for LLM quality issues in reasoning
	if hasLLMIssue, reason := checkLLMQualityIssues(reasoning); hasLLMIssue {
		return reason
	}

	// Phase 5: Check for timeout
	if isTimeout, reason := checkTimeout(stepResult); isTimeout {
		return reason
	}

	// Phase 6: Check for system errors
	if isSystemError, reason := checkSystemError(stepResult); isSystemError {
		return reason
	}

	// Phase 7: Default failure classification
	return getDefaultFailureReason(status)
}

// PersistFinalVerdicts converts and stores FinalVerdict results to persistent Verdict records.
// Returns count of verdicts stored and any error.
func PersistFinalVerdicts(ctx context.Context, s *store.Store, logger *zap.Logger, sessionID string, verdicts []FinalVerdict) (int, error) {
	count := 0
	for _, v := range verdicts {
		// Classify failure reason based on the step result and reasoning
		failureReason := ClassifyFailureReason(string(v.Status), v.StepResult, v.Reasoning)

		// Convert status to string
		status := string(v.Status)
		if status == "" {
			status = "uncertain"
		}

		// Extract target from test case
		target := v.StepResult.TestCase.Target
		if target == "" {
			target = "unknown"
		}

		// Create the verdict record
		_, err := s.CreateVerdict(
			ctx,
			sessionID,
			v.StepResult.TraceID,
			target,
			status,
			v.CorrectnessConfidence,
			"judge", // Use "judge" to satisfy database constraint
			v.Reasoning,
			nil, // suggestions
			failureReason,
		)
		if err != nil {
			logger.Warn("failed to persist verdict",
				zap.String("target", target),
				zap.Error(err))
			return count, err
		}

		logger.Debug("persisted verdict",
			zap.String("target", target),
			zap.String("status", status),
			zap.String("failure_reason", string(failureReason)))

		count++
	}
	return count, nil
}
