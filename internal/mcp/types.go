// internal/mcp/types.go
package mcp

// SessionProgress tracks the state of a running test session.
type SessionProgress struct {
	SessionID string        `json:"session_id"`
	Phase     string        `json:"phase"`
	Completed int           `json:"completed"`
	Total     int           `json:"total"`
	Status    string        `json:"status"`
	Event     *PendingEvent `json:"event,omitempty"`
}

// PendingEvent describes a critical event awaiting user decision.
type PendingEvent struct {
	Type    string         `json:"type"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data,omitempty"`
}

// RunParams is the input for cerberus_run tool.
type RunParams struct {
	Goal string `json:"goal"`
	URL  string `json:"url"`
}

// DecideParams is the input for cerberus_decide tool.
type DecideParams struct {
	SessionID string `json:"session_id"`
	Action    string `json:"action"`
	Payload   string `json:"payload,omitempty"`
}

// ReportEntry is a single test result in the report.
type ReportEntry struct {
	CaseID     string  `json:"case_id"`
	Name       string  `json:"name"`
	Target     string  `json:"target"`
	Status     string  `json:"status"`
	Confidence float64 `json:"confidence"`
	Reasoning  string  `json:"reasoning,omitempty"`
}
