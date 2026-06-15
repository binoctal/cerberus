package ai

import (
	"encoding/json"
	"fmt"
	"regexp"
)

var jsonBlockRE = regexp.MustCompile("(?s)```(?:json)?\\s*\\n(.*?)\\n```")

func ParseStructuredOutput(input string, target any) error {
	// Try direct JSON parsing first
	if err := json.Unmarshal([]byte(input), target); err == nil {
		return nil
	}

	// Try extracting JSON from markdown code blocks
	match := jsonBlockRE.FindStringSubmatch(input)
	if match != nil {
		if err := json.Unmarshal([]byte(match[1]), target); err == nil {
			return nil
		}
	}

	// Try extracting JSON object by finding matching braces
	start := -1
	end := -1
	braceDepth := 0
	for i, c := range input {
		if c == '{' {
			if start == -1 {
				start = i
			}
			braceDepth++
		}
		if c == '}' && braceDepth > 0 {
			braceDepth--
			if braceDepth == 0 {
				end = i
				break
			}
		}
	}
	if start != -1 && end > start {
		if err := json.Unmarshal([]byte(input[start:end+1]), target); err == nil {
			return nil
		}
	}

	// All attempts failed - return the last error with context
	return fmt.Errorf("failed to parse structured output: %w (input length: %d, starts with: %q)", json.Unmarshal([]byte(input), target), len(input), truncateString(input, 100))
}

// truncateString limits string length for error messages
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
