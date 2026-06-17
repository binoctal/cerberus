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
	// Phase 1: Extract first meaningful line from error output
	firstLine := extractFirstLine(raw)

	// Phase 2: If no useful content found, return safe default
	if firstLine == "" {
		return types.WaitAction{Duration: "1s"}
	}

	// Phase 3: Match keyword and construct action
	lower := strings.ToLower(firstLine)
	return matchKeyword(lower, defaultTarget)
}
