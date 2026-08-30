package examiner

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/llm"
)

// TestRejudgeRateLimited_UpgradesFallback: once the provider window reset has
// landed (reset in the past → no wait), a quota-fallback verdict is re-judged
// and replaced by the LLM verdict.
func TestRejudgeRateLimited_UpgradesFallback(t *testing.T) {
	mockClient := llm.NewMockClient(nil)
	mockClient.SetToolResponse("default", []llm.ToolCall{
		judgeResultCall(StatusPass, 0.95, 0.95, "Response matches expectation"),
	})
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))
	e := NewExaminer(driver, nil, setupExaminerStore(t), DefaultExaminerConfig(), zaptest.NewLogger(t))

	results := []agent.StepResult{makeStepResult("tc-1", "x", "/api/x", "returns 200", agent.StepPassed, 200, "")}
	verdicts := []FinalVerdict{fallbackVerdict(results[0], DefaultExaminerConfig().ConfThreshold, "Judge failed, using execution status")}
	assert.Equal(t, StatusPass, verdicts[0].Status) // fallback copies step status

	e.rejudgeRateLimited(context.Background(), results, verdicts, []bool{true}, time.Now().Add(-time.Minute))

	require.NotNil(t, verdicts[0])
	assert.Equal(t, "Response matches expectation", verdicts[0].Reasoning, "judge reasoning replaces the fallback text")
	assert.Equal(t, 0, verdicts[0].DegradedLevel)
}

// TestRejudgeRateLimited_UnknownResetKeepsFallback: with no parseable reset
// time the pass must not block the run — fallback verdicts stand untouched.
func TestRejudgeRateLimited_UnknownResetKeepsFallback(t *testing.T) {
	mockClient := llm.NewMockClient(nil)
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))
	e := NewExaminer(driver, nil, setupExaminerStore(t), DefaultExaminerConfig(), zaptest.NewLogger(t))

	results := []agent.StepResult{makeStepResult("tc-1", "x", "/api/x", "returns 200", agent.StepPassed, 200, "")}
	verdicts := []FinalVerdict{fallbackVerdict(results[0], DefaultExaminerConfig().ConfThreshold, "Judge failed, using execution status")}

	e.rejudgeRateLimited(context.Background(), results, verdicts, []bool{true}, time.Time{})

	assert.Equal(t, "Judge failed, using execution status", verdicts[0].Reasoning, "no reset time → no re-judge")
}

// TestRejudgeRateLimited_FarResetKeepsFallback: a reset beyond
// RateLimitRewaitMax is not worth blocking on — fallback verdicts stand.
func TestRejudgeRateLimited_FarResetKeepsFallback(t *testing.T) {
	mockClient := llm.NewMockClient(nil)
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))
	cfg := DefaultExaminerConfig()
	e := NewExaminer(driver, nil, setupExaminerStore(t), cfg, zaptest.NewLogger(t))

	results := []agent.StepResult{makeStepResult("tc-1", "x", "/api/x", "returns 200", agent.StepPassed, 200, "")}
	verdicts := []FinalVerdict{fallbackVerdict(results[0], cfg.ConfThreshold, "Judge failed, using execution status")}

	e.rejudgeRateLimited(context.Background(), results, verdicts, []bool{true}, time.Now().Add(cfg.RateLimitRewaitMax+time.Hour))

	assert.Equal(t, "Judge failed, using execution status", verdicts[0].Reasoning, "reset beyond cap → no re-judge")
}
