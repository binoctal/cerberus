//go:build unix

package sandbox

import (
	"os/exec"
	"syscall"
	"time"
)

// configureProcessGroup puts the command in its own process group and overrides
// exec.Cmd.Cancel to kill that whole group on context cancellation.
//
// This matters when the command forks grandchildren (e.g. `go test` forking the
// test binary, or `npm test` forking workers). exec.CommandContext's default
// cancel kills only the direct child, leaving grandchildren orphaned and
// running — which is exactly how a runaway recursive `go test` kept spawning
// `cerberus-cover` children that outlived their parent. Killing the process
// group (-pgid) cleans up the whole tree. WaitDelay bounds the wait so a
// grandchild holding the stdout/stderr pipe can't keep Run() blocked.
func configureProcessGroup(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		if c.Process == nil {
			return nil
		}
		// Negative pid => signal the whole process group.
		return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	}
	c.WaitDelay = 5 * time.Second
}
