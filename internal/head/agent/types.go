package agent

import (
	"fmt"
	"time"
)

// StepStatus represents the outcome of a single test step.
type StepStatus string

const (
	StepPassed    StepStatus = "pass"
	StepFailed    StepStatus = "fail"
	StepSkipped   StepStatus = "skip"
	StepUncertain StepStatus = "uncertain"
)

// ActionType enumerates operations the agent can perform.
type ActionType string

const (
	ActionClick      ActionType = "click"
	ActionInput      ActionType = "type"
	ActionNavigate   ActionType = "navigate"
	ActionAPIRequest ActionType = "api_request"
	ActionWait       ActionType = "wait"
)

// Action is a single operation decided by the rule engine or AI Steer.
type Action struct {
	Type    ActionType         `json:"type"`
	Target  string             `json:"target"`
	Value   string             `json:"value,omitempty"`
	Method  string             `json:"method,omitempty"`
	Headers map[string]string  `json:"headers,omitempty"`
}

// Observation is what the agent observes after executing an action.
type Observation struct {
	Success    bool              `json:"success"`
	StatusCode int               `json:"status_code,omitempty"`
	Body       string            `json:"body,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	Error      string            `json:"error,omitempty"`
	Duration   time.Duration     `json:"-"`
	URL        string            `json:"url,omitempty"`
}

// Evidence is collected data from test execution.
type Evidence struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

// StepResult is the outcome of executing a single TestCase.
type StepResult struct {
	TestCase   *TestCase
	Status     StepStatus
	Evidence   []Evidence
	TraceID    int64
	Attempts   int
	Duration   time.Duration
	LastAction Action
	LastObs    Observation
	Error      error
}

// String returns a human-readable summary of the result.
func (r StepResult) String() string {
	return fmt.Sprintf("%s: %s (%d attempts, %s)",
		r.TestCase.ID, r.Status, r.Attempts, r.Duration.Round(time.Millisecond))
}

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
}

// TestPlan is the output from the Scout head, input to the Agent head.
type TestPlan struct {
	Goal       string     `json:"goal"`
	Cases      []TestCase `json:"cases"`
	ProjectURL string     `json:"project_url"`
}

// SteerOutput is the structured JSON the LLM returns for a Steer decision.
type SteerOutput struct {
	Reasoning string `json:"reasoning"`
	Action    Action `json:"action"`
}

// RecoverOutput is the structured JSON the LLM returns for a Recover decision.
type RecoverOutput struct {
	Diagnosis string `json:"diagnosis"`
	Action    Action `json:"action"`
	Skip      bool   `json:"skip"`
}

// ReActConfig holds tunable parameters for the ReAct loop.
type ReActConfig struct {
	MaxSteerAttempts   int `json:"max_steer_attempts"`
	MaxRecoverAttempts int `json:"max_recover_attempts"`
}

// DefaultReActConfig returns sensible defaults.
func DefaultReActConfig() ReActConfig {
	return ReActConfig{
		MaxSteerAttempts:   3,
		MaxRecoverAttempts: 3,
	}
}
