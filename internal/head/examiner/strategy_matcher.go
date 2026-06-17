package examiner

import (
	"context"
	"fmt"
	"strings"

	"github.com/binoctal/cerberus/internal/store"
)

const (
	maxFailureStrategies = 2 // Max failure reflections to inject per match
	maxSuccessStrategies = 1 // Max success reflections to inject per match
)

// MatchStrategies scans L3 procedural memory and returns strategies matching
// the current operation target. Deterministic, zero tokens.
// currentTarget format: "<METHOD> <PATH> [returned <STATUS>]"
func MatchStrategies(ctx context.Context, s *store.Store, currentTarget string) ([]store.ProceduralMemory, error) {
	all, err := s.GetProceduralByEffectiveness(ctx, 0.2, 30)
	if err != nil {
		return nil, fmt.Errorf("get procedural: %w", err)
	}

	var failures, successes []store.ProceduralMemory
	for _, r := range all {
		if matchesPattern(r.Condition, currentTarget) {
			if r.Type == "success" {
				successes = append(successes, r)
			} else {
				failures = append(failures, r)
			}
		}
	}

	// Apply limits.
	if len(failures) > maxFailureStrategies {
		failures = failures[:maxFailureStrategies]
	}
	if len(successes) > maxSuccessStrategies {
		successes = successes[:maxSuccessStrategies]
	}

	return append(failures, successes...), nil
}

// matchesPattern checks if a condition pattern matches a target string.
// Supports * as wildcard (matches any substring) and simple glob matching.
// Falls back to substring matching for patterns without glob characters.
func matchesPattern(pattern, target string) bool {
	if pattern == "*" || pattern == "" {
		return false // Don't match universal patterns — too noisy.
	}

	// If no glob characters, use simple substring match.
	if !strings.Contains(pattern, "*") && !strings.Contains(pattern, "?") {
		return strings.Contains(target, pattern)
	}

	// Simple glob matching: split pattern by * and check sequential containment.
	return globMatch(pattern, target)
}

// globMatch implements simple glob matching with * and ? wildcards.
func globMatch(pattern, target string) bool {
	// Handle ? by expanding each ? to match exactly one character.
	// We process the pattern character by character.
	return globMatchRecursive(pattern, target, 0, 0)
}

func globMatchRecursive(pattern, target string, pi, ti int) bool {
	for pi < len(pattern) && ti < len(target) {
		switch pattern[pi] {
		case '*':
			// Handle * wildcard: match 0..N characters
			if matchWildcard(pattern, target, pi, ti) {
				return true
			}
			return false
		case '?':
			// Match exactly one character
			pi, ti = matchQuestion(pattern, target, pi, ti)
		default:
			// Match literal character
			matched, newPi, newTi := matchLiteral(pattern, target, pi, ti)
			if !matched {
				return false
			}
			pi, ti = newPi, newTi
		}
	}

	// Skip trailing *s in pattern
	pi = skipTrailingWildcards(pattern, pi)

	return isCompleteMatch(pattern, target, pi, ti)
}

// FormatStrategiesForPrompt formats matched strategies for injection into prompts.
func FormatStrategiesForPrompt(strategies []store.ProceduralMemory) string {
	if len(strategies) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Learned Strategies (matched to current context)\n")
	for _, s := range strategies {
		typeLabel := "failure"
		if s.Type == "success" {
			typeLabel = "success"
		}
		fmt.Fprintf(&b, "- [%s] When %s: %s (effectiveness: %.0f%%)\n",
			typeLabel, s.Condition, s.Action, s.Effectiveness*100)
	}
	return b.String()
}
