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
	"click":      {func(t string) types.TypedAction { return types.NavigateAction{URL: t} }},
	"type":       {func(t string) types.TypedAction { return types.NavigateAction{URL: t} }},
	"navigate":   {func(t string) types.TypedAction { return types.NavigateAction{URL: t} }},
	"api_request": {func(t string) types.TypedAction { return types.HTTPAction{Method: "GET", URL: t} }},
	"wait":       {func(t string) types.TypedAction { return types.WaitAction{Duration: "1s"} }},
	"get":        {func(t string) types.TypedAction { return types.HTTPAction{Method: "GET", URL: t} }},
	"post":       {func(t string) types.TypedAction { return types.HTTPAction{Method: "POST", URL: t} }},
	"put":        {func(t string) types.TypedAction { return types.HTTPAction{Method: "PUT", URL: t} }},
	"delete":     {func(t string) types.TypedAction { return types.HTTPAction{Method: "DELETE", URL: t} }},
	"patch":      {func(t string) types.TypedAction { return types.HTTPAction{Method: "PATCH", URL: t} }},
}

// FallbackParseAction attempts to extract a usable TypedAction from raw LLM output
// when JSON parsing fails. It scans for action-type keywords and constructs a
// minimal action to prevent the ReAct loop from stalling.
func FallbackParseAction(raw string, defaultTarget string) types.TypedAction {
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
		return types.WaitAction{Duration: "1s"}
	}

	lower := strings.ToLower(firstLine)
	for keyword, fb := range fallbackKeywords {
		if strings.Contains(lower, keyword) {
			return fb.makeAction(defaultTarget)
		}
	}

	// No keyword found — safe default: wait 1 second.
	return types.WaitAction{Duration: "1s"}
}
