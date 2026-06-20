package memory

import (
	"regexp"
	"strings"
)

var (
	numPathRE  = regexp.MustCompile(`/\d+`)
	hexPathRE  = regexp.MustCompile(`/[0-9a-f]{8,}`)
	trailingRE = regexp.MustCompile(`/+$`)
	wsRE       = regexp.MustCompile(`\s+`)
)

// NormalizeTarget canonicalizes a test target so the episodic write key and
// the episodic read key (endpoint.Path) agree. Path-only: method is dropped.
func NormalizeTarget(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.IndexByte(s, '?'); i >= 0 {
		s = s[:i] // strip query string
	}
	s = numPathRE.ReplaceAllString(s, "/{id}")
	s = hexPathRE.ReplaceAllString(s, "/{id}")
	s = trailingRE.ReplaceAllString(s, "")
	if s == "" {
		s = "/"
	}
	return s
}

// NormalizeCondition canonicalizes an L3 reflection condition so LLM phrasing
// variance collapses across sessions (enables upsert dedup + consistent embedding).
func NormalizeCondition(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = wsRE.ReplaceAllString(s, " ")
	s = strings.Trim(s, ".;,:!?")
	return s
}
