package agent

import (
	"strings"

	"github.com/binoctal/cerberus/internal/types"
)

// extractFirstLine extracts the first meaningful line from cleaned error output
func extractFirstLine(raw string) string {
	// Clean up the raw input by extracting only the relevant error message
	cleaned := raw
	if idx := strings.Index(raw, "\nraw:"); idx != -1 {
		// Only use the error message part before the raw response dump
		cleaned = raw[:idx]
	}

	// Take the first non-empty line from the cleaned error message
	for _, line := range strings.Split(cleaned, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "parse output:") {
			return trimmed
		}
	}

	// If no useful line found, try a different approach
	return extractMeaningfulContent(cleaned)
}

// extractMeaningfulContent looks for action intent in the error message
func extractMeaningfulContent(cleaned string) string {
	lines := strings.Split(cleaned, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip empty lines and error prefixes
		if trimmed != "" && !strings.HasPrefix(trimmed, "parse output:") && !strings.HasPrefix(trimmed, "unmarshal") {
			return trimmed
		}
	}
	return ""
}

// matchKeyword searches for action keywords in the input string
func matchKeyword(lower string, defaultTarget string) types.TypedAction {
	for keyword, fb := range fallbackKeywords {
		if strings.Contains(lower, keyword) {
			return fb.makeAction(defaultTarget)
		}
	}
	// No keyword found — safe default
	return types.WaitAction{Duration: "1s"}
}
