package types

import (
	"fmt"
	"time"
)

// ProcessResult represents a subprocess execution result.
type ProcessResult struct {
	OK       bool          `json:"success"`
	ExitCode int           `json:"exit_code"`
	Stdout   string        `json:"stdout"`
	Stderr   string        `json:"stderr"`
	Latency  time.Duration `json:"duration"`
	Err      string        `json:"error,omitempty"`
}

func (r ProcessResult) Success() bool           { return r.OK }
func (r ProcessResult) Duration() time.Duration { return r.Latency }
func (r ProcessResult) Summary() string {
	return fmt.Sprintf("exit %d (%s)\nstdout: %s", r.ExitCode, r.Latency, truncate(r.Stdout, 500))
}
func (r ProcessResult) Evidence() EvidenceData {
	return EvidenceData{Type: "process_output", Content: truncate(r.Stdout, 10000)}
}
