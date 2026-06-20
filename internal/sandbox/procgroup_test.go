//go:build unix

package sandbox

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestNoOpSandbox_KillsGrandchildOnTimeout verifies the process-group fix: when
// a command forks a long-running grandchild (`sh -c 'sleep 30'` — sh is the
// child, sleep is the grandchild), cancelling the context must reap the whole
// tree. exec.CommandContext's default cancel kills only the direct child, so
// without the Setpgid + group-kill in configureProcessGroup the orphaned sleep
// keeps the stdout pipe open and ExecCommand blocks ~30s instead of returning
// at the deadline.
func TestNoOpSandbox_KillsGrandchildOnTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, _, _, _ = NoOpSandbox{}.ExecCommand(ctx, "sh", []string{"-c", "sleep 30"}, nil, "", Policy{})
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 5*time.Second,
		"grandchild must be killed with the process group; ExecCommand returned after %v, "+
			"meaning the orphaned grandchild kept the pipe open (process-group kill not working)", elapsed)
}
