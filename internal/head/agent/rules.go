package agent

import (
	"strings"
	"sync/atomic"

	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/types"
)

// RuleEngine matches test cases to deterministic actions (zero tokens).
type RuleEngine struct {
	baseURL string
	actors  []project.Actor
	workDir string
	hits    atomic.Int64
	misses  atomic.Int64
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
	action, matched := r.matchRules(tc)
	if matched {
		r.hits.Add(1)
	} else {
		r.misses.Add(1)
	}
	return action, matched
}

// Stats returns the cumulative hit and miss counts for observability.
func (r *RuleEngine) Stats() (hits, misses int64) {
	return r.hits.Load(), r.misses.Load()
}

// matchRules contains the actual rule matching logic.
func (r *RuleEngine) matchRules(tc TestCase) (types.TypedAction, bool) {
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
		return types.BuildAction{
			ProcessExecAction: types.ProcessExecAction{
				Command: tc.Target,
				WorkDir: r.workDir,
			},
		}, true
	}

	// Rule 6: code_analyze — target is the path to analyze.
	if tc.Action == "code_analyze" {
		return types.CodeAnalyzeAction{TargetPath: r.workDir, Language: tc.Language}, true
	}

	// Rule 7: code_lint — target is the path to lint.
	if tc.Action == "code_lint" {
		return types.CodeLintAction{TargetPath: r.workDir, Language: tc.Language}, true
	}

	// Rule 8: code_symbols — target is the path for symbol inventory.
	if tc.Action == "code_symbols" {
		return types.CodeSymbolsAction{TargetPath: r.workDir, Language: tc.Language}, true
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

	// Rule 12: browser_goto — target is the URL to navigate.
	if tc.Action == "browser_goto" {
		url := tc.Target
		if !isURL(url) {
			url = r.baseURL + url
		}
		return types.BrowserGotoAction{URL: url}, true
	}

	// Rule 13: browser_click — target is the CSS selector.
	if tc.Action == "browser_click" {
		return types.BrowserClickAction{Selector: tc.Target}, true
	}

	// Rule 14: browser_fill — target is the selector, expectation holds the value.
	if tc.Action == "browser_fill" {
		return types.BrowserFillAction{Selector: tc.Target, Value: tc.Expectation}, true
	}

	// Rule 15: browser_eval — target is the JS expression.
	if tc.Action == "browser_eval" {
		return types.BrowserEvalAction{Expression: tc.Target}, true
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
