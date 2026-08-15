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

// RedispatchHint is the Examiner's structured diagnosis of a failure's
// correctable cause (feature #3). "none" means no targeted replanning; the
// others name a cause a replacement case could address. Defined in package
// agent (not examiner) so both scout and examiner can reference it without a
// scout->examiner import cycle.
type RedispatchHint string

const (
	HintNone          RedispatchHint = "none"
	HintEndpointDrift RedispatchHint = "endpoint_drift" // wrong path/method/verb
	HintAuth          RedispatchHint = "auth"           // missing/bad credentials or scheme
	HintShape         RedispatchHint = "shape"          // wrong payload/contract shape
	// D2 WS hints: WebSocket-correctable failure causes. Each implicates a
	// specific TestStep field for Scout to repair (see D2 spec §5.1).
	HintHandshake RedispatchHint = "handshake" // WS: mandatory/role handshake await mismatch
	HintWsShape   RedispatchHint = "ws_shape"  // WS: wrong ws_send message envelope/payload
	HintWsMatch   RedispatchHint = "ws_match"  // WS: ws_receive type/assert/match_all criteria wrong
	// HintCoverage is SESSION-SYNTHESIZED (D1 spec §5.1): the coverage gate was
	// not reached. It is never LLM-emitted and never parsed from judge output;
	// persisted verdicts carry it via JSON struct tags. parseRedispatchHint
	// rejects "coverage" (collapses to HintNone).
	HintCoverage RedispatchHint = "coverage"
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
	// FallbackFor is the ID of the primary case this case is a lazy fallback for
	// (A1 Phase 2). Empty on normal cases. The Agent skips a lazy fallback by
	// default and activates it only when its primary case fails at execution.
	FallbackFor string `json:"fallback_for,omitempty"`
	// Replaces is the ID of the failed case this case is a targeted replacement
	// for (feature #3). Empty on normal/planned cases. A replacement is scheduled
	// explicitly by the repair loop (NOT lazily activated like FallbackFor).
	Replaces string `json:"replaces,omitempty"`
	// Claims names the claims-ledger ids this case claims to prove. Used by
	// claims reconciliation to map product promises to evidence. Repair-loop
	// replacements and lazy fallbacks INHERIT the original case's Claims.
	Claims []string `json:"claims,omitempty"`
}

// TestStep is a single step in a deterministic WebSocket flow.
// When a TestCase has non-empty Steps, the deterministic multi-step path runs
// the steps and Action/Body are ignored for execution.
type TestStep struct {
	Action       string         `json:"action"`                  // ws_connect, ws_send, ws_receive, ws_disconnect
	ConnectionID string         `json:"connection_id,omitempty"` // Identifies the WS connection
	Role         string         `json:"role,omitempty"`          // Optional: role name (e.g., "web", "device")
	URL          string         `json:"url,omitempty"`           // ws_connect only: dial URL (defaults to tc.Target when empty)
	Message      string         `json:"message,omitempty"`       // For ws_send: JSON payload to send
	Type         string         `json:"type,omitempty"`          // For ws_receive: expected message type
	Aliases      []string       `json:"aliases,omitempty"`       // ws_receive: additional matching types
	Asserts      map[string]any `json:"asserts,omitempty"`       // For ws_receive: field assertions
	MatchAll     bool           `json:"match_all,omitempty"`     // ws_receive: collect every matching item in the burst (see WSReceiveAction.MatchAll)
	Timeout      int            `json:"timeout,omitempty"`       // ws_receive: seconds (0 ⇒ executor default)
	ExpectAbsent bool           `json:"expect_absent,omitempty"` // ws_receive: assert the type does NOT arrive (sender-exclusion probe)
	Code         int            `json:"code,omitempty"`          // ws_expect_close: expected close status code (e.g. 1009)
	// http_request: HTTP method (GET/POST/...). Defaults to GET when empty.
	Method string `json:"method,omitempty"`
	// http_request: explicit request headers (e.g. an injected Authorization).
	// When AuthRole is also set, explicit Headers override the auth header.
	Headers map[string]string `json:"headers,omitempty"`
	// http_request: request body (raw string, typically JSON).
	Body string `json:"body,omitempty"`
	// http_request: expected response status; 0 ⇒ do not assert (rely on the
	// executor's own success/ok gate).
	ExpectStatus int `json:"expect_status,omitempty"`
	// http_request: a declared role whose actor's HTTP token (http_login) is
	// injected as Authorization: Bearer <token>. Empty ⇒ no auth injection
	// (Headers must supply auth, if needed).
	AuthRole string `json:"auth_role,omitempty"`
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
	Type         string `json:"type"`
	Content      string `json:"content"`
	Action       string `json:"action,omitempty"`        // ws_connect|ws_send|ws_receive|...
	ConnectionID string `json:"connection_id,omitempty"` // WS step's connection
	MatchedType  string `json:"matched_type,omitempty"`  // ws_receive expected / ws_send sent type
	Matched      bool   `json:"matched,omitempty"`       // ws_receive observed a matching frame
	ExpectAbsent bool   `json:"expect_absent,omitempty"` // ws_receive: this was a negative (sender-exclusion) probe
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
	// Recovered is true when this result is a lazy fallback case that ran because
	// its primary case failed, and the fallback passed (A1 Phase 2). The primary
	// case's own result stays a fail; this marks the role recovered, not passed.
	Recovered bool
}

// String returns a human-readable summary of the result.
func (r StepResult) String() string {
	return fmt.Sprintf("%s: %s (%d attempts, %s)",
		r.TestCase.ID, r.Status, r.Attempts, r.Duration.Round(time.Millisecond))
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
