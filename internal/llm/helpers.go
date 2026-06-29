package llm

import (
	"regexp"
	"strings"
)

// joinBaseURL appends path to base unless base already ends with path.
//
// Claude Code (and the Anthropic SDK) configure a base URL prefix such as
// ANTHROPIC_BASE_URL="https://host/api/anthropic"; the endpoint is that prefix
// plus "/v1/messages". Treating a bare prefix as the full endpoint silently
// breaks deep-integration (some providers return HTTP 200 with an error body).
//
// Examples:
//
//	joinBaseURL("https://host/api/anthropic", "/v1/messages") // "https://host/api/anthropic/v1/messages"
//	joinBaseURL("https://host/api/anthropic/v1/messages", "/v1/messages") // "https://host/api/anthropic/v1/messages"
func joinBaseURL(base, path string) string {
	if base == "" {
		return ""
	}
	trimmed := strings.TrimRight(base, "/")
	if strings.HasSuffix(trimmed, path) {
		return trimmed
	}
	return trimmed + path
}

var thinkingSuffixRe = regexp.MustCompile(`\[\d+m\]$`)

// normalizeModelID lowercases and strips the Claude Code "[Nm]" thinking-budget
// suffix so the raw model id can be sent to case-sensitive endpoints.
func normalizeModelID(model string) string {
	m := strings.ToLower(model)
	return thinkingSuffixRe.ReplaceAllString(m, "")
}
