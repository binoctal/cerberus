package agent

import (
	"github.com/binoctal/cerberus/internal/types"
)

// matchProcessRules matches process execution and build actions
func (r *RuleEngine) matchProcessRules(tc TestCase) (types.TypedAction, bool) {
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

	return nil, false
}
