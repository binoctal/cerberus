package examiner

// skipWildcards skips consecutive * characters in pattern.
func skipWildcards(pattern string, pi int) int {
	for pi < len(pattern) && pattern[pi] == '*' {
		pi++
	}
	return pi
}

// matchWildcard attempts to match * wildcard with 0..N characters.
// Returns true if match succeeds.
func matchWildcard(pattern, target string, pi, ti int) bool {
	// Skip consecutive *s
	pi = skipWildcards(pattern, pi)
	if pi >= len(pattern) {
		return true // Trailing * matches everything
	}

	// Try matching * with 0..N characters
	for ti <= len(target) {
		if globMatchRecursive(pattern, target, pi, ti) {
			return true
		}
		ti++
	}
	return false
}

// matchQuestion matches ? wildcard with exactly one character.
// Returns updated indices.
func matchQuestion(pattern, target string, pi, ti int) (int, int) {
	return pi + 1, ti + 1
}

// matchLiteral matches literal characters.
// Returns true if match succeeds, along with updated indices.
func matchLiteral(pattern, target string, pi, ti int) (bool, int, int) {
	if pattern[pi] != target[ti] {
		return false, pi, ti
	}
	return true, pi + 1, ti + 1
}

// skipTrailingWildcards skips * characters at end of pattern.
func skipTrailingWildcards(pattern string, pi int) int {
	for pi < len(pattern) && pattern[pi] == '*' {
		pi++
	}
	return pi
}

// isCompleteMatch checks if we've consumed all pattern and target.
func isCompleteMatch(pattern, target string, pi, ti int) bool {
	return pi == len(pattern) && ti == len(target)
}
