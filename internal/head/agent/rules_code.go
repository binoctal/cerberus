package agent

import (
	"github.com/binoctal/cerberus/internal/types"
)

// matchCodeRules matches code analysis actions
func (r *RuleEngine) matchCodeRules(tc TestCase) (types.TypedAction, bool) {
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

	return nil, false
}
