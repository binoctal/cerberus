package examiner

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Positive expectations that merely mention error/failure handling must NOT be
// classified as negative tests (which would need the LLM to judge). The keyword
// match was too broad.
func TestExpectsFailureNotTriggeredByHandlingClauses(t *testing.T) {
	for _, exp := range []string{
		"returns 200 and handles errors gracefully",
		"handles 500 errors without crashing",
	} {
		assert.Falsef(t, expectsFailure(exp), "positive expectation misclassified as failure: %q", exp)
	}
	// Genuine negative expectations (system should return a failure) still detected.
	for _, exp := range []string{
		"should return 404 for unknown user",
		"must reject unauthenticated requests with 401",
		"request fails when the token is expired",
		"invalid input is rejected with 422",
	} {
		assert.Truef(t, expectsFailure(exp), "negative expectation missed: %q", exp)
	}
}
