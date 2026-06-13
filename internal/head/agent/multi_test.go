package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/sandbox"
	"github.com/binoctal/cerberus/internal/types"
)

// TestSandboxPolicyFor_BuildActionNoPanic is a regression test: sandboxPolicyFor
// previously asserted every process action as ProcessExecAction, so a BuildAction
// (which embeds but is not ProcessExecAction) triggered an interface-conversion
// panic. Both action types must now resolve a process sandbox policy without panic.
func TestSandboxPolicyFor_BuildActionNoPanic(t *testing.T) {
	m := &MultiExecutor{}
	want := sandbox.DefaultProcessPolicy("/work")

	buildPolicy := m.sandboxPolicyFor(types.BuildAction{
		ProcessExecAction: types.ProcessExecAction{WorkDir: "/work"},
	})
	assert.Equal(t, want, buildPolicy)

	execPolicy := m.sandboxPolicyFor(types.ProcessExecAction{WorkDir: "/work"})
	assert.Equal(t, want, execPolicy, "build and exec actions sharing a WorkDir should map to the same policy")
}
