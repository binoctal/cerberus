package examiner

import (
	"fmt"
	"strings"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/types"
)

// objectiveVerdict returns a deterministic verdict when the result has an
// unambiguous success signal, skipping the LLM entirely. It applies only to
// action types whose Success() maps directly to expectation satisfaction
// (process exit code, HTTP 2xx) and only for positive expectations — negative
// tests like "should return 404" still need the LLM to judge whether the
// observed failure is the expected one. Returns (nil, false) when the case
// must go to the LLM.
func objectiveVerdict(r agent.StepResult, expectation string) (*JudgeResult, bool) {
	if r.Result == nil || expectsFailure(expectation) {
		return nil, false
	}
	switch r.Result.(type) {
	case types.ProcessResult:
		// Success() is objective (exit 0). HTTP/file/code Success does not
		// reliably imply the expectation (e.g. body content) is met, so those
		// still go to the LLM.
	default:
		return nil, false
	}
	summary := r.Result.Summary()
	if r.Result.Success() {
		return &JudgeResult{
			Status:                StatusPass,
			ExistenceConfidence:   1.0,
			CorrectnessConfidence: 1.0,
			Reasoning:             fmt.Sprintf("objective: action succeeded — %s", summary),
		}, true
	}
	return &JudgeResult{
		Status:                StatusFail,
		ExistenceConfidence:   0.9,
		CorrectnessConfidence: 0.0,
		Reasoning:             fmt.Sprintf("objective: action failed — %s", summary),
	}, true
}

// expectsFailure reports whether the expectation asks for a failure/error
// outcome (a negative test). Such cases need the LLM to judge whether the
// observed failure is the expected one.
func expectsFailure(expectation string) bool {
	e := strings.ToLower(expectation)
	for _, kw := range []string{"fail", "error", "404", "401", "403", "4xx", "5xx", "reject", "denied", "invalid", "unauthorized"} {
		if strings.Contains(e, kw) {
			return true
		}
	}
	return false
}
