//go:build !unix

package sandbox

import "os/exec"

// configureProcessGroup is a no-op on platforms without Unix process groups
// (e.g. Windows). exec.CommandContext's default child-kill behavior is used.
func configureProcessGroup(c *exec.Cmd) {}
