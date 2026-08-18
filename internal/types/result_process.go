package types

import (
	"fmt"
	"time"
)

// ProcessResult represents a subprocess execution result.
type ProcessResult struct {
	OK       bool          `json:"success"`
	ExitCode int           `json:"exit_code"`
	Stdout   string        `json:"stdout"`
	Stderr   string        `json:"stderr"`
	Latency  time.Duration `json:"duration"`
	Err      string        `json:"error,omitempty"`
}

func (r ProcessResult) Success() bool           { return r.OK }
func (r ProcessResult) Duration() time.Duration { return r.Latency }
func (r ProcessResult) Summary() string {
	return fmt.Sprintf("exit %d (%s)\nstdout: %s", r.ExitCode, r.Latency, truncate(r.Stdout, 500))
}
func (r ProcessResult) Evidence() EvidenceData {
	return EvidenceData{Type: "process_output", Content: truncate(r.Stdout, 10000)}
}

// ProcessRestartResult is the outcome of a process_restart step: the harness
// tore down and re-launched a real-process actor, waiting for its ready
// pattern. OK false with Err set means the actor failed to come back.
type ProcessRestartResult struct {
	OK      bool          `json:"success"`
	Actor   string        `json:"actor"`
	Latency time.Duration `json:"duration"`
	Err     string        `json:"error,omitempty"`
}

func (r ProcessRestartResult) Success() bool           { return r.OK }
func (r ProcessRestartResult) Duration() time.Duration { return r.Latency }
func (r ProcessRestartResult) Summary() string {
	if r.OK {
		return "process restarted: " + r.Actor
	}
	return "process restart failed: " + r.Actor + ": " + r.Err
}
func (r ProcessRestartResult) Evidence() EvidenceData {
	return EvidenceData{Type: "process_restart", Content: r.Summary()}
}
