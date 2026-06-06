package agent

import (
	"strings"
)

// actionKeywords maps recognized keywords to ActionType.
var actionKeywords = map[string]ActionType{
	"click":      ActionClick,
	"type":       ActionInput,
	"navigate":   ActionNavigate,
	"api_request": ActionAPIRequest,
	"wait":       ActionWait,
	"get":        ActionAPIRequest,
	"post":       ActionAPIRequest,
	"put":        ActionAPIRequest,
	"delete":     ActionAPIRequest,
	"patch":      ActionAPIRequest,
}

// FallbackParseAction attempts to extract a usable Action from raw LLM output
// when JSON parsing fails. It scans for action-type keywords and constructs a
// minimal Action to prevent the ReAct loop from stalling.
func FallbackParseAction(raw string, defaultTarget string) Action {
	// Take the first non-empty line.
	var firstLine string
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			firstLine = trimmed
			break
		}
	}
	if firstLine == "" {
		return Action{Type: ActionWait, Target: defaultTarget, Value: "1s"}
	}

	lower := strings.ToLower(firstLine)
	for keyword, actionType := range actionKeywords {
		if strings.Contains(lower, keyword) {
			return Action{Type: actionType, Target: defaultTarget}
		}
	}

	// No keyword found — safe default: wait 1 second.
	return Action{Type: ActionWait, Target: defaultTarget, Value: "1s"}
}
