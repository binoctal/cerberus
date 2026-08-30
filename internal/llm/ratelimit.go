package llm

import (
	"fmt"
	"regexp"
	"time"
)

// resetTimePattern matches the provider-rendered window reset embedded in
// quota-exhausted bodies, e.g. anthropic-proxy code 1308:
// "[已达到 5 小时的使用上限。您的限额将在 2026-08-30 13:00:23 重置。]".
// The timestamp is interpreted in the local timezone; a wrong guess only
// shortens or lengthens the caller's bounded wait, never correctness.
var resetTimePattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}`)

// RateLimitError marks provider-side quota exhaustion (HTTP 429). ResetAt is
// the provider-advertised window reset when the body carries a timestamp
// (zero otherwise). It keeps the plain "<provider> error <code>: <body>" text
// so log output is unchanged; use errors.As to detect it through wrapped
// chains.
type RateLimitError struct {
	Provider string    // error-text label: "anthropic api" / "anthropic stream"
	Status   int       // HTTP status (429)
	Body     string    // raw response body
	ResetAt  time.Time // parsed window reset, zero when unknown
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("%s error %d: %s", e.Provider, e.Status, e.Body)
}

// NewRateLimitError builds the typed error and parses the reset timestamp.
func NewRateLimitError(provider string, status int, body string) *RateLimitError {
	rle := &RateLimitError{Provider: provider, Status: status, Body: body}
	if m := resetTimePattern.FindString(body); m != "" {
		if t, err := time.ParseInLocation("2006-01-02 15:04:05", m, time.Local); err == nil {
			rle.ResetAt = t
		}
	}
	return rle
}
