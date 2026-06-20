package agent

import (
	"strings"

	"go.uber.org/zap"

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
	action := matchKeyword(lower, defaultTarget)
	// An HTTP action only makes sense against a URL target. For non-URL
	// targets, pick a local executor matching the target's shape so the case
	// proceeds (file_read for files, file_glob for dirs, process_exec for
	// commands) instead of failing to connect.
	if _, ok := action.(types.HTTPAction); ok {
		if alt := localActionFor(defaultTarget); alt != nil {
			return alt
		}
	}
	return action
}

// looksLikeURLTarget reports whether a target looks like an HTTP target:
// an absolute URL (http/https) or a server-relative path ("/api/...").
func looksLikeURLTarget(target string) bool {
	return strings.HasPrefix(target, "http://") ||
		strings.HasPrefix(target, "https://") ||
		strings.HasPrefix(target, "/")
}

// localActionFor returns a local-execution action matching the target's shape,
// or nil if the target is a URL (the caller keeps the HTTP action). Shapes:
//   - command (contains spaces, e.g. "go test ./..."): ProcessExecAction
//   - file path (has a file extension, e.g. "pkg/file.go"): FileReadAction
//   - directory (no extension, e.g. "internal/llm"): FileGlobAction
func localActionFor(target string) types.TypedAction {
	if looksLikeURLTarget(target) {
		return nil
	}
	if strings.Contains(target, " ") {
		parts := strings.Fields(target)
		return types.ProcessExecAction{Command: parts[0], Args: parts[1:]}
	}
	if hasFileExtension(target) {
		return types.FileReadAction{Path: target}
	}
	return types.FileGlobAction{Pattern: strings.TrimRight(target, "/") + "/**", Path: "."}
}

// hasFileExtension reports whether the final path segment looks like a file
// (contains a dot that is not a leading dot, e.g. "client.go" but not "llm").
func hasFileExtension(target string) bool {
	base := target
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	return strings.IndexByte(base, '.') > 0
}

// actionFromEnvelope parses an LLM action envelope into a concrete action,
// falling back to a safe default whenever the envelope cannot be turned into a
// valid action instead of hard-failing. Centralizes the parse-and-fallback
// logic shared by steer and recovery so the two cannot drift.
//
// The envelope is always LLM-sourced here, so any UnmarshalAction failure is
// treated as fallback-eligible: malformed/empty payloads (parse errors) AND
// payloads that parse but fail the action's Validate (e.g. a known type with
// missing required fields, common with non-Claude models). Aborting the case
// on either would skip the target outright, which is worse than retrying with
// a safe default.
func actionFromEnvelope(envelope types.ActionEnvelope, target string, logger *zap.Logger) (types.TypedAction, error) {
	action, err := types.UnmarshalAction(envelope)
	if err != nil {
		logger.Warn("action unusable, using fallback", zap.Error(err))
		return FallbackParseAction(err.Error(), target), nil
	}
	return action, nil
}
