package prompts

import (
	"strings"
)

// isKeyLine checks if a line is a YAML key line
func isKeyLine(line, trimmed string) bool {
	return !strings.HasPrefix(trimmed, " ") && !strings.HasPrefix(trimmed, "\t") && strings.Contains(trimmed, ": |")
}

// shouldEndBlock checks if the current block should end
func shouldEndBlock(line, trimmed string, currentValueLen int) bool {
	return (!strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && trimmed != "" && currentValueLen > 0)
}

// extractKey extracts the key from a key line
func extractKey(trimmed string) string {
	return strings.TrimSpace(strings.Split(trimmed, ":")[0])
}

// processBlockContent processes a content line within a YAML block
func processBlockContent(line string, currentValue *strings.Builder) {
	content := strings.TrimPrefix(line, "  ")
	if currentValue.Len() > 0 {
		currentValue.WriteString("\n")
	}
	currentValue.WriteString(content)
}
