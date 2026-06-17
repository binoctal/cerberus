package architecture

import (
	"strings"
)

// PatternMatcher matches identifiers against responsibility patterns
type PatternMatcher struct {
	patterns []Responsibility
}

// NewPatternMatcher creates a new pattern matcher
func NewPatternMatcher(patterns []Responsibility) *PatternMatcher {
	return &PatternMatcher{patterns: patterns}
}

// matchIdentifier checks if an identifier matches any responsibility pattern
// Returns the matched responsibility name and whether it matched
func (pm *PatternMatcher) matchIdentifier(identifier string) (string, bool) {
	identifierLower := strings.ToLower(identifier)

	for _, pattern := range pm.patterns {
		for _, keyword := range pattern.Keywords {
			if strings.Contains(identifierLower, keyword) {
				return pattern.Name, true
			}
		}
	}

	return "", false
}

// matchIdentifiers checks multiple identifiers and returns all unique responsibilities found
func (pm *PatternMatcher) matchIdentifiers(identifiers []string) map[string]bool {
	responsibilities := make(map[string]bool)

	for _, identifier := range identifiers {
		if respName, matched := pm.matchIdentifier(identifier); matched {
			responsibilities[respName] = true
		}
	}

	return responsibilities
}

// collectMatches finds all matching patterns for identifiers and updates pattern examples
func (pm *PatternMatcher) collectMatches(identifiers []string) map[string]bool {
	responsibilities := make(map[string]bool)

	for _, identifier := range identifiers {
		identifierLower := strings.ToLower(identifier)

		for i := range pm.patterns {
			for _, keyword := range pm.patterns[i].Keywords {
				if strings.Contains(identifierLower, keyword) {
					responsibilities[pm.patterns[i].Name] = true
					pm.patterns[i].Examples = append(pm.patterns[i].Examples, identifier)
				}
			}
		}
	}

	return responsibilities
}
