package examiner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/binoctal/cerberus/internal/types"
)

// judgeResultCall builds a judge_result tool call fixture matching the
// judgeTools() schema. Only the four LLM-emitted fields are set;
// self_critique/critique_triggered remain absent (they are code-set by the
// critique path, never LLM-emitted).
func judgeResultCall(status JudgeStatus, existence, correctness float64, reasoning string) llm.ToolCall {
	return llm.ToolCall{
		Name: "judge_result",
		Input: map[string]any{
			"status":                 string(status),
			"existence_confidence":   existence,
			"correctness_confidence": correctness,
			"reasoning":              reasoning,
		},
	}
}

// critiqueVerdictCall builds a critique_verdict tool call fixture matching the
// criticTools() schema.
func critiqueVerdictCall(issues bool, critique string, suggested JudgeStatus, suggestedConf float64) llm.ToolCall {
	return llm.ToolCall{
		Name: "critique_verdict",
		Input: map[string]any{
			"issues_found":         issues,
			"critique":             critique,
			"suggested_status":     string(suggested),
			"suggested_confidence": suggestedConf,
		},
	}
}

// suggestFixCall builds a suggest_fix tool call fixture matching the
// autofixTools() schema. `skip` is a field on the single object (autofix has
// no competing action surface).
func suggestFixCall(reasoning string, skip bool) llm.ToolCall {
	return llm.ToolCall{
		Name: "suggest_fix",
		Input: map[string]any{
			"reasoning": reasoning,
			"skip":      skip,
		},
	}
}

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
	// High confidence pass — should skip critique. DecideWithTools returns a
	// judge_result tool call; assembleJudge turns it into the verdict.
	mockClient := llm.NewMockClient(nil)
	mockClient.SetToolResponse("default", []llm.ToolCall{
		judgeResultCall(StatusPass, 0.95, 0.95, "Response matches expectation"),
	})
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))

	judge := NewJudge(driver, nil, DefaultExaminerConfig())
	result, err := judge.Judge(context.Background(), makeStepResult(
		"tc-1", "Get users", "/api/users", "returns 200", agent.StepPassed, 200, `{"users":[]}`,
	))
	require.NoError(t, err)
	assert.Equal(t, StatusPass, result.Status)
	assert.InDelta(t, 0.95, result.CorrectnessConfidence, 0.01)
	assert.False(t, result.CritiqueTriggered)
	// SelfCritique/CritiqueTriggered are code-set, not LLM-emitted: the
	// high-confidence path skips critique entirely so both stay zero.
	assert.Empty(t, result.SelfCritique)
}

func TestJudge_LowConfidence_TriggersCritique(t *testing.T) {
	// Low confidence judge verdict → triggers critic. The critic returns
	// IssuesFound=true → critique corrections are applied to the initial
	// verdict. Separate mocks so judge and critic return distinct tool calls.
	judgeMock := llm.NewMockClient(nil)
	judgeMock.SetToolResponse("default", []llm.ToolCall{
		judgeResultCall(StatusUncertain, 0.9, 0.6, "Response exists but content unclear"),
	})
	criticMock := llm.NewMockClient(nil)
	criticMock.SetToolResponse("default", []llm.ToolCall{
		critiqueVerdictCall(true, "False positive risk: status 200 but empty body", StatusFail, 0.3),
	})

	judgeDriver := ai.NewDriver(judgeMock, ai.NewTokenBudget(200000, 10000))
	criticDriver := ai.NewDriver(criticMock, ai.NewTokenBudget(200000, 10000))

	judge := NewJudge(judgeDriver, criticDriver, DefaultExaminerConfig())
	result, err := judge.Judge(context.Background(), makeStepResult(
		"tc-1", "Get users", "/api/users", "returns user list", agent.StepPassed, 200, "",
	))
	require.NoError(t, err)
	// Critique was applied: status/confidence overwritten, flags set.
	assert.Equal(t, StatusFail, result.Status, "critique should override status")
	assert.InDelta(t, 0.3, result.CorrectnessConfidence, 0.01)
	assert.True(t, result.CritiqueTriggered, "CritiqueTriggered must be code-set after critique")
	assert.Contains(t, result.SelfCritique, "False positive risk")
}

func TestJudge_NoCriticDriver(t *testing.T) {
	// Without critic driver, uncertain result stays uncertain.
	mockClient := llm.NewMockClient(nil)
	mockClient.SetToolResponse("default", []llm.ToolCall{
		judgeResultCall(StatusUncertain, 0.8, 0.5, "Cannot determine"),
	})
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
	mockClient := llm.NewMockClient(nil)
	mockClient.SetToolResponse("default", []llm.ToolCall{
		judgeResultCall(StatusUncertain, 0.8, 0.5, "Uncertain"),
	})
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))

	cfg := ExaminerConfig{MaxCritiques: 0, ConfThreshold: 0.9} // Max 0 critiques
	judge := NewJudge(driver, driver, cfg)

	result, err := judge.Judge(context.Background(), makeStepResult(
		"tc-1", "Test", "/api", "works", agent.StepPassed, 200, "ok",
	))
	require.NoError(t, err)
	assert.False(t, result.CritiqueTriggered)
}

// TestJudge_ZeroToolCalls_Error verifies the zero-call judge path: when the
// judge LLM emits no tool calls (drift/quality), Judge surfaces an error
// (not a silent verdict). The caller (examiner.go) maps this to fallbackVerdict.
func TestJudge_ZeroToolCalls_Error(t *testing.T) {
	mockClient := llm.NewMockClient(nil)
	mockClient.SetToolResponse("default", nil) // zero tool calls
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))

	judge := NewJudge(driver, nil, DefaultExaminerConfig())
	_, err := judge.Judge(context.Background(), makeStepResult(
		"tc-1", "Test", "/api", "works", agent.StepPassed, 200, "ok",
	))
	require.Error(t, err, "zero tool calls must surface as an error")
	assert.Contains(t, err.Error(), "zero tool calls")
}

// TestCritic_ZeroToolCalls_RefundsSlot covers the NEW zero-call refund path
// (today's tests only cover the error-refund path). When the critic LLM emits
// no tool calls (drift), the reserved critique slot is refunded and the
// initial verdict is kept — identical to the Decide-error refund policy.
func TestCritic_ZeroToolCalls_RefundsSlot(t *testing.T) {
	judgeMock := llm.NewMockClient(nil)
	judgeMock.SetToolResponse("default", []llm.ToolCall{
		judgeResultCall(StatusUncertain, 0.9, 0.6, "uncertain"),
	})
	// Critic returns ZERO tool calls (drift).
	criticMock := llm.NewMockClient(nil)
	criticMock.SetToolResponse("default", nil)

	judgeDriver := ai.NewDriver(judgeMock, ai.NewTokenBudget(200000, 10000))
	criticDriver := ai.NewDriver(criticMock, ai.NewTokenBudget(200000, 10000))

	cfg := ExaminerConfig{MaxCritiques: 1, ConfThreshold: 0.9}
	j := NewJudge(judgeDriver, criticDriver, cfg)

	result, err := j.Judge(context.Background(), makeStepResult(
		"tc-1", "Test", "/api", "works", agent.StepPassed, 200, "ok",
	))
	require.NoError(t, err)
	// Initial verdict kept (no critique applied).
	assert.Equal(t, StatusUncertain, result.Status)
	assert.False(t, result.CritiqueTriggered)
	// Slot was refunded: critiqueUsed back to 0, so a subsequent uncertain
	// verdict can still claim the slot.
	assert.Equal(t, int64(0), j.critiqueUsed.Load(),
		"zero-call critic must refund the reserved slot")
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
			name:     "valid failure reflection",
			r:        Reflection{Type: "failure", Diagnosis: "Auth token expired", Strategy: "Refresh token before retrying the request", ConditionPattern: "* returned 401", Category: "auth_failure"},
			expected: true,
		},
		{
			name:     "valid success reflection",
			r:        Reflection{Type: "success", Diagnosis: "Pagination works correctly", Strategy: "Always include page parameter for list endpoints", ConditionPattern: "GET /api/*/list*", Category: "general_failure"},
			expected: true,
		},
		{
			name:     "empty diagnosis",
			r:        Reflection{Type: "failure", Diagnosis: "", Strategy: "Some strategy here that is long enough", ConditionPattern: "* returned 500", Category: "server_error"},
			expected: false,
		},
		{
			name:     "strategy too short",
			r:        Reflection{Type: "failure", Diagnosis: "Timeout occurred", Strategy: "retry", ConditionPattern: "* timeout", Category: "timeout_recovery"},
			expected: false,
		},
		{
			name:     "invalid type",
			r:        Reflection{Type: "invalid", Diagnosis: "Something", Strategy: "A strategy that is long enough to pass", ConditionPattern: "*", Category: "general_failure"},
			expected: false,
		},
		{
			name:     "empty condition pattern",
			r:        Reflection{Type: "failure", Diagnosis: "Error", Strategy: "A strategy long enough to pass the gate", ConditionPattern: "", Category: "general_failure"},
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

	mockClient := llm.NewMockClient(nil)
	mockClient.SetToolResponse("default", []llm.ToolCall{
		reportReflectionCall(Reflection{
			Type:             "failure",
			Diagnosis:        "Auth token expired causing 401",
			Strategy:         "Refresh auth token before retrying the request",
			ConditionPattern: "* returned 401",
			Category:         "auth_failure",
		}),
	})
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

	// Mix of valid and invalid reflections — emitted as 3 report_reflection
	// tool calls. assembleReflections walks all three; qualityGate drops the
	// two invalid ones before L3 storage.
	mockClient := llm.NewMockClient(nil)
	mockClient.SetToolResponse("default", []llm.ToolCall{
		reportReflectionCall(Reflection{Type: "failure", Diagnosis: "Valid diagnosis", Strategy: "Valid strategy that is long enough", ConditionPattern: "* returned 500", Category: "server_error"}),
		reportReflectionCall(Reflection{Type: "failure", Diagnosis: "", Strategy: "Strategy without diagnosis", ConditionPattern: "*", Category: "general_failure"}), // Invalid: empty diagnosis
		reportReflectionCall(Reflection{Type: "success", Diagnosis: "Works well", Strategy: "short", ConditionPattern: "GET *", Category: "general_failure"}),        // Invalid: strategy too short
	})
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

	// Judge returns pass with high confidence via a judge_result tool call.
	// Keyed on the judge task substring so the learner gets no tool-call match
	// (zero reflections, no error) — the pipeline test only asserts verdicts,
	// not reflections.
	mockClient := llm.NewMockClient(nil)
	mockClient.SetToolResponse("Evaluate this test evidence", []llm.ToolCall{
		judgeResultCall(StatusPass, 0.95, 0.95, "Response matches expectation"),
	})
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
	// Both get StatusPass because the judge mock returns the same tool call.
	// The pipeline itself is what we're testing, not per-case judge accuracy.
	assert.Equal(t, StatusPass, verdicts[0].Status)
	assert.Equal(t, StatusPass, verdicts[1].Status)
	assert.GreaterOrEqual(t, reflectionCount, 0)
}

func TestExaminer_StepStatusFallback(t *testing.T) {
	s := setupExaminerStore(t)

	// No tool response → Judge gets zero tool calls → error → fallback verdict.
	mockClient := llm.NewMockClient(nil)
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

func TestExaminer_Parallel_PreservesVerdictsByIndex(t *testing.T) {
	s := setupExaminerStore(t)
	// No tool calls → Judge fails → verdict falls back to each step's own
	// status, so per-index verdicts are distinguishable. Parallel Examine must
	// preserve the input order exactly (verdicts are written by index into a
	// pre-allocated slice, with no mutex, so any ordering bug shows up as a
	// mismatch here).
	mockClient := llm.NewMockClient(nil)
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))
	cfg := DefaultExaminerConfig()
	cfg.MaxWorkers = 3 // < len(results) to exercise worker slot reuse.
	examinerHead := NewExaminer(driver, nil, s, cfg, zap.NewNop())

	statuses := []agent.StepStatus{
		agent.StepPassed, agent.StepFailed, agent.StepPassed,
		agent.StepFailed, agent.StepPassed, agent.StepFailed,
	}
	results := make([]agent.StepResult, len(statuses))
	for i, st := range statuses {
		results[i] = makeStepResult("tc-"+string(rune('a'+i)), "Test", "/api", "works", st, 200, "ok")
	}

	sess, err := s.CreateSession(context.Background(), "run", "test", "test-project")
	require.NoError(t, err)

	verdicts, _, err := examinerHead.Examine(context.Background(), results, sess.ID, "test-project")
	require.NoError(t, err)
	require.Len(t, verdicts, len(statuses))

	for i, st := range statuses {
		assert.Equal(t, stepStatusToJudgeStatus(st), verdicts[i].Status,
			"verdict[%d] must match its own step status (parallel preserves order)", i)
	}
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

// TestAutoFixer_Fix_ToolCallAssembles is the S4 RED→GREEN gate: preset
// suggest_fix{skip:false,reasoning:"..."} and assert the assembled reasoning
// flows through Fix() into the verdict. Pre-migration this fixture was a JSON
// string; post-migration it is a tool call that assembleAutofix unwraps.
func TestAutoFixer_Fix_ToolCallAssembles(t *testing.T) {
	mockClient := llm.NewMockClient(nil)
	mockClient.SetToolResponse("default", []llm.ToolCall{
		suggestFixCall("Missing Authorization header", false),
	})
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
	assert.Contains(t, result.Verdict.Reasoning, "Missing Authorization header")
	assert.Contains(t, result.Verdict.Reasoning, "Auto-fix analysis:")
}

// TestAutoFixer_Fix_Skip_True verifies the skip:true branch: the verdict is
// downgraded to StatusSkip and the reasoning is prefixed "Auto-fix:".
func TestAutoFixer_Fix_Skip_True(t *testing.T) {
	mockClient := llm.NewMockClient(nil)
	mockClient.SetToolResponse("default", []llm.ToolCall{
		suggestFixCall("Endpoint returns 201 for creation, not 200", true),
	})
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

// TestAutoFixer_Fix_ZeroToolCalls_Degrades verifies the autofix error policy:
// zero tool calls (drift/quality) degrade to {Attempted:true, Success:false}
// (NOT propagate). autofix is part of the repair loop — a degraded verdict
// means "no repair applied, keep the original fail", which the loop already
// handles.
func TestAutoFixer_Fix_ZeroToolCalls_Degrades(t *testing.T) {
	mockClient := llm.NewMockClient(nil)
	mockClient.SetToolResponse("default", nil) // zero tool calls
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))
	logger := zaptest.NewLogger(t)
	af := NewAutoFixer(driver, logger)

	verdict := FinalVerdict{
		Status:    StatusFail,
		Reasoning: "Got 401",
		StepResult: agent.StepResult{
			TestCase: &agent.TestCase{ID: "tc-1", Name: "T", Method: "GET", Target: "/x", Expectation: "200"},
		},
	}

	result := af.Fix(context.Background(), verdict, "")
	assert.True(t, result.Attempted, "zero tool calls still counts as an attempt")
	assert.False(t, result.Success, "zero tool calls degrade to Success=false (no repair applied)")
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
