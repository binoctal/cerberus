package examiner

import (
	"context"
	"strings"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/store"
)

// ClassifyFailureReason determines the root cause of a failure based on the error and result.
func ClassifyFailureReason(status string, stepResult agent.StepResult, reasoning string) store.FailureReason {
	// Pass cases
	if status == "pass" || status == "passed" {
		return store.FailureReasonNone
	}

	// Skip cases
	if status == "skip" || status == "skipped" {
		return store.FailureReasonNone
	}

	// Check for policy rejection first
	if stepResult.Error != nil && strings.Contains(stepResult.Error.Error(), "policy") {
		if strings.Contains(stepResult.Error.Error(), "rejected") || strings.Contains(stepResult.Error.Error(), "denied") {
			return store.FailureReasonPolicyRejected
		}
	}

	// Check for dependency/build issues
	if stepResult.Error != nil {
		errMsg := stepResult.Error.Error()
		if strings.Contains(errMsg, "build") || strings.Contains(errMsg, "compile") ||
		   strings.Contains(errMsg, "dependency") || strings.Contains(errMsg, "missing") {
			return store.FailureReasonDependencyMissing
		}
	}

	// Check for LLM quality issues in reasoning
	if strings.Contains(reasoning, "steer failed") || strings.Contains(reasoning, "unmarshal") ||
	   strings.Contains(reasoning, "JSON") || strings.Contains(reasoning, "parse") {
		return store.FailureReasonLLMQuality
	}

	// Check for timeout
	if stepResult.Error != nil && strings.Contains(stepResult.Error.Error(), "timeout") {
		return store.FailureReasonTimeout
	}

	// Check for system errors (crashes, panics)
	if stepResult.Error != nil && (strings.Contains(stepResult.Error.Error(), "panic") ||
	   strings.Contains(stepResult.Error.Error(), "crash") || strings.Contains(stepResult.Error.Error(), "fatal")) {
		return store.FailureReasonSystemError
	}

	// Default failed verdicts to assertion failed (real functional failure)
	if status == "fail" || status == "failed" {
		return store.FailureReasonAssertionFailed
	}

	// Default uncertain cases
	return store.FailureReasonNone
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
