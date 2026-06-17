package ai

import (
	"encoding/json"
	"fmt"
)

func ParseStructuredOutput(input string, target any) error {
	// Phase 1: Try direct JSON parsing
	if tryDirectJSON(input, target) {
		return nil
	}

	// Phase 2: Try extracting JSON from markdown code blocks
	if tryMarkdownJSON(input, target) {
		return nil
	}

	// Phase 3: Try extracting JSON object by finding matching braces
	if tryExtractJSON(input, target) {
		return nil
	}

	// Phase 4: All attempts failed - return error with context
	return fmt.Errorf("failed to parse structured output: %w (input length: %d, starts with: %q)",
		json.Unmarshal([]byte(input), target), len(input), truncateString(input, 100))
}

// truncateString limits string length for error messages
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
