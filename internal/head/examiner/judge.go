package examiner

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
)

// Judge performs Self-Refine evaluation: initial judgment (main model) +
// optional critique (critic model) for uncertain results.
type Judge struct {
	judgeDriver  *ai.Driver
	criticDriver *ai.Driver // nil means no Self-Refine
	config       ExaminerConfig
	critiqueUsed int // session-level critique counter
}

// NewJudge creates a Judge with main and optional critic drivers.
func NewJudge(judgeDriver, criticDriver *ai.Driver, config ExaminerConfig) *Judge {
	return &Judge{
		judgeDriver:  judgeDriver,
		criticDriver: criticDriver,
		config:       config,
	}
}

// Judge evaluates a step result against its expectation and returns a verdict.
func (j *Judge) Judge(ctx context.Context, result agent.StepResult) (*JudgeResult, error) {
	evidence := j.buildEvidenceContext(result)
	expectation := result.TestCase.Expectation
	task := fmt.Sprintf("Evaluate this test evidence against expectations.\nExpectation: %s", expectation)

	prompt := ai.NewPrompt().
		System(promptJudgeSystem).
		Context(evidence).
		Task(task).
		Output(promptJudgeOutput).
		Build()

	var judgeResult JudgeResult
	if err := j.judgeDriver.Decide(ctx, prompt, &judgeResult); err != nil {
		return nil, fmt.Errorf("judge decide: %w", err)
	}

	// Phase 2: Early stop — high confidence, not uncertain.
	if j.isHighConfidence(&judgeResult) {
		return &judgeResult, nil
	}

	// Phase 3: Self-Refine critique if critic available.
	if j.criticDriver != nil && j.critiqueUsed < j.config.MaxCritiques {
		critiqued := j.critique(ctx, judgeResult, evidence, expectation)
		if critiqued != nil {
			return critiqued, nil
		}
	}

	return &judgeResult, nil
}

// isHighConfidence checks if the result is confident enough to skip critique.
func (j *Judge) isHighConfidence(r *JudgeResult) bool {
	return r.CorrectnessConfidence >= j.config.ConfThreshold && r.Status != StatusUncertain
}

// critique runs the critic model to review the initial verdict.
func (j *Judge) critique(ctx context.Context, initial JudgeResult, evidence, expectation string) *JudgeResult {
	judgeJSON, _ := json.Marshal(initial)
	task := fmt.Sprintf("Review this initial verdict for errors.\nInitial verdict: %s\nEvidence: %s\nExpectation: %s",
		string(judgeJSON), evidence, expectation)

	prompt := ai.NewPrompt().
		System(promptCriticSystem).
		Task(task).
		Output(promptCriticOutput).
		Build()

	var critique CritiqueResult
	if err := j.criticDriver.Decide(ctx, prompt, &critique); err != nil {
		// Critic failed — return nil to use initial result.
		return nil
	}

	j.critiqueUsed++

	if !critique.IssuesFound {
		return nil // No issues found — keep initial result.
	}

	// Apply critique corrections.
	result := initial
	result.Status = critique.SuggestedStatus
	result.CorrectnessConfidence = critique.SuggestedConfidence
	result.SelfCritique = critique.Critique
	result.CritiqueTriggered = true
	return &result
}

// buildEvidenceContext formats step result data for the Judge prompt.
func (j *Judge) buildEvidenceContext(r agent.StepResult) string {
	var b []byte
	b = append(b, fmt.Sprintf("Test Case: %s (%s)\n", r.TestCase.Name, r.TestCase.ID)...)
	b = append(b, fmt.Sprintf("Target: %s\n", r.TestCase.Target)...)
	b = append(b, fmt.Sprintf("Status: %s\n", r.Status)...)
	b = append(b, fmt.Sprintf("Attempts: %d\n", r.Attempts)...)

	if r.LastObs.StatusCode != 0 {
		b = append(b, fmt.Sprintf("HTTP Status: %d\n", r.LastObs.StatusCode)...)
	}
	if r.LastObs.Body != "" {
		// Truncate large bodies to keep prompt manageable.
		body := r.LastObs.Body
		if len(body) > 2000 {
			body = body[:2000] + "\n... (truncated)"
		}
		b = append(b, fmt.Sprintf("Response Body: %s\n", body)...)
	}
	if r.LastObs.Error != "" {
		b = append(b, fmt.Sprintf("Error: %s\n", r.LastObs.Error)...)
	}
	if r.LastAction.Type != "" {
		actionJSON, _ := json.Marshal(r.LastAction)
		b = append(b, fmt.Sprintf("Last Action: %s\n", string(actionJSON))...)
	}
	if r.Error != nil {
		b = append(b, fmt.Sprintf("Step Error: %s\n", r.Error)...)
	}

	return string(b)
}
