package examiner

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/binoctal/cerberus/internal/types"
)

// TestClassifyFailureReason_Unreachable verifies that target-unreachable
// failures (service down, connection refused, DNS) are classified as
// environmental, NOT as assertion failures. This is what lets effectiveness
// consolidation avoid penalizing recalled strategies for an unreachable target.
func TestClassifyFailureReason_Unreachable(t *testing.T) {
	tests := []struct {
		name   string
		errMsg string
		want   store.FailureReason
	}{
		{"target unreachable", "target unreachable: /api/auth/login", store.FailureReasonUnreachable},
		{"connection refused", "dial tcp: connect: connection refused", store.FailureReasonUnreachable},
		{"no such host", "dial tcp: lookup svc: no such host", store.FailureReasonUnreachable},
		{"connection reset", "read tcp: connection reset by peer", store.FailureReasonUnreachable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := agent.StepResult{Error: errors.New(tt.errMsg)}
			got := ClassifyFailureReason("fail", sr, "")
			assert.Equal(t, tt.want, got)
			assert.True(t, got.IsEnvironmentIssue(), "unreachable must be an environment issue")
			assert.False(t, got.CountsAsStrategyEvidence(), "unreachable must not count as strategy evidence")
		})
	}
}

// TestClassifyFailureReason_AssertionDefault verifies a fail with no
// transport/system/timeout signal still defaults to AssertionFailed (unchanged).
func TestClassifyFailureReason_AssertionDefault(t *testing.T) {
	sr := agent.StepResult{} // no error, no special signal
	got := ClassifyFailureReason("fail", sr, "")
	assert.Equal(t, store.FailureReasonAssertionFailed, got)
	assert.True(t, got.CountsAsStrategyEvidence(), "assertion failure must count as strategy evidence")
}

// TestClassifyFailureReason_HTTPZeroResult verifies the common connection-refused
// case: the HTTP executor returns StatusCode 0 (no response), which surfaces in
// the result summary as "HTTP 0 ...". This must classify as unreachable, not
// assertion-failed, so strategies are not penalized for a down service.
func TestClassifyFailureReason_HTTPZeroResult(t *testing.T) {
	sr := agent.StepResult{
		Result: types.HTTPResult{StatusCode: 0, URL: "http://localhost:3000/auth/login"},
	}
	got := ClassifyFailureReason("fail", sr, "")
	assert.Equal(t, store.FailureReasonUnreachable, got)
	assert.True(t, got.IsEnvironmentIssue())
}
