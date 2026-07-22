package agent

import (
	"encoding/json"
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
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Target      string     `json:"target"`
	Method      string     `json:"method,omitempty"`
	Action      string     `json:"action,omitempty"`
	Expectation string     `json:"expectation,omitempty"`
	Priority    float64    `json:"priority,omitempty"`
	DependsOn   Deps       `json:"depends_on,omitempty"`
	Language    string     `json:"language,omitempty"`
	Background  bool       `json:"background,omitempty"`
	WaitFor     string     `json:"wait_for,omitempty"`
	Cleanup     bool       `json:"cleanup,omitempty"`
	Severity    string     `json:"severity,omitempty"` // "low", "medium", "high", "critical" (from invariant)
	Service     string     `json:"service,omitempty"`
	Body        string     `json:"body,omitempty"`
	Steps       []TestStep `json:"steps,omitempty"` // Deterministic multi-step WebSocket flow
}

// TestStep is a single step in a deterministic WebSocket flow.
// When a TestCase has non-empty Steps, the deterministic multi-step path runs
// the steps and Action/Body are ignored for execution.
type TestStep struct {
	Action       string         `json:"action"`            // ws_connect, ws_send, ws_receive, ws_close
	ConnectionID string         `json:"connection_id"`     // Identifies the WS connection
	Role         string         `json:"role,omitempty"`    // Optional: role name (e.g., "web", "device")
	Message      string         `json:"message,omitempty"` // For ws_send: JSON payload to send
	Type         string         `json:"type,omitempty"`    // For ws_receive: expected message type
	Asserts      map[string]any `json:"asserts,omitempty"` // For ws_receive: field assertions
}

// Deps is a []string that unmarshals from either a single string or an array.
type Deps []string

// UnmarshalJSON handles both `"depends_on": "tc-001"` and `"depends_on": ["tc-001","tc-002"]`.
func (d *Deps) UnmarshalJSON(data []byte) error {
	// Try array first.
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		*d = arr
		return nil
	}
	// Try single string.
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		if s != "" {
			*d = []string{s}
		}
		return nil
	}
	return fmt.Errorf("depends_on must be string or array of strings")
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
	// ProceduralRecallTopK caps how many L3 memories recovery recalls per case.
	ProceduralRecallTopK int `json:"procedural_recall_top_k,omitempty"`
	// ProceduralRecallThreshold is the minimum trigram cosine similarity for an
	// L3 memory to be recalled during recovery. Lower = broader (noisier) recall.
	ProceduralRecallThreshold float64 `json:"procedural_recall_threshold,omitempty"`
}

// DefaultReActConfig returns sensible defaults.
func DefaultReActConfig() ReActConfig {
	return ReActConfig{
		MaxSteerAttempts:          3,
		MaxRecoverAttempts:        3,
		PerCaseTimeout:            2 * time.Minute,
		ProceduralRecallTopK:      5,
		ProceduralRecallThreshold: 0.1,
	}
}

// ProgressEvent represents a real-time event from the test runner.
type ProgressEvent struct {
	Type      string     `json:"type"` // "case_start", "case_complete", "plan_complete"
	CaseID    string     `json:"case_id"`
	Status    StepStatus `json:"status"`
	Attempt   int        `json:"attempt"`
	Timestamp time.Time  `json:"timestamp"`
}
