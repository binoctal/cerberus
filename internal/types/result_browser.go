package types

import (
	"fmt"
	"time"
)

// BrowserResult represents a browser automation result.
type BrowserResult struct {
	OK         bool          `json:"success"`
	URL        string        `json:"url"`
	Title      string        `json:"title"`
	Text       string        `json:"text,omitempty"`
	Screenshot string        `json:"screenshot,omitempty"` // base64 encoded
	EvalResult string        `json:"eval_result,omitempty"`
	Latency    time.Duration `json:"duration"`
	Err        string        `json:"error,omitempty"`
}

func (r BrowserResult) Success() bool           { return r.OK }
func (r BrowserResult) Duration() time.Duration { return r.Latency }
func (r BrowserResult) Summary() string {
	status := "ok"
	if !r.OK {
		status = "error"
	}
	return fmt.Sprintf("browser %s %s (%s)", status, r.URL, r.Latency)
}
func (r BrowserResult) Evidence() EvidenceData {
	content := r.Text
	if r.EvalResult != "" {
		content = r.EvalResult
	}
	return EvidenceData{Type: "browser_content", Content: truncate(content, 10000)}
}
