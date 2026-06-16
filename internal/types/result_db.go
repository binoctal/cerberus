package types

import (
	"encoding/json"
	"fmt"
	"time"
)

// DBResult represents a database query result.
type DBResult struct {
	OK              bool             `json:"success"`
	Driver          string           `json:"driver"`
	Query           string           `json:"query"`
	Columns         []string         `json:"columns,omitempty"`
	Rows            []map[string]any `json:"rows,omitempty"`
	AssertionPassed bool             `json:"assertion_passed,omitempty"`
	Latency         time.Duration    `json:"duration"`
	Err             string           `json:"error,omitempty"`
}

func (r DBResult) Success() bool           { return r.OK }
func (r DBResult) Duration() time.Duration { return r.Latency }
func (r DBResult) Summary() string {
	status := "ok"
	if !r.OK {
		status = "error"
	}
	return fmt.Sprintf("db %s %s (%d rows, %s)", status, r.Driver, len(r.Rows), r.Latency)
}
func (r DBResult) Evidence() EvidenceData {
	content, _ := json.Marshal(r.Rows)
	return EvidenceData{Type: "db_result", Content: truncate(string(content), 10000)}
}
