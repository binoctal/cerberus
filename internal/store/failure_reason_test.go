package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Table-driven classification of every FailureReason constant: display name
// plus all five predicate methods. These predicates drive reflexion/effectiveness
// consolidation (CountsAsStrategyEvidence in particular), so a misclassification
// silently corrupts strategy scoring.
func TestFailureReason_Classification(t *testing.T) {
	cases := []struct {
		r                                       FailureReason
		display                                 string
		systemBug, env, evidence, expected, llm bool
	}{
		{FailureReasonNone, "Passed", false, false, false, false, false},
		{FailureReasonAssertionFailed, "Functional Failure", true, false, true, false, false},
		{FailureReasonLLMQuality, "LLM Quality Issue", false, false, false, false, true},
		{FailureReasonPolicyRejected, "Policy Rejected", false, false, true, true, false},
		{FailureReasonDependencyMissing, "Dependency Issue", false, true, false, false, false},
		{FailureReasonTimeout, "Timeout", false, true, false, false, false},
		{FailureReasonSystemError, "System Error", true, false, false, false, false},
		{FailureReasonUnreachable, "Target Unreachable", false, true, false, false, false},
		{FailureReason("bogus"), "Unknown", false, false, false, false, false},
	}
	for _, c := range cases {
		assert.Equal(t, c.display, c.r.DisplayName(), "DisplayName(%q)", c.r)
		assert.Equal(t, c.systemBug, c.r.IsSystemBug(), "IsSystemBug(%q)", c.r)
		assert.Equal(t, c.env, c.r.IsEnvironmentIssue(), "IsEnvironmentIssue(%q)", c.r)
		assert.Equal(t, c.evidence, c.r.CountsAsStrategyEvidence(), "CountsAsStrategyEvidence(%q)", c.r)
		assert.Equal(t, c.expected, c.r.IsExpectedBehavior(), "IsExpectedBehavior(%q)", c.r)
		assert.Equal(t, c.llm, c.r.IsLLMIssue(), "IsLLMIssue(%q)", c.r)
	}
}
