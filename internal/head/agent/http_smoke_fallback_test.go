package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/binoctal/cerberus/internal/types"
)

// smokeStubExec is a stub TypedExecutor that returns a deterministic HTTPResult
// for the smoke fallback case. It holds a fixed status code and reports OK iff
// the status is a 2xx/3xx response. This lets the integration test drive the
// smoke judgment through the real executeStep -> tryRuleEngine path without
// standing up an httptest.Server (the URL the rule engine builds is irrelevant
// to the smoke verdict, which depends only on the returned HTTPResult.StatusCode).
type smokeStubExec struct {
	status int
}

func (e *smokeStubExec) Execute(ctx context.Context, a types.TypedAction) types.ExecutorResult {
	return types.HTTPResult{
		OK:         e.status >= 200 && e.status < 400,
		StatusCode: e.status,
	}
}

// TestExecutePlan_HTTPSmokeRecoversOnNonEnvironmentalFailure proves the lazy
// GET-smoke fallback shipped in Tasks 1-3 is judged deterministically through
// the real executeStep -> tryRuleEngine path: a 2xx/3xx/4xx response yields
// StepPassed (reachable and non-5xx) and a 5xx yields StepFailed. The smoke
// case never enters the ReAct LLM loop — tryRuleEngine returns a verdict
// directly because FallbackFor != "".
func TestExecutePlan_HTTPSmokeRecoversOnNonEnvironmentalFailure(t *testing.T) {
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, store.RunMigrations(context.Background(), s.DB(), "../../../migrations"))

	driver := ai.NewDriver(llm.NewMockClient(nil), ai.NewTokenBudget(200000, 10000))
	embedder := embed.NewTrigramProvider(embed.DefaultDimension)

	smoke := TestCase{
		ID:          "smoke-api-users",
		Target:      "/users",
		Method:      "GET",
		Service:     "api",
		Expectation: "reachable and non-5xx",
		FallbackFor: "tc-primary",
		Priority:    -1,
	}

	runSmoke := func(status int) StepStatus {
		loop := NewReActLoopWithConfig(ReActLoopConfig{
			Driver:   driver,
			Store:    s,
			Engine:   NewRuleEngine(nil, nil, "."),
			Executor: &smokeStubExec{status: status},
			Config:   DefaultReActConfig(),
			Logger:   zap.NewNop(),
			Embedder: embedder,
		})
		sess, err := s.CreateSession(context.Background(), "run", "test", "")
		require.NoError(t, err)
		return loop.executeStep(context.Background(), &smoke, sess.ID).Status
	}

	// 2xx -> reachable and non-5xx -> StepPassed.
	assert.Equal(t, StepPassed, runSmoke(200),
		"smoke GET on a 200 endpoint -> StepPassed (reachable, non-5xx)")
	// 5xx -> server erroring -> StepFailed.
	assert.Equal(t, StepFailed, runSmoke(500),
		"smoke GET on a 500 endpoint -> StepFailed (5xx)")
}
