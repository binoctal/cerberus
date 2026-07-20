package types

import (
	"fmt"
	"time"
)

// WSResult represents a WebSocket operation result.
type WSResult struct {
	OK  bool   `json:"success"`
	URL string `json:"url"`
	// MatchedMessage is the message that satisfied a WSReceive match (empty for
	// connect/send/disconnect).
	MatchedMessage string `json:"matched_message,omitempty"`
	// SeenMessages are non-matching messages observed while WSReceive scanned.
	SeenMessages []string `json:"seen_messages,omitempty"`
	// Messages is the legacy combined list (kept for back-compat readers).
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
	matched := 0
	if r.MatchedMessage != "" {
		matched = 1
	}
	return fmt.Sprintf("ws %s %s (matched=%d seen=%d, %s)", status, r.URL, matched, len(r.SeenMessages), r.Latency)
}
func (r WSResult) Evidence() EvidenceData {
	var all []string
	if r.MatchedMessage != "" {
		all = append(all, "matched: "+r.MatchedMessage)
	}
	all = append(all, r.SeenMessages...)
	all = append(all, r.Messages...)
	return EvidenceData{Type: "ws_messages", Content: truncate(joinStrings(all, "\n"), 10000)}
}
