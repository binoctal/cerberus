package agent

import (
	"github.com/binoctal/cerberus/internal/sandbox"
	"github.com/binoctal/cerberus/internal/types"
)

// sandboxPolicyFor returns the appropriate sandbox policy for an action type.
func (m *MultiExecutor) sandboxPolicyFor(action types.TypedAction) sandbox.Policy {
	switch action.GetActionType() {
	case types.ActionProcessExec, types.ActionProcessBuild:
		return m.processSandboxPolicy(action)
	case types.ActionFileRead, types.ActionFileWrite, types.ActionFileExists, types.ActionFileGlob:
		return sandbox.DefaultFilePolicy(".")
	case types.ActionMCPCall:
		return sandbox.DefaultMCPPolicy()
	case types.ActionCodeAnalyze, types.ActionCodeLint, types.ActionCodeSymbols:
		return sandbox.DefaultCodePolicy(".")
	case types.ActionBrowserGoto, types.ActionBrowserClick, types.ActionBrowserFill, types.ActionBrowserEval:
		return sandbox.DefaultBrowserPolicy()
	case types.ActionDBQuery, types.ActionDBAssert:
		return sandbox.DefaultDBPolicy()
	case types.ActionGraphQLQuery:
		return sandbox.DefaultGraphQLPolicy()
	case types.ActionWSConnect, types.ActionWSSend:
		return sandbox.DefaultWSPolicy()
	default:
		return sandbox.DefaultHTTPPolicy()
	}
}

// processSandboxPolicy returns sandbox policy for process-related actions.
func (m *MultiExecutor) processSandboxPolicy(action types.TypedAction) sandbox.Policy {
	switch a := action.(type) {
	case types.BuildAction:
		return sandbox.DefaultProcessPolicy(a.WorkDir)
	case types.ProcessExecAction:
		return sandbox.DefaultProcessPolicy(a.WorkDir)
	default:
		return sandbox.DefaultProcessPolicy(".")
	}
}
