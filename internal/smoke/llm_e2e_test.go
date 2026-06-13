//go:build e2e

package smoke

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/binoctal/cerberus/internal/head/scout"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/sandbox"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/binoctal/cerberus/internal/types"
)

func getAPIKey(t *testing.T) string {
	t.Helper()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping E2E test")
	}
	return apiKey
}

// TestE2E_LLMComplete tests a real LLM Complete call.
func TestE2E_LLMComplete(t *testing.T) {
	apiKey := getAPIKey(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := llm.NewClient("claude-sonnet-4-6", apiKey)
	require.NoError(t, err)

	resp, err := client.Complete(ctx, llm.Request{
		Messages: []llm.Message{
			{Role: "user", Content: "Reply with exactly: {\"status\": \"ok\"}"},
		},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Content)
	assert.Equal(t, "end_turn", resp.StopReason)
	assert.Greater(t, resp.Usage.InputTokens, 0)
	assert.Greater(t, resp.Usage.OutputTokens, 0)
	t.Logf("Tokens: in=%d out=%d total=%d", resp.Usage.InputTokens, resp.Usage.OutputTokens, resp.Usage.TotalTokens)
}

// TestE2E_AIDriverDecide tests ai.Driver with a real LLM,
// verifying structured output parsing.
func TestE2E_AIDriverDecide(t *testing.T) {
	apiKey := getAPIKey(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := llm.NewClient("claude-sonnet-4-6", apiKey)
	require.NoError(t, err)

	budget := ai.NewTokenBudget(50000, 10000)
	driver := ai.NewDriver(client, budget)
	// Disable cache for deterministic E2E test.
	driver.SetCache(nil)

	var result struct {
		Endpoints []struct {
			Method string `json:"method"`
			Path   string `json:"path"`
		} `json:"endpoints"`
	}

	prompt := `Analyze this API specification and list the endpoints.
Respond with a JSON object containing an "endpoints" array.
Each endpoint should have "method" and "path" fields.

API: A todo application with:
- GET /todos - list all todos
- POST /todos - create a todo
- GET /todos/:id - get a specific todo
- DELETE /todos/:id - delete a todo

Output valid JSON only.`

	err = driver.Decide(ctx, prompt, &result)
	require.NoError(t, err)
	require.NotEmpty(t, result.Endpoints, "should detect at least one endpoint")
	t.Logf("Detected %d endpoints", len(result.Endpoints))
	for _, ep := range result.Endpoints {
		t.Logf("  %s %s", ep.Method, ep.Path)
	}

	// Verify budget was consumed.
	assert.Greater(t, budget.Remaining(), 0, "should have remaining budget")
	assert.Less(t, budget.Remaining(), 50000, "should have consumed some tokens")
}

// TestE2E_ScoutAnalyze tests Scout.Analyze with a real LLM,
// verifying it can infer API endpoints from a project config.
func TestE2E_ScoutAnalyze(t *testing.T) {
	apiKey := getAPIKey(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	client, err := llm.NewClient("claude-sonnet-4-6", apiKey)
	require.NoError(t, err)

	budget := ai.NewTokenBudget(100000, 20000)
	driver := ai.NewDriver(client, budget)
	driver.SetCache(nil)

	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../migrations"))

	logger := zap.NewNop()
	cfg := project.DefaultConfig()
	cfg.Project.Name = "e2e-test"
	cfg.Services = []project.Service{
		{Name: "api", URL: "http://localhost:8080", Health: "/health"},
	}

	scoutHead := scout.NewScout(driver, s, &cfg, logger)

	model, err := scoutHead.Analyze(ctx, scout.TargetInfo{
		URL:  "http://localhost:8080",
		Goal: "Test all REST API endpoints",
	})
	require.NoError(t, err)
	require.NotNil(t, model)
	t.Logf("Model info score: %.2f", model.InfoScore(false))
	t.Logf("Endpoints detected: %d", len(model.API.Endpoints))
}

// TestE2E_ExaminerJudge tests Examiner.Examine with a real LLM,
// verifying judge verdicts and reflection learning.
func TestE2E_ExaminerJudge(t *testing.T) {
	apiKey := getAPIKey(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client, err := llm.NewClient("claude-sonnet-4-6", apiKey)
	require.NoError(t, err)

	budget := ai.NewTokenBudget(200000, 50000)
	driver := ai.NewDriver(client, budget)
	driver.SetCache(nil)

	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../migrations"))

	// Seed a session so examiner has data to work with.
	sess, err := s.CreateSession(ctx, "run", "e2e examiner test", "e2e-test")
	require.NoError(t, err)

	// Build fake step results for the examiner.
	results := []agent.StepResult{
		{
			TestCase: &agent.TestCase{
				ID: "tc-001", Name: "health check", Target: "/healthz",
				Method: "GET", Expectation: "returns 200", Priority: 0.9,
			},
			Status:   agent.StepPassed,
			Attempts: 1,
			Evidence: []agent.Evidence{{Type: "http_response", Content: `HTTP 200 OK, body: {"status": "healthy"}`}},
		},
	}

	logger := zap.NewNop()
	examinerCfg := examiner.DefaultExaminerConfig()
	examinerHead := examiner.NewExaminer(driver, nil, s, examinerCfg, logger)

	verdicts, reflections, err := examinerHead.Examine(ctx, results, sess.ID, "e2e-test")
	require.NoError(t, err)
	require.NotEmpty(t, verdicts, "should produce at least one verdict")

	t.Logf("Verdicts: %d, Reflections: %d", len(verdicts), reflections)
	for _, v := range verdicts {
		t.Logf("  %s: status=%s confidence=%.2f", v.StepResult.TestCase.Target, v.Status, v.ExistenceConfidence)
	}

	assert.Greater(t, budget.Remaining(), 0, "should have remaining budget")
}

// TestE2E_CodeExecutor_GoAnalyze tests CodeExecutor analyzing
// cerberus's own codebase (no LLM needed, uses Go parser).
func TestE2E_CodeExecutor_GoAnalyze(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	logger := zap.NewNop()
	exec := agent.NewCodeExecutor(sandbox.NoOpSandbox{}, logger)
	action := types.CodeAnalyzeAction{
		TargetPath: "internal/ai",
		Language:   "go",
	}

	result := exec.Execute(ctx, action)
	assert.True(t, result.Success(), "GoAnalyze should succeed: %s", result.Summary())
	t.Logf("Analysis result: %s", result.Summary())
}
