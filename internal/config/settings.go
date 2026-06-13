package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// claudeCodeSettings is the subset of .claude/settings.json that cerberus reads.
// Cerberus deep-integrates with Claude Code and reuses its LLM configuration
// (base URL, auth token, default model) so a project works with zero extra setup.
type claudeCodeSettings struct {
	Env map[string]string `json:"env"`
}

// loadClaudeCodeEnv reads .claude/settings.json (searched upward from the
// process working directory) and returns its env map. Returns nil when the file
// is missing or unreadable, so callers can fall back to environment defaults.
func loadClaudeCodeEnv() map[string]string {
	if os.Getenv("CERBERUS_NO_CLAUDE_SETTINGS") != "" {
		return nil // opt out of Claude Code config reuse
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	return loadClaudeCodeEnvFrom(cwd)
}

// loadClaudeCodeEnvFrom is the testable form: it searches upward from dir.
func loadClaudeCodeEnvFrom(dir string) map[string]string {
	path := findUp(dir, ".claude"+string(filepath.Separator)+"settings.json")
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var s claudeCodeSettings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil
	}
	return s.Env
}

// findUp searches for target starting at dir and walking upward to the
// filesystem root. Returns the first existing file path, or "" if not found.
func findUp(dir, target string) string {
	dir = filepath.Clean(dir)
	for {
		p := filepath.Join(dir, target)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "" // reached filesystem root
		}
		dir = parent
	}
}
