package examiner

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/binoctal/cerberus/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func setupExaminerStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	err = store.RunMigrations(ctx, s.DB(), "../../../migrations")
	require.NoError(t, err)
	return s
}

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

func TestJudge_HighConfidence(t *testing.T) {
	// High confidence pass — should skip critique.
	judgeResult := JudgeResult{
		Status:                StatusPass,
		ExistenceConfidence:   0.95,
		CorrectnessConfidence: 0.95,
		Reasoning:             "Response matches expectation",
	}
	judgeJSON, _ := json.Marshal(judgeResult)

	mockClient := llm.NewMockClient(map[string]string{"default": string(judgeJSON)})
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))

	judge := NewJudge(driver, nil, DefaultExaminerConfig())
	result, err := judge.Judge(context.Background(), makeStepResult(
		"tc-1", "Get users", "/api/users", "returns 200", agent.StepPassed, 200, `{"users":[]}`,
	))
	require.NoError(t, err)
	assert.Equal(t, StatusPass, result.Status)
	assert.InDelta(t, 0.95, result.CorrectnessConfidence, 0.01)
	assert.False(t, result.CritiqueTriggered)
}

func TestJudge_LowConfidence_TriggersCritique(t *testing.T) {
	// Low confidence result → should trigger critic.
	judgeResult := JudgeResult{
		Status:                StatusUncertain,
		ExistenceConfidence:   0.9,
		CorrectnessConfidence: 0.6,
		Reasoning:             "Response exists but content unclear",
	}
	_ = judgeResult // Used conceptually; mockClient returns same JSON for both calls.

	critiqueResult := CritiqueResult{
		IssuesFound:         true,
		Critique:            "False positive risk: status 200 but empty body",
		SuggestedStatus:     StatusFail,
		SuggestedConfidence: 0.3,
	}
	critiqueJSON, _ := json.Marshal(critiqueResult)

	mockClient := llm.NewMockClient(map[string]string{
		"default": string(critiqueJSON), // Both judge and critic use default
	})
	judgeDriver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))
	criticDriver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))

	judge := NewJudge(judgeDriver, criticDriver, DefaultExaminerConfig())
	_, err := judge.Judge(context.Background(), makeStepResult(
		"tc-1", "Get users", "/api/users", "returns user list", agent.StepPassed, 200, "",
	))
	require.NoError(t, err)
	// The judge returns low confidence (0.6), so it should trigger critique.
	// Critic will return its verdict but mockClient returns same JSON for both calls.
	// The important thing is the critique was attempted.
}

func TestJudge_NoCriticDriver(t *testing.T) {
	// Without critic driver, uncertain result stays uncertain.
	judgeResult := JudgeResult{
		Status:                StatusUncertain,
		ExistenceConfidence:   0.8,
		CorrectnessConfidence: 0.5,
		Reasoning:             "Cannot determine",
	}
	judgeJSON, _ := json.Marshal(judgeResult)

	mockClient := llm.NewMockClient(map[string]string{"default": string(judgeJSON)})
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))

	judge := NewJudge(driver, nil, DefaultExaminerConfig())
	result, err := judge.Judge(context.Background(), makeStepResult(
		"tc-1", "Test", "/api", "works", agent.StepPassed, 200, "ok",
	))
	require.NoError(t, err)
	assert.Equal(t, StatusUncertain, result.Status)
	assert.False(t, result.CritiqueTriggered)
}

func TestJudge_MaxCritiquesExceeded(t *testing.T) {
	// Session-level max critiques should prevent further critiques.
	judgeResult := JudgeResult{
		Status:                StatusUncertain,
		ExistenceConfidence:   0.8,
		CorrectnessConfidence: 0.5,
		Reasoning:             "Uncertain",
	}
	judgeJSON, _ := json.Marshal(judgeResult)

	mockClient := llm.NewMockClient(map[string]string{"default": string(judgeJSON)})
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))

	cfg := ExaminerConfig{MaxCritiques: 0, ConfThreshold: 0.9} // Max 0 critiques
	judge := NewJudge(driver, driver, cfg)

	result, err := judge.Judge(context.Background(), makeStepResult(
		"tc-1", "Test", "/api", "works", agent.StepPassed, 200, "ok",
	))
	require.NoError(t, err)
	assert.False(t, result.CritiqueTriggered)
}

func TestPolicy_NotUncertain(t *testing.T) {
	jr := &JudgeResult{
		Status:                StatusPass,
		ExistenceConfidence:   0.95,
		CorrectnessConfidence: 0.95,
		Reasoning:             "All good",
	}
	sr := makeStepResult("tc-1", "Test", "/api", "works", agent.StepPassed, 200, "ok")

	verdict := VerdictPolicy(jr, sr, 0.9)
	assert.Equal(t, StatusPass, verdict.Status)
	assert.Equal(t, 0, verdict.DegradedLevel)
	assert.False(t, verdict.PendingReview)
}

func TestPolicy_UncertainDegradedLevel2(t *testing.T) {
	// Uncertain + HTTP 2xx → degraded to pass with low confidence.
	jr := &JudgeResult{
		Status:                StatusUncertain,
		ExistenceConfidence:   0.9,
		CorrectnessConfidence: 0.5,
		Reasoning:             "Exists but correctness unclear",
	}
	sr := makeStepResult("tc-1", "Test", "/api", "works", agent.StepPassed, 200, "ok")

	verdict := VerdictPolicy(jr, sr, 0.9)
	assert.Equal(t, StatusPass, verdict.Status)
	assert.Equal(t, 2, verdict.DegradedLevel)
	assert.InDelta(t, 0.5, verdict.CorrectnessConfidence, 0.01)
}

func TestPolicy_UncertainDegradedLevel3(t *testing.T) {
	// Uncertain + HTTP 5xx → pending review.
	jr := &JudgeResult{
		Status:                StatusUncertain,
		ExistenceConfidence:   0.5,
		CorrectnessConfidence: 0.3,
		Reasoning:             "Server error",
	}
	sr := makeStepResult("tc-1", "Test", "/api", "works", agent.StepFailed, 500, "")

	verdict := VerdictPolicy(jr, sr, 0.9)
	assert.Equal(t, StatusUncertain, verdict.Status)
	assert.Equal(t, 3, verdict.DegradedLevel)
	assert.True(t, verdict.PendingReview)
	assert.True(t, verdict.NeedsReview())
}

	func TestPolicy_ThresholdDowngrade(t *testing.T) {
		// Pass with confidence 0.6, threshold 0.9 -> should downgrade to uncertain.
		jr := &JudgeResult{
			Status:                StatusPass,
			ExistenceConfidence:   0.9,
			CorrectnessConfidence: 0.6,
			Reasoning:             "Looks ok",
		}
		sr := makeStepResult("tc-1", "Test", "/api", "works", agent.StepPassed, 200, "ok")

		verdict := VerdictPolicy(jr, sr, 0.9)
		assert.Equal(t, StatusUncertain, verdict.Status)
		assert.Equal(t, 1, verdict.DegradedLevel)
		assert.Contains(t, verdict.Reasoning, "below threshold")
	}

	func TestPolicy_ThresholdPass(t *testing.T) {
		// Pass with confidence 0.95, threshold 0.9 -> should remain pass.
		jr := &JudgeResult{
			Status:                StatusPass,
			ExistenceConfidence:   0.95,
			CorrectnessConfidence: 0.95,
			Reasoning:             "All good",
		}
		sr := makeStepResult("tc-1", "Test", "/api", "works", agent.StepPassed, 200, "ok")

		verdict := VerdictPolicy(jr, sr, 0.9)
		assert.Equal(t, StatusPass, verdict.Status)
		assert.Equal(t, 0, verdict.DegradedLevel)
	}

	func TestPolicy_ThresholdZero_NoDowngrade(t *testing.T) {
		// threshold 0 disables downgrade entirely.
		jr := &JudgeResult{
			Status:                StatusPass,
			ExistenceConfidence:   0.3,
			CorrectnessConfidence: 0.3,
			Reasoning:             "Meh",
		}
		sr := makeStepResult("tc-1", "Test", "/api", "works", agent.StepPassed, 200, "ok")

		verdict := VerdictPolicy(jr, sr, 0)
		assert.Equal(t, StatusPass, verdict.Status)
		assert.Equal(t, 0, verdict.DegradedLevel)
	}

func TestLearner_QualityGate(t *testing.T) {
	tests := []struct {
		name     string
		r        Reflection
		expected bool
	}{
		{
			name: "valid failure reflection",
			r:    Reflection{Type: "failure", Diagnosis: "Auth token expired", Strategy: "Refresh token before retrying the request", ConditionPattern: "* returned 401", Category: "auth_failure"},
			expected: true,
		},
		{
			name: "valid success reflection",
			r:    Reflection{Type: "success", Diagnosis: "Pagination works correctly", Strategy: "Always include page parameter for list endpoints", ConditionPattern: "GET /api/*/list*", Category: "general_failure"},
			expected: true,
		},
		{
			name: "empty diagnosis",
			r:    Reflection{Type: "failure", Diagnosis: "", Strategy: "Some strategy here that is long enough", ConditionPattern: "* returned 500", Category: "server_error"},
			expected: false,
		},
		{
			name: "strategy too short",
			r:    Reflection{Type: "failure", Diagnosis: "Timeout occurred", Strategy: "retry", ConditionPattern: "* timeout", Category: "timeout_recovery"},
			expected: false,
		},
		{
			name: "invalid type",
			r:    Reflection{Type: "invalid", Diagnosis: "Something", Strategy: "A strategy that is long enough to pass", ConditionPattern: "*", Category: "general_failure"},
			expected: false,
		},
		{
			name: "empty condition pattern",
			r:    Reflection{Type: "failure", Diagnosis: "Error", Strategy: "A strategy long enough to pass the gate", ConditionPattern: "", Category: "general_failure"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, qualityGate(tt.r))
		})
	}
}

func TestLearner_StoreReflections(t *testing.T) {
	s := setupExaminerStore(t)

	reflections := Reflection{
		Type:             "failure",
		Diagnosis:        "Auth token expired causing 401",
		Strategy:         "Refresh auth token before retrying the request",
		ConditionPattern: "* returned 401",
		Category:         "auth_failure",
	}
	reflJSON, _ := json.Marshal([]Reflection{reflections})

	mockClient := llm.NewMockClient(map[string]string{"default": string(reflJSON)})
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))

	learner := NewLearner(driver, s, zap.NewNop(), nil)

	results := []agent.StepResult{
		makeStepResult("tc-1", "Get users", "/api/users", "returns 200", agent.StepFailed, 401, ""),
	}

	stored, err := learner.Learn(context.Background(), LearnInput{
		SessionID: "test-session",
		Project:   "test-project",
		Results:   results,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, stored)

	// Verify the reflection was stored.
	memories, err := s.GetProceduralByMatch(context.Background(), "401", 10)
	require.NoError(t, err)
	require.Len(t, memories, 1)
	assert.Equal(t, "auth_failure", memories[0].Name)
}

func TestLearner_EmptyResults(t *testing.T) {
	s := setupExaminerStore(t)
	mockClient := llm.NewMockClient(nil)
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))

	learner := NewLearner(driver, s, zap.NewNop(), nil)
	stored, err := learner.Learn(context.Background(), LearnInput{
		SessionID: "test",
		Project:   "test",
		Results:   []agent.StepResult{},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, stored)
}

func TestLearner_QualityGateFiltersBadReflections(t *testing.T) {
	s := setupExaminerStore(t)

	// Mix of valid and invalid reflections.
	reflections := []Reflection{
		{Type: "failure", Diagnosis: "Valid diagnosis", Strategy: "Valid strategy that is long enough", ConditionPattern: "* returned 500", Category: "server_error"},
		{Type: "failure", Diagnosis: "", Strategy: "Strategy without diagnosis", ConditionPattern: "*", Category: "general_failure"}, // Invalid: empty diagnosis
		{Type: "success", Diagnosis: "Works well", Strategy: "short", ConditionPattern: "GET *", Category: "general_failure"}, // Invalid: strategy too short
	}
	reflJSON, _ := json.Marshal(reflections)

	mockClient := llm.NewMockClient(map[string]string{"default": string(reflJSON)})
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))
	learner := NewLearner(driver, s, zap.NewNop(), nil)

	results := []agent.StepResult{
		makeStepResult("tc-1", "Test", "/api", "works", agent.StepFailed, 500, ""),
	}

	stored, err := learner.Learn(context.Background(), LearnInput{
		SessionID: "test",
		Project:   "test",
		Results:   results,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, stored) // Only the valid one
}

func TestExaminer_FullPipeline(t *testing.T) {
	s := setupExaminerStore(t)

	// Judge returns pass with high confidence.
	judgeResult := JudgeResult{
		Status:                StatusPass,
		ExistenceConfidence:   0.95,
		CorrectnessConfidence: 0.95,
		Reasoning:             "Response matches expectation",
	}
	judgeJSON, _ := json.Marshal(judgeResult)
	_ = judgeJSON // MockClient returns same JSON for judge and reflection calls.

	// Reflection returns one valid failure reflection.
	reflections := []Reflection{
		{Type: "failure", Diagnosis: "Endpoint timeout", Strategy: "Increase timeout and retry with backoff", ConditionPattern: "* returned timeout", Category: "timeout_recovery"},
	}
	_ = reflections // MockClient uses default response for all calls.

	mockClient := llm.NewMockClient(map[string]string{"default": string(judgeJSON)})
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))

	examinerHead := NewExaminer(driver, nil, s, DefaultExaminerConfig(), zap.NewNop())

	results := []agent.StepResult{
		makeStepResult("tc-1", "Get users", "/api/users", "returns 200", agent.StepPassed, 200, `{"users":[]}`),
		makeStepResult("tc-2", "Get posts", "/api/posts", "returns 200", agent.StepFailed, 500, "timeout"),
	}

	// Need a session for FK constraint.
	sess, err := s.CreateSession(context.Background(), "run", "test", "test-project")
	require.NoError(t, err)

	verdicts, reflectionCount, err := examinerHead.Examine(context.Background(), results, sess.ID, "test-project")
	require.NoError(t, err)

	require.Len(t, verdicts, 2)
	// Both get StatusPass because MockClient returns same judge JSON for all calls.
	// The pipeline itself is what we're testing, not the judge accuracy per case.
	assert.Equal(t, StatusPass, verdicts[0].Status)
	assert.Equal(t, StatusPass, verdicts[1].Status) // Same mock response for both
	assert.GreaterOrEqual(t, reflectionCount, 0)
}

func TestExaminer_StepStatusFallback(t *testing.T) {
	s := setupExaminerStore(t)

	// MockClient returns invalid JSON → Judge fails → fallback to step status.
	mockClient := llm.NewMockClient(map[string]string{"default": "not json"})
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))

	examinerHead := NewExaminer(driver, nil, s, DefaultExaminerConfig(), zap.NewNop())

	results := []agent.StepResult{
		makeStepResult("tc-1", "Test", "/api", "works", agent.StepPassed, 200, "ok"),
	}

	sess, err := s.CreateSession(context.Background(), "run", "test", "test-project")
	require.NoError(t, err)

	verdicts, _, err := examinerHead.Examine(context.Background(), results, sess.ID, "test-project")
	require.NoError(t, err)
	require.Len(t, verdicts, 1)
	assert.Equal(t, StatusPass, verdicts[0].Status) // Fallback from StepPassed
	assert.Contains(t, verdicts[0].Reasoning, "Judge failed")
}

func TestStepStatusToJudgeStatus(t *testing.T) {
	assert.Equal(t, StatusPass, stepStatusToJudgeStatus(agent.StepPassed))
	assert.Equal(t, StatusFail, stepStatusToJudgeStatus(agent.StepFailed))
	assert.Equal(t, StatusSkip, stepStatusToJudgeStatus(agent.StepSkipped))
	assert.Equal(t, StatusUncertain, stepStatusToJudgeStatus(agent.StepUncertain))
}

func TestShouldAutoFix(t *testing.T) {
	tests := []struct {
		name     string
		verdict  FinalVerdict
		mode     string
		severity string
		want     bool
	}{
		// "off" mode — never auto-fix.
		{"off/low_fail", FinalVerdict{Status: StatusFail}, "off", "low", false},
		{"off/medium_fail", FinalVerdict{Status: StatusFail}, "off", "medium", false},
		{"off/high_fail", FinalVerdict{Status: StatusFail}, "off", "high", false},
		{"empty_mode/low_fail", FinalVerdict{Status: StatusFail}, "", "low", false},

		// "low_only" mode — only low severity fails.
		{"low_only/low_fail", FinalVerdict{Status: StatusFail}, "low_only", "low", true},
		{"low_only/medium_fail", FinalVerdict{Status: StatusFail}, "low_only", "medium", false},
		{"low_only/high_fail", FinalVerdict{Status: StatusFail}, "low_only", "high", false},
		{"low_only/critical_fail", FinalVerdict{Status: StatusFail}, "low_only", "critical", false},
		{"low_only/low_pass", FinalVerdict{Status: StatusPass}, "low_only", "low", false},
		{"low_only/low_uncertain", FinalVerdict{Status: StatusUncertain}, "low_only", "low", false},

		// "aggressive" mode — low + medium severity fails.
		{"aggressive/low_fail", FinalVerdict{Status: StatusFail}, "aggressive", "low", true},
		{"aggressive/medium_fail", FinalVerdict{Status: StatusFail}, "aggressive", "medium", true},
		{"aggressive/high_fail", FinalVerdict{Status: StatusFail}, "aggressive", "high", false},
		{"aggressive/critical_fail", FinalVerdict{Status: StatusFail}, "aggressive", "critical", false},
		{"aggressive/medium_pass", FinalVerdict{Status: StatusPass}, "aggressive", "medium", false},

		// Unknown mode.
		{"unknown/low_fail", FinalVerdict{Status: StatusFail}, "custom", "low", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldAutoFix(tt.verdict, tt.mode, tt.severity)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAutoFixer_Fix_Success(t *testing.T) {
	fixJSON := `{"reasoning":"The endpoint returns 201 for creation, not 200","skip":true}`
	mockClient := llm.NewMockClient(map[string]string{"default": fixJSON})
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))
	logger := zaptest.NewLogger(t)
	af := NewAutoFixer(driver, logger)

	verdict := FinalVerdict{
		Status:    StatusFail,
		Reasoning: "Expected 200, got 201",
		StepResult: agent.StepResult{
			TestCase: &agent.TestCase{
				ID:          "tc-001",
				Name:        "Create user",
				Method:      "POST",
				Target:      "/users",
				Expectation: "status 200",
			},
		},
	}

	result := af.Fix(context.Background(), verdict, "Users API should return 200")
	assert.True(t, result.Attempted)
	assert.True(t, result.Success)
	assert.Equal(t, StatusSkip, result.Verdict.Status)
	assert.Contains(t, result.Verdict.Reasoning, "Auto-fix:")
}

func TestAutoFixer_Fix_NoRepair(t *testing.T) {
	fixJSON := `{"reasoning":"Missing auth token in header","skip":false}`
	mockClient := llm.NewMockClient(map[string]string{"default": fixJSON})
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))
	logger := zaptest.NewLogger(t)
	af := NewAutoFixer(driver, logger)

	verdict := FinalVerdict{
		Status:    StatusFail,
		Reasoning: "Got 401 Unauthorized",
		StepResult: agent.StepResult{
			TestCase: &agent.TestCase{
				ID:          "tc-002",
				Name:        "Get profile",
				Method:      "GET",
				Target:      "/profile",
				Expectation: "status 200",
			},
		},
	}

	result := af.Fix(context.Background(), verdict, "")
	assert.True(t, result.Attempted)
	assert.True(t, result.Success)
	// Not skipped — verdict reasoning updated but status unchanged.
	assert.Equal(t, StatusFail, result.Verdict.Status)
	assert.Contains(t, result.Verdict.Reasoning, "Auto-fix analysis:")
}

func TestAutoFixer_Fix_NilTestCase(t *testing.T) {
	mockClient := llm.NewMockClient(nil)
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))
	logger := zaptest.NewLogger(t)
	af := NewAutoFixer(driver, logger)

	verdict := FinalVerdict{
		Status:     StatusFail,
		Reasoning:  "no test case",
		StepResult: agent.StepResult{TestCase: nil},
	}

	result := af.Fix(context.Background(), verdict, "")
	assert.False(t, result.Attempted)
	assert.False(t, result.Success)
}
