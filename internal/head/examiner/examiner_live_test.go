//go:build live

package examiner_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/types"
)

// TestJudge_LiveGLM drives Judge.Judge through the real DecideWithTools path
// against a live LLM (loaded from .claude/settings.json via config.Load). The
// synthetic StepResult uses an HTTPResult with an ambiguous body so the case
// bypasses objectiveVerdict (only ProcessResult is objective) and reaches the
// migrated judge site. Validates that GLM emits a judge_result tool call whose
// assembled verdict has status ∈ {pass, fail, skip, uncertain} and a
// schema-valid confidence in [0, 1]. Build-tagged `live` so it never runs in
// `make test`. Run:
//
//	go test -tags live -run TestJudge_LiveGLM -v ./internal/head/examiner/
func TestJudge_LiveGLM(t *testing.T) {
	cfg := config.Load()
	if cfg.LLMAPIKey == "" {
		t.Skip("no LLM API key in settings.json env — live probe needs a real LLM")
	}

	client, err := llm.NewClientWithConfig(llm.ClientConfig{
		Model:      cfg.LLMModel,
		APIKey:     cfg.LLMAPIKey,
		BaseURL:    cfg.LLMBaseURL,
		Provider:   cfg.LLMProvider,
		AuthScheme: cfg.LLMAuthScheme,
	})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	driver := ai.NewDriver(client, ai.NewTokenBudget(60000, 10000))
	// nil critic driver: this gate targets the judge site, not Self-Refine.
	// If the verdict is uncertain, Judge falls back to the initial result.
	judge := examiner.NewJudge(driver, nil, examiner.DefaultExaminerConfig())

	// HTTPResult bypasses objectiveVerdict's fast path (only ProcessResult is
	// objective), forcing the case through DecideWithTools. Empty body paired
	// with a positive "non-empty list" expectation gives GLM an ambiguous
	// content case to actually judge rather than rubber-stamping.
	stepResult := agent.StepResult{
		TestCase: &agent.TestCase{
			ID:          "tc-live-1",
			Name:        "List users",
			Target:      "http://localhost:8080/api/v1/users",
			Expectation: "returns a non-empty user list",
		},
		Status:   agent.StepPassed,
		Attempts: 1,
		Result: types.HTTPResult{
			OK:         true,
			StatusCode: 200,
			Body:       `{}`,
		},
		Action: types.HTTPAction{Method: "GET", URL: "http://localhost:8080/api/v1/users"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	result, err := judge.Judge(ctx, stepResult)
	if err != nil {
		t.Fatalf("judge: %v", err)
	}

	t.Logf("=== verdict ===")
	t.Logf("status: %s", result.Status)
	t.Logf("existence_confidence: %.2f", result.ExistenceConfidence)
	t.Logf("correctness_confidence: %.2f", result.CorrectnessConfidence)
	t.Logf("reasoning: %s", result.Reasoning)
	t.Logf("critique_triggered: %v", result.CritiqueTriggered)

	switch result.Status {
	case examiner.StatusPass, examiner.StatusFail, examiner.StatusSkip, examiner.StatusUncertain:
		// valid — GLM emitted a judge_result tool call with a schema-valid status.
	default:
		t.Fatalf("unexpected verdict status %q (must be pass/fail/skip/uncertain)", result.Status)
	}

	assert.GreaterOrEqual(t, result.ExistenceConfidence, 0.0, "existence_confidence must be non-negative")
	assert.LessOrEqual(t, result.ExistenceConfidence, 1.0, "existence_confidence must be ≤ 1.0")
	assert.GreaterOrEqual(t, result.CorrectnessConfidence, 0.0, "correctness_confidence must be non-negative")
	assert.LessOrEqual(t, result.CorrectnessConfidence, 1.0, "correctness_confidence must be ≤ 1.0")
	require.NotEmpty(t, result.Reasoning, "judge_result must include reasoning")
}
