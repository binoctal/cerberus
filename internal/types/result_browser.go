package types

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// BrowserResult represents a browser automation result.
type BrowserResult struct {
	OK         bool   `json:"success"`
	URL        string `json:"url"`
	Title      string `json:"title"`
	Text       string `json:"text,omitempty"`
	Screenshot string `json:"screenshot,omitempty"` // base64 encoded
	EvalResult string `json:"eval_result,omitempty"`
	// Assertion facts (browser_expect): expected vs observed, judged by the
	// executor; the Examiner reviews why, not whether.
	Selector    string        `json:"selector,omitempty"`
	Expectation string        `json:"expectation,omitempty"`
	Observed    string        `json:"observed,omitempty"`
	Latency     time.Duration `json:"duration"`
	Err         string        `json:"error,omitempty"`
}

func (r BrowserResult) Success() bool           { return r.OK }
func (r BrowserResult) Duration() time.Duration { return r.Latency }
func (r BrowserResult) Summary() string {
	if !r.OK && r.Expectation != "" {
		// An expect that exhausted its window is not a transport error —
		// name the missing thing so the log line alone tells the story
		// (run30's #23 chase: "browser error <url> (30s)" hid a text that
		// simply never appeared).
		reason := r.Err
		if reason == "" {
			reason = "not found within timeout"
		}
		return fmt.Sprintf("browser expect failed %s %s: %s (%s)", r.Expectation, r.Selector, reason, r.Latency)
	}
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

// EvaluateBrowserExpectation judges a comparator against one observation.
// Polarity (spec amendment A2): text_present passes on a hit within the
// window; text_absent passes only when the element NEVER appeared — the
// executor polls the whole window and fails fast on appearance. Pure; the
// executor supplies text ("" = element not found) and the locator count.
func EvaluateBrowserExpectation(comparator, observedText string, count int) (bool, string, error) {
	switch comparator {
	case "text_present":
		return observedText != "", observedText, nil
	case "text_absent":
		return observedText == "", observedText, nil
	case "element_visible":
		return count > 0, "", nil
	default:
		if strings.HasPrefix(comparator, "element_count>=") {
			n, err := strconv.Atoi(strings.TrimPrefix(comparator, "element_count>="))
			if err != nil || n < 0 {
				return false, "", fmt.Errorf("bad count comparator %q", comparator)
			}
			return count >= n, fmt.Sprintf("%d", count), nil
		}
		return false, "", fmt.Errorf("unknown comparator %q", comparator)
	}
}
