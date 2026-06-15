package agent

import (
	"strings"

	"github.com/binoctal/cerberus/internal/types"
)

// fallbackKeyword maps recognized keywords to action constructors.
type fallbackKeyword struct {
	makeAction func(target string) types.TypedAction
}

var fallbackKeywords = map[string]fallbackKeyword{
	"navigate":    {func(t string) types.TypedAction { return types.NavigateAction{URL: t} }},
	"api_request": {func(t string) types.TypedAction { return types.HTTPAction{Method: "GET", URL: t} }},
	"wait":        {func(t string) types.TypedAction { return types.WaitAction{Duration: "1s"} }},
	"get":         {func(t string) types.TypedAction { return types.HTTPAction{Method: "GET", URL: t} }},
	"post":        {func(t string) types.TypedAction { return types.HTTPAction{Method: "POST", URL: t} }},
	"put":         {func(t string) types.TypedAction { return types.HTTPAction{Method: "PUT", URL: t} }},
	"delete":      {func(t string) types.TypedAction { return types.HTTPAction{Method: "DELETE", URL: t} }},
	"patch":       {func(t string) types.TypedAction { return types.HTTPAction{Method: "PATCH", URL: t} }},
	"click":       {func(t string) types.TypedAction { return types.BrowserClickAction{Selector: t} }},
	"type":        {func(t string) types.TypedAction { return types.BrowserFillAction{Selector: t, Value: ""} }},
	"fill":        {func(t string) types.TypedAction { return types.BrowserFillAction{Selector: t, Value: ""} }},
	"goto":        {func(t string) types.TypedAction { return types.NavigateAction{URL: t} }},
}

// FallbackParseAction attempts to extract a usable TypedAction from raw LLM output
// when JSON parsing fails. It scans for action-type keywords and constructs a
// minimal action to prevent the ReAct loop from stalling.
func FallbackParseAction(raw string, defaultTarget string) types.TypedAction {
	// Clean up the raw input by extracting only the relevant error message
	// Error format from driver: "parse output: <error>\nraw: <full response>"
	cleaned := raw
	if idx := strings.Index(raw, "\nraw:"); idx != -1 {
		// Only use the error message part before the raw response dump
		cleaned = raw[:idx]
	}

	// Take the first non-empty line from the cleaned error message.
	var firstLine string
	for _, line := range strings.Split(cleaned, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "parse output:") {
			firstLine = trimmed
			break
		}
	}
	// If no useful line found, try a different approach: extract meaningful content
	if firstLine == "" || firstLine == "parse output:" {
		// Look for action intent in the error message
		lines := strings.Split(cleaned, "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			// Skip empty lines and error prefixes
			if trimmed != "" && !strings.HasPrefix(trimmed, "parse output:") && !strings.HasPrefix(trimmed, "unmarshal") {
				firstLine = trimmed
				break
			}
		}
	}

	if firstLine == "" {
		// Still no useful content - safe default
		return types.WaitAction{Duration: "1s"}
	}

	lower := strings.ToLower(firstLine)
	for keyword, fb := range fallbackKeywords {
		if strings.Contains(lower, keyword) {
			return fb.makeAction(defaultTarget)
		}
	}

	// No keyword found — safe default: wait 1 second to give LLM time to recover.
	return types.WaitAction{Duration: "1s"}
}
