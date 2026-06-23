package examiner

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/llm"
)

// countingClient wraps an llm.Client and counts Complete/CompleteWithVision
// calls, so a test can assert how many times the critic was actually invoked.
type countingClient struct {
	inner llm.Client
	n     *atomic.Int64
}

func (c *countingClient) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	c.n.Add(1)
	return c.inner.Complete(ctx, req)
}

func (c *countingClient) CompleteWithVision(ctx context.Context, prompt string, images [][]byte) (*llm.Response, error) {
	c.n.Add(1)
	return c.inner.CompleteWithVision(ctx, prompt, images)
}

func (c *countingClient) Stream(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
	return c.inner.Stream(ctx, req)
}

// TestJudgeCritiqueBudgetHeldUnderConcurrency: with MaxCritiques=2 and 20
// concurrent low-confidence verdicts, the critic must be invoked at most twice.
// Before the fix, critiqueUsed (a plain int) was read in Judge and incremented
// in critique without synchronization, so the budget was exceeded under load.
func TestJudgeCritiqueBudgetHeldUnderConcurrency(t *testing.T) {
	judgeResult := JudgeResult{
		Status:                StatusUncertain,
		ExistenceConfidence:   0.8,
		CorrectnessConfidence: 0.5,
		Reasoning:             "uncertain",
	}
	judgeJSON, _ := json.Marshal(judgeResult)
	critiqueResult := CritiqueResult{
		IssuesFound:         true,
		SuggestedStatus:     StatusPass,
		SuggestedConfidence: 0.9,
		Critique:            "c",
	}
	critiqueJSON, _ := json.Marshal(critiqueResult)

	judgeDriver := ai.NewDriver(
		llm.NewMockClient(map[string]string{"default": string(judgeJSON)}),
		ai.NewTokenBudget(200000, 10000))
	var criticCalls atomic.Int64
	criticDriver := ai.NewDriver(
		&countingClient{inner: llm.NewMockClient(map[string]string{"default": string(critiqueJSON)}), n: &criticCalls},
		ai.NewTokenBudget(200000, 10000))

	judge := NewJudge(judgeDriver, criticDriver, ExaminerConfig{MaxCritiques: 2, ConfThreshold: 0.9})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = judge.Judge(context.Background(),
				makeStepResult("tc", "t", "/api", "works", agent.StepPassed, 200, "ok"))
		}()
	}
	wg.Wait()

	if got := criticCalls.Load(); got > 2 {
		t.Fatalf("critique budget exceeded under concurrency: %d critic calls, MaxCritiques=2", got)
	}
}
