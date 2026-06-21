package types

import "strings"

// environmentalSignals are summary substrings that indicate a transport-level
// failure (target unreachable) rather than a strategy/assertion failure.
var environmentalSignals = []string{
	"http 0", "connection refused", "no such host",
	"connection reset", "unreachable",
}

// IsEnvironmentalFailure reports whether an executor result represents an
// environmental failure — the target could not be reached (service down,
// connection refused, DNS failure). Such failures are not evidence that a
// recalled strategy was bad, so they must be excluded from effectiveness
// attribution.
//
// It checks the precise HTTP status-0 case first (the common connection-refused
// shape, where StatusCode is 0 and the error reason lives in the Err field, not
// the summary), then falls back to scanning the result summary for transport
// signals so non-HTTP executors that surface connection errors in their summary
// are also caught.
func IsEnvironmentalFailure(r ExecutorResult) bool {
	if r == nil {
		return false
	}
	if h, ok := r.(HTTPResult); ok && h.StatusCode == 0 {
		return true
	}
	msg := strings.ToLower(r.Summary())
	for _, sig := range environmentalSignals {
		if strings.Contains(msg, sig) {
			return true
		}
	}
	return false
}
