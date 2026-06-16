package ai

// isRetryable determines if an LLM error is transient and worth retrying.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()

	// 5xx server errors.
	if containsAny(msg, "500", "502", "503", "504", "server error", "internal server error") {
		return true
	}

	// Rate limiting.
	if containsAny(msg, "429", "rate limit", "rate_limit", "too many requests") {
		return true
	}

	// Network / timeout errors.
	if containsAny(msg, "timeout", "connection refused", "connection reset", "temporary failure", "deadline exceeded") {
		return true
	}

	// API overloaded.
	if containsAny(msg, "overloaded", "capacity") {
		return true
	}

	return false
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
