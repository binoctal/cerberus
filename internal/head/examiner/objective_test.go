package examiner

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/types"
)

func stepWith(result types.ExecutorResult, expectation string) agent.StepResult {
	return agent.StepResult{
		TestCase: &agent.TestCase{ID: "tc", Target: "/t", Expectation: expectation},
		Result:   result,
	}
}

func TestObjectiveVerdict_ProcessSuccess(t *testing.T) {
	r := stepWith(types.ProcessResult{OK: true, ExitCode: 0}, "all tests pass")
	v, ok := objectiveVerdict(r, "all tests pass")
	require.True(t, ok)
	assert.Equal(t, StatusPass, v.Status)
}

func TestObjectiveVerdict_ProcessFailure(t *testing.T) {
	r := stepWith(types.ProcessResult{OK: false, ExitCode: 1}, "build succeeds")
	v, ok := objectiveVerdict(r, "build succeeds")
	require.True(t, ok)
	assert.Equal(t, StatusFail, v.Status)
}

func TestObjectiveVerdict_HTTPGoesToLLM(t *testing.T) {
	// HTTP success (2xx) does not reliably imply the expectation (e.g. body
	// content) is met, so HTTP results still go to the LLM.
	r := stepWith(types.HTTPResult{OK: true, StatusCode: 200}, "returns 200")
	_, ok := objectiveVerdict(r, "returns 200")
	assert.False(t, ok, "HTTP results should go to the LLM")
}

func TestObjectiveVerdict_NegativeExpectationGoesToLLM(t *testing.T) {
	r := stepWith(types.HTTPResult{OK: false, StatusCode: 404}, "returns 404 not found")
	_, ok := objectiveVerdict(r, "returns 404 not found")
	assert.False(t, ok, "negative test (expects 404) must go to LLM")
}

func TestObjectiveVerdict_FileResultGoesToLLM(t *testing.T) {
	r := stepWith(types.FileResult{OK: true}, "file contains retry logic")
	_, ok := objectiveVerdict(r, "file contains retry logic")
	assert.False(t, ok, "file content checks need the LLM")
}

func TestObjectiveVerdict_NilResultGoesToLLM(t *testing.T) {
	r := stepWith(nil, "works")
	_, ok := objectiveVerdict(r, "works")
	assert.False(t, ok)
}
