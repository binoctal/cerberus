package agent

import (
	"fmt"
	"time"

	"github.com/binoctal/cerberus/internal/types"
)

// StepStatus represents the outcome of a single test step.
type StepStatus string

const (
	StepPassed    StepStatus = "pass"
	StepFailed    StepStatus = "fail"
	StepSkipped   StepStatus = "skip"
	StepUncertain StepStatus = "uncertain"
)

// TestCase represents a single testable operation.
type TestCase struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Target      string  `json:"target"`
	Method      string  `json:"method,omitempty"`
	Action      string  `json:"action,omitempty"`
	Expectation string  `json:"expectation,omitempty"`
	Priority    float64 `json:"priority,omitempty"`
	DependsOn   string  `json:"depends_on,omitempty"`
	Language    string  `json:"language,omitempty"`
	Background  bool    `json:"background,omitempty"`
	WaitFor     string  `json:"wait_for,omitempty"`
	Cleanup     bool    `json:"cleanup,omitempty"`
}

// TestPlan is the output from the Scout head, input to the Agent head.
type TestPlan struct {
	Goal       string     `json:"goal"`
	Cases      []TestCase `json:"cases"`
	ProjectURL string     `json:"project_url"`
}

// Evidence is collected data from test execution.
type Evidence struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

// StepResult is the outcome of executing a single TestCase.
type StepResult struct {
	TestCase *TestCase
	Status   StepStatus
	Evidence []Evidence
	TraceID  int64
	Attempts int
	Duration time.Duration
	Action   types.TypedAction
	Result   types.ExecutorResult
	Error    error
}

// String returns a human-readable summary of the result.
func (r StepResult) String() string {
	return fmt.Sprintf("%s: %s (%d attempts, %s)",
		r.TestCase.ID, r.Status, r.Attempts, r.Duration.Round(time.Millisecond))
}

// SteerOutput is the structured JSON the LLM returns for a Steer decision.
type SteerOutput struct {
	Reasoning string               `json:"reasoning"`
	Envelope  types.ActionEnvelope `json:"action"`
}

// RecoverOutput is the structured JSON the LLM returns for a Recover decision.
type RecoverOutput struct {
	Diagnosis string               `json:"diagnosis"`
	Envelope  types.ActionEnvelope `json:"action,omitempty"`
	Skip      bool                 `json:"skip"`
}

// ReActConfig holds tunable parameters for the ReAct loop.
type ReActConfig struct {
	MaxSteerAttempts   int           `json:"max_steer_attempts"`
	MaxRecoverAttempts int           `json:"max_recover_attempts"`
	PerCaseTimeout     time.Duration `json:"per_case_timeout,omitempty"`
}

// DefaultReActConfig returns sensible defaults.
func DefaultReActConfig() ReActConfig {
	return ReActConfig{
		MaxSteerAttempts:   3,
		MaxRecoverAttempts: 3,
		PerCaseTimeout:     2 * time.Minute,
	}
}

// ProgressEvent represents a real-time event from the test runner.
type ProgressEvent struct {
	Type      string    `json:"type"`       // "case_start", "case_complete", "plan_complete"
	CaseID    string    `json:"case_id"`
	Status    StepStatus `json:"status"`
	Attempt   int       `json:"attempt"`
	Timestamp time.Time `json:"timestamp"`
}
