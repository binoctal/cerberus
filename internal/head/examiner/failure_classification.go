package examiner

import (
	"strings"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/binoctal/cerberus/internal/types"
)

// checkPassOrSkip determines if the test passed or was skipped
func checkPassOrSkip(status string) (bool, store.FailureReason) {
	if status == "pass" || status == "passed" || status == "skip" || status == "skipped" {
		return true, store.FailureReasonNone
	}
	return false, store.FailureReasonNone
}

// checkPolicyRejection checks if the error indicates policy rejection
func checkPolicyRejection(stepResult agent.StepResult) (bool, store.FailureReason) {
	if stepResult.Error != nil && strings.Contains(stepResult.Error.Error(), "policy") {
		if strings.Contains(stepResult.Error.Error(), "rejected") || strings.Contains(stepResult.Error.Error(), "denied") {
			return true, store.FailureReasonPolicyRejected
		}
	}
	return false, store.FailureReasonNone
}

// checkDependencyIssues checks for dependency/build/compile errors
func checkDependencyIssues(stepResult agent.StepResult) (bool, store.FailureReason) {
	if stepResult.Error != nil {
		errMsg := stepResult.Error.Error()
		if strings.Contains(errMsg, "build") || strings.Contains(errMsg, "compile") ||
			strings.Contains(errMsg, "dependency") || strings.Contains(errMsg, "missing") {
			return true, store.FailureReasonDependencyMissing
		}
	}
	return false, store.FailureReasonNone
}

// checkLLMQualityIssues checks for LLM parsing/quality errors in reasoning
func checkLLMQualityIssues(reasoning string) (bool, store.FailureReason) {
	if strings.Contains(reasoning, "steer failed") || strings.Contains(reasoning, "unmarshal") ||
		strings.Contains(reasoning, "JSON") || strings.Contains(reasoning, "parse") {
		return true, store.FailureReasonLLMQuality
	}
	return false, store.FailureReasonNone
}

// checkTimeout checks for timeout errors
func checkTimeout(stepResult agent.StepResult) (bool, store.FailureReason) {
	if stepResult.Error != nil && strings.Contains(stepResult.Error.Error(), "timeout") {
		return true, store.FailureReasonTimeout
	}
	return false, store.FailureReasonNone
}

// checkSystemError checks for panic/crash/fatal errors
func checkSystemError(stepResult agent.StepResult) (bool, store.FailureReason) {
	if stepResult.Error != nil && (strings.Contains(stepResult.Error.Error(), "panic") ||
		strings.Contains(stepResult.Error.Error(), "crash") || strings.Contains(stepResult.Error.Error(), "fatal")) {
		return true, store.FailureReasonSystemError
	}
	return false, store.FailureReasonNone
}

// checkUnreachable detects target-unreachable failures: the service was down,
// DNS failed, or the connection was refused. Such failures are environmental —
// a recalled strategy cannot help an unreachable target, so they must be
// excluded from effectiveness attribution (not penalized).
func checkUnreachable(stepResult agent.StepResult) (bool, store.FailureReason) {
	// Transport errors surfaced as a Go error (e.g. the escalated
	// "target unreachable" result, or a dial error propagated by an executor).
	if stepResult.Error != nil {
		msg := strings.ToLower(stepResult.Error.Error())
		for _, sig := range []string{
			"unreachable", "connection refused", "connection reset",
			"no such host", "dial tcp", "server unreachable",
		} {
			if strings.Contains(msg, sig) {
				return true, store.FailureReasonUnreachable
			}
		}
	}
	// Transport errors surfaced in the executor result (HTTP status 0 or a
	// connection signal in the summary). Shared with the agent react loop so a
	// case that hit an environmental failure on ANY attempt is recognized.
	if types.IsEnvironmentalFailure(stepResult.Result) {
		return true, store.FailureReasonUnreachable
	}
	return false, store.FailureReasonNone
}

// getDefaultFailureReason returns the default failure reason based on status
func getDefaultFailureReason(status string) store.FailureReason {
	if status == "fail" || status == "failed" {
		return store.FailureReasonAssertionFailed
	}
	return store.FailureReasonNone
}
