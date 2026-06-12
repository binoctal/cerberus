package agent

import (
	"strings"

	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/types"
)

// RuleEngine matches test cases to deterministic actions (zero tokens).
type RuleEngine struct {
	baseURL string
	actors  []project.Actor
	workDir string
}

// NewRuleEngine creates a rule engine for the given base URL, actors, and workDir.
// workDir is used as the working directory for process and code actions.
func NewRuleEngine(baseURL string, actors []project.Actor, workDir string) *RuleEngine {
	return &RuleEngine{
		baseURL: strings.TrimRight(baseURL, "/"),
		actors:  actors,
		workDir: workDir,
	}
}

// Match attempts to produce a deterministic TypedAction for the given TestCase.
// Returns the action and true if matched, nil and false otherwise.
func (r *RuleEngine) Match(tc TestCase) (types.TypedAction, bool) {
	// Rule 1: API test — method is set and target is a path.
	if tc.Method != "" && strings.HasPrefix(tc.Target, "/") {
		action := types.HTTPAction{
			Method: strings.ToUpper(tc.Method),
			URL:    r.baseURL + tc.Target,
		}
		if len(r.actors) > 0 {
			action.Headers = r.authHeaders()
		}
		return action, true
	}

	// Rule 2: Navigate action with a path target.
	if tc.Action == "navigate" && strings.HasPrefix(tc.Target, "/") {
		return types.NavigateAction{URL: r.baseURL + tc.Target}, true
	}

	// Rule 3: Target is a full URL.
	if isURL(tc.Target) {
		if tc.Method != "" {
			return types.HTTPAction{
				Method: strings.ToUpper(tc.Method),
				URL:    tc.Target,
			}, true
		}
		return types.NavigateAction{URL: tc.Target}, true
	}

	// Rule 4: process_exec — target is the command to run.
	if tc.Action == "process_exec" {
		return types.ProcessExecAction{
			Command: tc.Target,
			WorkDir: r.workDir,
		}, true
	}

	// Rule 5: process_build — target is the build command.
	if tc.Action == "process_build" {
		return types.ProcessExecAction{
			Command: tc.Target,
			WorkDir: r.workDir,
		}, true
	}

	// Rule 6: code_analyze — target is the path to analyze.
	if tc.Action == "code_analyze" {
		return types.CodeAnalyzeAction{TargetPath: r.workDir}, true
	}

	// Rule 7: code_lint — target is the path to lint.
	if tc.Action == "code_lint" {
		return types.CodeLintAction{TargetPath: r.workDir}, true
	}

	// Rule 8: code_symbols — target is the path for symbol inventory.
	if tc.Action == "code_symbols" {
		return types.CodeSymbolsAction{TargetPath: r.workDir}, true
	}

	// Rule 9: file_read/write/exists/glob — target is the file path or pattern.
	if tc.Action == "file_read" {
		return types.FileReadAction{Path: tc.Target}, true
	}
	if tc.Action == "file_write" {
		return types.FileWriteAction{Path: tc.Target}, true
	}
	if tc.Action == "file_exists" {
		return types.FileExistsAction{Path: tc.Target}, true
	}
	if tc.Action == "file_glob" {
		return types.FileGlobAction{Pattern: tc.Target}, true
	}

	// Rule 10: wait — target is the duration string.
	if tc.Action == "wait" {
		return types.WaitAction{Duration: tc.Target}, true
	}

	// Rule 11: mcp_call — target is the method, server derived from context.
	if tc.Action == "mcp_call" {
		return types.MCPCallAction{
			Server: tc.Target,
			Method: tc.Method,
		}, true
	}

	// No rule matches — AI Steer needed.
	return nil, false
}

// authHeaders returns basic auth headers from the first configured actor.
func (r *RuleEngine) authHeaders() map[string]string {
	if len(r.actors) == 0 {
		return nil
	}
	actor := r.actors[0]
	if actor.Credentials.Email == "" {
		return nil
	}
	return map[string]string{
		"X-Test-User": actor.Credentials.Email,
	}
}

func isURL(s string) bool {
	return strings.Contains(s, "://")
}
