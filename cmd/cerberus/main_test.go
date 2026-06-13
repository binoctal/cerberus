package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/project"
)

// --- containsLine() tests ---

func TestContainsLine(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		line     string
		expected bool
	}{
		{"found", "foo\nbar\nbaz", "bar", true},
		{"not found", "foo\nbar", "baz", false},
		{"empty content", "", "foo", false},
		{"empty line", "foo\n\nbar", "", true},
		{"substring matches", "foobar", "foo", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, containsLine(tt.content, tt.line))
		})
	}
}

// --- loadProjectConfig() tests ---

func TestLoadProjectConfig_ValidFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "project.yaml")

	yamlContent := `project:
  name: test-project
services:
  - name: web
    url: "http://localhost:3000"
settings:
  ai_budget:
    model: "claude-sonnet-4-6"
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(yamlContent), 0644))

	logger := zap.NewNop()
	cfg := loadProjectConfig(cfgPath, "", "test goal", logger)
	require.NotNil(t, cfg)
	assert.Equal(t, "test-project", cfg.Project.Name)
	assert.Len(t, cfg.Services, 1)
	assert.Equal(t, "web", cfg.Services[0].Name)
}

func TestLoadProjectConfig_MissingFile(t *testing.T) {
	logger := zap.NewNop()
	cfg := loadProjectConfig("/nonexistent/project.yaml", "", "", logger)
	require.NotNil(t, cfg)
	// Should fall back to defaults.
	assert.NotNil(t, cfg)
}

func TestLoadProjectConfig_EmptyPath(t *testing.T) {
	logger := zap.NewNop()
	cfg := loadProjectConfig("", "", "", logger)
	require.NotNil(t, cfg)
	// Should return default config.
}

func TestLoadProjectConfig_URLInjection(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "project.yaml")

	// Config with no services.
	yamlContent := `project:
  name: test
services: []
settings: {}
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(yamlContent), 0644))

	logger := zap.NewNop()
	cfg := loadProjectConfig(cfgPath, "http://myapp.example.com", "test it", logger)
	require.NotNil(t, cfg)
	require.Len(t, cfg.Services, 1)
	assert.Equal(t, "http://myapp.example.com", cfg.Services[0].URL)
	assert.Equal(t, "default", cfg.Services[0].Name)
}

func TestLoadProjectConfig_URLNoInjectionWhenServicesExist(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "project.yaml")

	yamlContent := `project:
  name: test
services:
  - name: api
    url: "http://localhost:8080"
settings: {}
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(yamlContent), 0644))

	logger := zap.NewNop()
	cfg := loadProjectConfig(cfgPath, "http://other.example.com", "test it", logger)
	require.NotNil(t, cfg)
	// URL flag should NOT inject when services already exist.
	assert.Len(t, cfg.Services, 1)
	assert.Equal(t, "http://localhost:8080", cfg.Services[0].URL)
}

// --- Command factory tests ---

func TestInitCmd_NotNil(t *testing.T) {
	cmd := initCmd()
	require.NotNil(t, cmd)
	assert.Equal(t, "init", cmd.Use)
}

func TestRunCmd_NotNil(t *testing.T) {
	cmd := runCmd()
	require.NotNil(t, cmd)
	assert.Equal(t, "run", cmd.Use)
}

func TestVerifyCmd_NotNil(t *testing.T) {
	cmd := verifyCmd()
	require.NotNil(t, cmd)
	assert.Equal(t, "verify", cmd.Use)
}

func TestServeCmd_NotNil(t *testing.T) {
	cmd := serveCmd()
	require.NotNil(t, cmd)
	assert.Equal(t, "serve", cmd.Use)
}

func TestMCPCmd_NotNil(t *testing.T) {
	cmd := mcpCmd()
	require.NotNil(t, cmd)
	assert.Equal(t, "mcp", cmd.Use)
}

func TestReportCmd_NotNil(t *testing.T) {
	cmd := reportCmd()
	require.NotNil(t, cmd)
	assert.Equal(t, "report", cmd.Use)
}

func TestDashboardCmd_NotNil(t *testing.T) {
	cmd := dashboardCmd()
	require.NotNil(t, cmd)
	assert.Equal(t, "dashboard", cmd.Use)
}

func TestVersionCmd_NotNil(t *testing.T) {
	cmd := versionCmd()
	require.NotNil(t, cmd)
	assert.Equal(t, "version", cmd.Use)
}

// --- Version command output ---

func TestVersionCmd_Output(t *testing.T) {
	// Override package-level vars.
	origVersion, origCommit, origDate := version, commit, date
	version, commit, date = "1.0.0", "abc123", "2026-01-01"
	defer func() { version, commit, date = origVersion, origCommit, origDate }()

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "cerberus %s (commit: %s, built: %s)\n", version, commit, date)
	output := buf.String()
	assert.Contains(t, output, "cerberus 1.0.0")
	assert.Contains(t, output, "abc123")
	assert.Contains(t, output, "2026-01-01")
}

// --- init command file generation ---

func TestInitCmd_CreatesFiles(t *testing.T) {
	// Save and restore working directory.
	origDir, _ := os.Getwd()
	dir := t.TempDir()
	require.NoError(t, os.Chdir(dir))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := initCmd()
	require.NotNil(t, cmd)

	err := cmd.RunE(cmd, nil)
	require.NoError(t, err)

	// Verify files were created.
	assert.FileExists(t, ".cerberus/project.yaml")
	assert.FileExists(t, ".cerberus/credentials.yaml")

	// Verify project.yaml content.
	content, err := os.ReadFile(".cerberus/project.yaml")
	require.NoError(t, err)
	assert.Contains(t, string(content), "services:")
	assert.Contains(t, string(content), "actors:")
	assert.Contains(t, string(content), "settings:")

	// Verify credentials.yaml content.
	content, err = os.ReadFile(".cerberus/credentials.yaml")
	require.NoError(t, err)
	assert.Contains(t, string(content), "actors:")
}

func TestInitCmd_UpdatesGitignore(t *testing.T) {
	origDir, _ := os.Getwd()
	dir := t.TempDir()
	require.NoError(t, os.Chdir(dir))
	defer func() { _ = os.Chdir(origDir) }()

	err := initCmd().RunE(initCmd(), nil)
	require.NoError(t, err)

	content, err := os.ReadFile(".gitignore")
	require.NoError(t, err)
	assert.Contains(t, string(content), ".cerberus/credentials.yaml")
}

func TestInitCmd_IdempotentGitignore(t *testing.T) {
	origDir, _ := os.Getwd()
	dir := t.TempDir()
	require.NoError(t, os.Chdir(dir))
	defer func() { _ = os.Chdir(origDir) }()

	// Run init twice.
	err := initCmd().RunE(initCmd(), nil)
	require.NoError(t, err)
	err = initCmd().RunE(initCmd(), nil)
	require.NoError(t, err)

	content, err := os.ReadFile(".gitignore")
	require.NoError(t, err)
	// Should only appear once.
	assert.Equal(t, 1, strings.Count(string(content), ".cerberus/credentials.yaml"))
}

func TestInitCmd_CreatesMCPSettings(t *testing.T) {
	origDir, _ := os.Getwd()
	dir := t.TempDir()
	require.NoError(t, os.Chdir(dir))
	defer func() { _ = os.Chdir(origDir) }()

	err := initCmd().RunE(initCmd(), nil)
	require.NoError(t, err)

	assert.FileExists(t, ".claude/settings.json")
	content, err := os.ReadFile(".claude/settings.json")
	require.NoError(t, err)
	assert.Contains(t, string(content), "cerberus")
	assert.Contains(t, string(content), "mcp")
}

// --- project.Config integration with loadProjectConfig ---

func TestLoadProjectConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "project.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("{{invalid yaml"), 0644))

	logger := zap.NewNop()
	cfg := loadProjectConfig(cfgPath, "", "", logger)
	require.NotNil(t, cfg)
	// Should fall back to defaults on parse error.
}

func TestLoadProjectConfig_DefaultConfig(t *testing.T) {
	d := project.DefaultConfig()
	assert.NotNil(t, d)
	// Default config should have valid settings structure.
	assert.NotNil(t, d.Settings)
}
