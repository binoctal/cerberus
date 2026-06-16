package types

import (
	"fmt"
	"time"
)

// WaitResult represents a wait/sleep operation result.
type WaitResult struct {
	OK      bool          `json:"success"`
	Latency time.Duration `json:"duration"`
}

func (r WaitResult) Success() bool           { return r.OK }
func (r WaitResult) Duration() time.Duration { return r.Latency }
func (r WaitResult) Summary() string {
	return fmt.Sprintf("wait completed (%s)", r.Latency)
}
func (r WaitResult) Evidence() EvidenceData {
	return EvidenceData{Type: "wait", Content: "completed"}
}
