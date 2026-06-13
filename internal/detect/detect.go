// Package detect resolves which CLI hosts cerberus (Claude Code today; Codex /
// Gemini CLI later). The CLI identity is the source of truth for the LLM
// provider: Claude Code always speaks the Anthropic protocol, regardless of the
// model name configured underneath.
package detect

import "os"

// CLI identifies the host command-line environment.
type CLI string

const (
	CLIClaudeCode CLI = "claude-code"
	CLIUnknown    CLI = "unknown"
)

// Profile bundles everything a detected CLI deterministically implies.
type Profile struct {
	CLI          CLI
	Provider     string // "anthropic" | "openai" | "gemini"
	SettingsFile string // e.g. ".claude/settings.json"
	EnvPrefix    string // "ANTHROPIC" | "OPENAI" | "GEMINI"
}

// Detector recognizes one CLI. Detect returns the profile and true when this
// detector's CLI is the active host.
type Detector interface {
	Detect() (Profile, bool)
}

// ClaudeCodeDetector recognizes Claude Code via the CLAUDECODE env var that
// Claude Code injects into every subprocess. Single signal, cross-platform —
// no process-tree walking.
type ClaudeCodeDetector struct{}

func (ClaudeCodeDetector) Detect() (Profile, bool) {
	if os.Getenv("CLAUDECODE") != "" {
		return Profile{
			CLI:          CLIClaudeCode,
			Provider:     "anthropic",
			SettingsFile: ".claude/settings.json",
			EnvPrefix:    "ANTHROPIC",
		}, true
	}
	return Profile{CLI: CLIUnknown}, false
}

// detectors is the ordered registry. First hit wins. Append new detectors here
// to support additional CLIs; no other file needs to change.
var detectors = []Detector{
	ClaudeCodeDetector{},
}

// Detect runs the detector registry and returns the first hit, or an unknown
// profile when no detector matches.
func Detect() Profile {
	for _, d := range detectors {
		if p, ok := d.Detect(); ok {
			return p
		}
	}
	return Profile{CLI: CLIUnknown}
}
