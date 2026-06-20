package examiner_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/binoctal/cerberus/internal/types"
)

func makeStepResult(id, name, target, expectation string, status agent.StepStatus, statusCode int, body string) agent.StepResult {
	return agent.StepResult{
		TestCase: &agent.TestCase{ID: id, Name: name, Target: target, Expectation: expectation},
		Status:   status,
		Attempts: 1,
		Result: types.HTTPResult{
			OK:         status == agent.StepPassed,
			StatusCode: statusCode,
			Body:       body,
		},
		Action: types.HTTPAction{Method: "GET", URL: target},
	}
}

func TestLearn_NormalizesAndEmbedsCondition(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../../migrations"))

	// LLM returns one reflection whose condition is un-normalized prose.
	driver := ai.NewDriver(llm.NewMockClient(map[string]string{
		"default": `[{"diagnosis":"d","condition_pattern":"POST /x/* Returned 401. ","strategy":"retry auth","category":"auth","type":"failure"}]`,
	}), ai.NewTokenBudget(100000, 10000))
	l := examiner.NewLearner(driver, s, zap.NewNop(), embed.NewTrigramProvider(embed.DefaultDimension))

	// Create a step result for the learning context
	results := []agent.StepResult{
		makeStepResult("tc-1", "Test POST /x", "POST /x/123", "returns 200", agent.StepFailed, 401, ""),
	}

	n, err := l.Learn(ctx, examiner.LearnInput{SessionID: "s1", Project: "p", Results: results})
	require.NoError(t, err)
	require.Equal(t, 1, n)

	// Stored condition is normalized; embedding is populated with the trigram model.
	mem, err := s.GetProceduralByExactKey(ctx, "p", "post /x/* returned 401", "retry auth")
	require.NoError(t, err)
	require.NotEmpty(t, mem.Embedding, "condition must be embedded")
	require.Equal(t, embed.NewTrigramProvider(embed.DefaultDimension).ModelName(), mem.EmbeddingModel)
}
