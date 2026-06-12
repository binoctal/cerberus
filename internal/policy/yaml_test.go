package policy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/binoctal/cerberus/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadPolicyConfig_NotExists(t *testing.T) {
	cfg, err := LoadPolicyConfig("/nonexistent/policy.yaml")
	assert.NoError(t, err)
	assert.Nil(t, cfg)
}

func TestLoadPolicyConfig_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	yamlContent := `
commands:
  allow: ["custom-tool", "rustc"]
  deny: ["go"]
paths:
  deny: ["/etc/secret", "/opt/keys"]
env:
  deny: ["AWS_SECRET_ACCESS_KEY"]
mcp:
  allow: ["custom/method"]
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "policy.yaml"), []byte(yamlContent), 0644))

	cfg, err := LoadPolicyConfig(filepath.Join(dir, "policy.yaml"))
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, []string{"custom-tool", "rustc"}, cfg.Commands.Allow)
	assert.Equal(t, []string{"go"}, cfg.Commands.Deny)
	assert.Equal(t, []string{"/etc/secret", "/opt/keys"}, cfg.Paths.Deny)
	assert.Equal(t, []string{"AWS_SECRET_ACCESS_KEY"}, cfg.Env.Deny)
	assert.Equal(t, []string{"custom/method"}, cfg.MCP.Allow)
}

func TestLoadPolicyConfig_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "policy.yaml"), []byte(""), 0644))

	cfg, err := LoadPolicyConfig(filepath.Join(dir, "policy.yaml"))
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Empty(t, cfg.Commands.Allow)
	assert.Empty(t, cfg.Commands.Deny)
}

func TestPolicyConfigApply_AddCommands(t *testing.T) {
	p := NewDefaultActionPolicy(".")

	overrides := &PolicyConfig{}
	overrides.Commands.Allow = []string{"custom-tool"}
	overrides.Apply(p)

	// Verify custom-tool is now allowed.
	err := p.Validate(types.ProcessExecAction{Command: "custom-tool", WorkDir: "."})
	assert.NoError(t, err)
}

func TestPolicyConfigApply_DenyCommands(t *testing.T) {
	p := NewDefaultActionPolicy(".")

	overrides := &PolicyConfig{}
	overrides.Commands.Deny = []string{"go"}
	overrides.Apply(p)

	// Verify go is no longer allowed.
	err := p.Validate(types.ProcessExecAction{Command: "go", WorkDir: "."})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "command not allowed: go")
}

func TestPolicyConfigApply_DeniedPaths(t *testing.T) {
	p := NewDefaultActionPolicy("/tmp/project")

	overrides := &PolicyConfig{}
	overrides.Paths.Deny = []string{"/tmp/project/secret"}
	overrides.Apply(p)

	err := p.Validate(types.FileWriteAction{Path: "/tmp/project/secret"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "path denied")
}

func TestPolicyConfigApply_NilOverrides(t *testing.T) {
	p := NewDefaultActionPolicy(".")

	var overrides *PolicyConfig
	overrides.Apply(p) // Should not panic.

	// Original defaults still work.
	err := p.Validate(types.ProcessExecAction{Command: "go", WorkDir: "."})
	assert.NoError(t, err)
}
