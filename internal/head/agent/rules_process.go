package agent

import (
	"strings"

	"github.com/binoctal/cerberus/internal/types"
)

// matchProcessRules matches process execution and build actions.
func (r *RuleEngine) matchProcessRules(tc TestCase) (types.TypedAction, bool) {
	// Rule 4: process_exec — target is the command to run. Split it into the
	// executable (Command) and its arguments (Args) so the policy allowlist —
	// which keys on the executable name, e.g. "go" — can match. Without this
	// split the whole "go build ./..." lands in Command and policy denies it,
	// forcing every command through the ReAct fallback loop.
	if tc.Action == "process_exec" {
		action, ok := splitCommand(tc.Target, r.workDir)
		if !ok {
			return nil, false
		}
		return action, true
	}

	// Rule 5: process_build — target is the build command (same split logic).
	if tc.Action == "process_build" {
		action, ok := splitCommand(tc.Target, r.workDir)
		if !ok {
			return nil, false
		}
		return types.BuildAction{ProcessExecAction: action}, true
	}

	return nil, false
}

// splitCommand parses a command string like "go build ./..." into a
// ProcessExecAction with Command="go" and Args=["build","./..."]. An empty or
// whitespace-only target yields ok=false.
func splitCommand(target, workDir string) (types.ProcessExecAction, bool) {
	parts := strings.Fields(target)
	if len(parts) == 0 {
		return types.ProcessExecAction{}, false
	}
	return types.ProcessExecAction{
		Command: parts[0],
		Args:    parts[1:],
		WorkDir: workDir,
	}, true
}
