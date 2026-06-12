package agent

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"
)

// ManagedProcess describes a background process to be managed.
type ManagedProcess struct {
	Name    string
	Cmd     string
	Args    []string
	WorkDir string
	Health  string        // URL to poll for readiness (optional)
	Timeout time.Duration // Max wait for health endpoint

	cmd     *exec.Cmd
	running bool
}

// ProcessManager manages the lifecycle of background processes.
type ProcessManager struct {
	mu        sync.Mutex
	processes map[string]*ManagedProcess
	logger    *zap.Logger
}

// NewProcessManager creates a new process manager.
func NewProcessManager(logger *zap.Logger) *ProcessManager {
	return &ProcessManager{
		processes: make(map[string]*ManagedProcess),
		logger:    logger,
	}
}

// Start launches a managed process and optionally polls its health endpoint
// until it responds with a non-5xx status or the timeout expires.
func (pm *ProcessManager) Start(ctx context.Context, mp *ManagedProcess) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if mp.running {
		return fmt.Errorf("process %q is already running", mp.Name)
	}

	cmd := exec.CommandContext(ctx, mp.Cmd, mp.Args...)
	if mp.WorkDir != "" {
		cmd.Dir = mp.WorkDir
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start process %q: %w", mp.Name, err)
	}

	mp.cmd = cmd
	mp.running = true
	pm.processes[mp.Name] = mp

	pm.logger.Info("started managed process",
		zap.String("name", mp.Name),
		zap.String("cmd", mp.Cmd),
		zap.Int("pid", cmd.Process.Pid),
	)

	// Poll health endpoint if configured.
	if mp.Health != "" {
		if err := pm.pollHealth(mp); err != nil {
			// Health check failed — stop the process.
			_ = pm.stopLocked(mp.Name)
			return fmt.Errorf("health check failed for %q: %w", mp.Name, err)
		}
	}

	return nil
}

// Stop terminates a managed process by name.
// Sends SIGTERM, waits up to 5 seconds, then SIGKILL.
func (pm *ProcessManager) Stop(name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return pm.stopLocked(name)
}

func (pm *ProcessManager) stopLocked(name string) error {
	mp, ok := pm.processes[name]
	if !ok || !mp.running {
		return fmt.Errorf("process %q not found or not running", name)
	}

	if err := mp.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		// If SIGTERM fails, try SIGKILL directly.
		_ = mp.cmd.Process.Kill()
	} else {
		// Wait up to 5 seconds for graceful shutdown.
		done := make(chan error, 1)
		go func() {
			done <- mp.cmd.Wait()
		}()
		select {
		case <-done:
			// Process exited cleanly.
		case <-time.After(5 * time.Second):
			_ = mp.cmd.Process.Kill()
		}
	}

	mp.running = false
	delete(pm.processes, name)
	pm.logger.Info("stopped managed process", zap.String("name", name))
	return nil
}

// StopAll terminates all managed processes in reverse order.
func (pm *ProcessManager) StopAll() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Collect names in reverse insertion order isn't guaranteed by map,
	// so iterate all.
	for name := range pm.processes {
		_ = pm.stopLocked(name)
	}
}

// pollHealth polls the health URL until it returns a non-5xx response
// or the timeout expires.
func (pm *ProcessManager) pollHealth(mp *ManagedProcess) error {
	timeout := mp.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		resp, err := client.Get(mp.Health)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				pm.logger.Info("health check passed",
					zap.String("name", mp.Name),
					zap.Int("status", resp.StatusCode),
				)
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("health endpoint %s not ready after %s", mp.Health, timeout)
}
