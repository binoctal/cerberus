package ai

import (
	"regexp"
	"strconv"
)

// apiStatusRE matches the "<provider> api error <status>" prefix all LLM
// clients emit, so retry decisions use the status code instead of the
// (arbitrary, body-influenced) response text.
var apiStatusRE = regexp.MustCompile(`api error (\d{3})`)

// bareStatusRE matches a bare 429 or 5xx at word boundaries, for errors that
// carry a status but not the "api error" prefix (e.g. "503 service unavailable"
// from the net layer). Word boundaries avoid matching digits inside bodies or
// identifiers like "request_id:50012".
var bareStatusRE = regexp.MustCompile(`\b(429|5\d{2})\b`)

// isRetryable determines if an LLM error is transient and worth retrying.
//
// Priority:
//  1. Explicit "<provider> api error NNN:" — decide by status alone; a 4xx body
//     that mentions transient words ("500", "timeout", "capacity") is NOT retried.
//  2. Bare status without the prefix (net-layer form) — 429/5xx retry.
//  3. Network / overload signals without a status — substring fallback.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()

	if m := apiStatusRE.FindStringSubmatch(msg); m != nil {
		code, _ := strconv.Atoi(m[1])
		return code == 429 || code >= 500
	}

	if bareStatusRE.MatchString(msg) {
		return true
	}

	return containsAny(msg,
		"rate limit", "rate_limit", "too many requests",
		"timeout", "connection refused", "connection reset", "temporary failure", "deadline exceeded",
		"overloaded", "capacity", "server error", "internal server error",
	)
}

// containsAny checks if a string contains any of the given substrings.
func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if len(s) >= len(sub) {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}
