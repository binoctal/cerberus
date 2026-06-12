package sandbox

import "go.uber.org/zap"

// TryNewLinuxSandbox attempts to create a Linux sandbox with runtime capability detection.
// Returns nil if the sandbox is unavailable (non-Linux OS or insufficient privileges).
func TryNewLinuxSandbox(logger *zap.Logger) Sandbox {
	sb := NewLinuxSandbox(logger)
	if !sb.IsAvailable() {
		return nil
	}
	return sb
}
