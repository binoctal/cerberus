package store

// FailureReason represents the root cause of a test failure.
// This helps distinguish between system bugs and environment/LLM issues.
type FailureReason string

const (
	// FailureReasonNone indicates no failure (test passed)
	FailureReasonNone FailureReason = ""

	// FailureReasonAssertionFailed indicates a real functional test failure
	// This is the only failure type that represents a genuine system bug
	FailureReasonAssertionFailed FailureReason = "assertion_failed"

	// FailureReasonLLMQuality indicates LLM output quality issues
	// Examples: JSON parsing failures, malformed responses, incomplete outputs
	// This is NOT a system bug - it's an LLM limitation
	FailureReasonLLMQuality FailureReason = "llm_quality"

	// FailureReasonPolicyRejected indicates security/policy rejection
	// Examples: sandbox denied, dangerous commands blocked
	// This is EXPECTED behavior, not a bug
	FailureReasonPolicyRejected FailureReason = "policy_rejected"

	// FailureReasonDependencyMissing indicates dependency/environment issues
	// Examples: build failures, missing libraries, configuration errors
	// This is an environment issue, not a system bug
	FailureReasonDependencyMissing FailureReason = "dependency_missing"

	// FailureReasonTimeout indicates test timeout
	// Could be either system slowness or timeout set too low
	FailureReasonTimeout FailureReason = "timeout"

	// FailureReasonSystemError indicates unexpected system errors
	// Examples: crashes, panics, internal errors
	// This IS a system bug that needs attention
	FailureReasonSystemError FailureReason = "system_error"
)

// DisplayName returns a human-readable name for the failure reason
func (r FailureReason) DisplayName() string {
	switch r {
	case FailureReasonNone:
		return "Passed"
	case FailureReasonAssertionFailed:
		return "Functional Failure"
	case FailureReasonLLMQuality:
		return "LLM Quality Issue"
	case FailureReasonPolicyRejected:
		return "Policy Rejected"
	case FailureReasonDependencyMissing:
		return "Dependency Issue"
	case FailureReasonTimeout:
		return "Timeout"
	case FailureReasonSystemError:
		return "System Error"
	default:
		return "Unknown"
	}
}

// IsSystemBug returns true if this failure reason indicates a genuine system bug
// (as opposed to LLM issues, policy rejections, or environment problems)
func (r FailureReason) IsSystemBug() bool {
	switch r {
	case FailureReasonAssertionFailed, FailureReasonSystemError:
		return true
	default:
		return false
	}
}

// IsEnvironmentIssue returns true if this failure is due to environment/setup
func (r FailureReason) IsEnvironmentIssue() bool {
	switch r {
	case FailureReasonDependencyMissing, FailureReasonTimeout:
		return true
	default:
		return false
	}
}

// IsExpectedBehavior returns true if this failure is expected/policy-driven
func (r FailureReason) IsExpectedBehavior() bool {
	return r == FailureReasonPolicyRejected
}

// IsLLMIssue returns true if this failure is due to LLM output quality
func (r FailureReason) IsLLMIssue() bool {
	return r == FailureReasonLLMQuality
}
