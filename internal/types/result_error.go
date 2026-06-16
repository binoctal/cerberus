package types

import (
	"fmt"
	"time"
)

// ErrorResult represents a generic error result.
type ErrorResult struct {
	Err     string        `json:"error"`
	Latency time.Duration `json:"duration,omitempty"`
}

func (r ErrorResult) Success() bool           { return false }
func (r ErrorResult) Duration() time.Duration { return r.Latency }
func (r ErrorResult) Summary() string         { return fmt.Sprintf("error: %s", r.Err) }
func (r ErrorResult) Evidence() EvidenceData {
	return EvidenceData{Type: "error", Content: r.Err}
}
