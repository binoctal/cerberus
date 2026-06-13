package detect

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClaudeCodeDetector_Hit(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	p, ok := ClaudeCodeDetector{}.Detect()
	assert.True(t, ok)
	assert.Equal(t, CLIClaudeCode, p.CLI)
	assert.Equal(t, "anthropic", p.Provider)
	assert.Equal(t, "ANTHROPIC", p.EnvPrefix)
}

func TestClaudeCodeDetector_Miss(t *testing.T) {
	t.Setenv("CLAUDECODE", "")
	p, ok := ClaudeCodeDetector{}.Detect()
	assert.False(t, ok)
	assert.Equal(t, CLIUnknown, p.CLI)
}

func TestDetect_RegistryFirstHitWins(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	p := Detect()
	assert.Equal(t, CLIClaudeCode, p.CLI)
}

func TestDetect_AllMiss_ReturnsUnknown(t *testing.T) {
	t.Setenv("CLAUDECODE", "")
	p := Detect()
	assert.Equal(t, CLIUnknown, p.CLI)
}
