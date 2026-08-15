package types

import (
	"fmt"
	"net/url"
	"time"
)

var secretQueryParams = map[string]bool{
	"token": true, "password": true, "secret": true, "key": true,
	"apikey": true, "api_key": true, "authorization": true,
}

// redactSecretQuery redacts known-sensitive query params from a url string.
func redactSecretQuery(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	changed := false
	for k := range q {
		if secretQueryParams[k] {
			q.Set(k, "<redacted>")
			changed = true
		}
	}
	if !changed {
		return raw
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// WSResult represents a WebSocket operation result.
type WSResult struct {
	OK  bool   `json:"success"`
	URL string `json:"url"`
	// MatchedMessage is the message that satisfied a WSReceive match (empty for
	// connect/send/disconnect). Under MatchAll it holds the FIRST matched frame
	// (or the FAILING frame on an assert error) for back-compat readers.
	MatchedMessage string `json:"matched_message,omitempty"`
	// MatchedMessages are all frames collected by a MatchAll receive (every item
	// of a burst). Empty for non-MatchAll receives and for connect/send/disconnect.
	MatchedMessages []string `json:"matched_messages,omitempty"`
	// MatchedCount is the number of frames a MatchAll receive collected. Zero for
	// non-MatchAll receives (use MatchedMessage presence for single-match counts).
	MatchedCount int `json:"matched_count,omitempty"`
	// CloseCode is the peer's close status code observed by ws_expect_close
	// (zero for every other action).
	CloseCode int `json:"close_code,omitempty"`
	// SeenMessages are non-matching messages observed while WSReceive scanned.
	SeenMessages []string `json:"seen_messages,omitempty"`
	// Messages is the legacy combined list (kept for back-compat readers).
	Messages []string `json:"messages,omitempty"`
	// ConnectionID is the id ws_connect used or auto-assigned (empty for
	// send/receive/disconnect). Echoed so the Steer LLM can reuse an
	// auto-assigned id on ws_send/ws_receive instead of failing with
	// "unknown connection_id" (2026-07-21 dogfood Finding 4).
	ConnectionID string        `json:"connection_id,omitempty"`
	Latency      time.Duration `json:"duration"`
	Err          string        `json:"error,omitempty"`
}

func (r WSResult) Success() bool           { return r.OK }
func (r WSResult) Duration() time.Duration { return r.Latency }
func (r WSResult) Summary() string {
	status := "ok"
	if !r.OK {
		status = "error"
	}
	matched := r.MatchedCount
	if matched == 0 && r.MatchedMessage != "" {
		matched = 1
	}
	conn := ""
	if r.ConnectionID != "" {
		conn = fmt.Sprintf(" connection_id=%s", r.ConnectionID)
	}
	return fmt.Sprintf("ws %s %s%s (matched=%d seen=%d, %s)", status, redactSecretQuery(r.URL), conn, matched, len(r.SeenMessages), r.Latency)
}
func (r WSResult) Evidence() EvidenceData {
	var all []string
	if r.MatchedMessage != "" {
		all = append(all, "matched: "+r.MatchedMessage)
	}
	all = append(all, r.MatchedMessages...)
	all = append(all, r.SeenMessages...)
	all = append(all, r.Messages...)
	return EvidenceData{Type: "ws_messages", Content: truncate(joinStrings(all, "\n"), 10000)}
}
