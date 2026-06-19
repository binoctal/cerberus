package examiner

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/head/agent"
)

// TestBuildEvidenceContext_FlagsFrameworkError verifies that a StepResult
// carrying a framework error (the test itself failed to run — e.g. a steer
// unmarshal failure) is explicitly flagged in the evidence, so the judge LLM
// does not misread it as the system-under-test returning an expected error.
func TestBuildEvidenceContext_FlagsFrameworkError(t *testing.T) {
	j := NewJudge(nil, nil, ExaminerConfig{})
	r := agent.StepResult{
		TestCase: &agent.TestCase{ID: "tc", Name: "n", Target: "/t", Expectation: "e"},
		Status:   agent.StepFailed,
		Error:    errors.New("unmarshal steer action: unexpected end of JSON input"),
	}
	ctx := j.buildEvidenceContext(r)

	assert.Contains(t, ctx, "FRAMEWORK",
		"framework errors must be flagged so the judge does not treat them as SUT responses")
}

// TestPromptJudgeSystem_WarnsAboutFrameworkErrors verifies the judge system
// prompt instructs the LLM that framework/steer errors are failures, not
// negative-test passes.
func TestPromptJudgeSystem_WarnsAboutFrameworkErrors(t *testing.T) {
	assert.Contains(t, promptJudgeSystem, "framework")
	assert.Contains(t, promptJudgeSystem, "Step Error")
}
