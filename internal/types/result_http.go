package types

import (
	"fmt"
	"time"
)

// HTTPResult represents an HTTP request/response.
type HTTPResult struct {
	OK         bool              `json:"success"`
	StatusCode int               `json:"status_code"`
	Body       string            `json:"body"`
	Headers    map[string]string `json:"headers"`
	URL        string            `json:"url"`
	Latency    time.Duration     `json:"duration"`
	Err        string            `json:"error,omitempty"`
}

func (r HTTPResult) Success() bool           { return r.OK }
func (r HTTPResult) Duration() time.Duration { return r.Latency }
func (r HTTPResult) Summary() string {
	return fmt.Sprintf("HTTP %d %s (%s)", r.StatusCode, r.URL, r.Latency)
}
func (r HTTPResult) Evidence() EvidenceData {
	return EvidenceData{Type: "http_response", Content: truncate(r.Body, 10000)}
}
