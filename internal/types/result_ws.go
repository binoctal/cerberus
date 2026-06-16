package types

import (
	"fmt"
	"time"
)

// WSResult represents a WebSocket operation result.
type WSResult struct {
	OK       bool          `json:"success"`
	URL      string        `json:"url"`
	Messages []string      `json:"messages,omitempty"`
	Latency  time.Duration `json:"duration"`
	Err      string        `json:"error,omitempty"`
}

func (r WSResult) Success() bool           { return r.OK }
func (r WSResult) Duration() time.Duration { return r.Latency }
func (r WSResult) Summary() string {
	status := "ok"
	if !r.OK {
		status = "error"
	}
	return fmt.Sprintf("ws %s %s (%d msgs, %s)", status, r.URL, len(r.Messages), r.Latency)
}
func (r WSResult) Evidence() EvidenceData {
	return EvidenceData{Type: "ws_messages", Content: truncate(joinStrings(r.Messages, "\n"), 10000)}
}
