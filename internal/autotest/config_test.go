package autotest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultNodeCoverageConfig(t *testing.T) {
	cfg := DefaultNodeCoverageConfig()
	assert.Equal(t, []string{"npm", "test"}, cfg.TestCommand)
	assert.Equal(t, "coverage/coverage-final.json", cfg.OutputPath)
	assert.Equal(t, ProjectTypeNode, cfg.ProjectType)
	assert.True(t, cfg.Timeout > 0)
	assert.Contains(t, cfg.Env, "NODE_ENV=test")
}

func TestDefaultMochaCoverageConfig(t *testing.T) {
	cfg := DefaultMochaCoverageConfig()
	assert.Equal(t, []string{"npm", "test"}, cfg.TestCommand)
	assert.Equal(t, ProjectTypeMocha, cfg.ProjectType)
	assert.True(t, cfg.Timeout > 0)
}

func TestDefaultPythonCoverageConfig(t *testing.T) {
	cfg := DefaultPythonCoverageConfig()
	assert.Contains(t, cfg.TestCommand[0], "pytest")
	assert.Equal(t, "coverage.json", cfg.OutputPath)
	assert.Equal(t, ".coverage", cfg.DatabasePath)
	assert.Equal(t, ProjectTypePython, cfg.ProjectType)
}

func TestNodeCoverageConfig_Custom(t *testing.T) {
	cfg := NodeCoverageConfig([]string{"jest"}, "out.json", 2*time.Minute)
	assert.Equal(t, []string{"jest"}, cfg.TestCommand)
	assert.Equal(t, "out.json", cfg.OutputPath)
	assert.Equal(t, 2*time.Minute, cfg.Timeout)
	assert.Equal(t, ProjectTypeNode, cfg.ProjectType)
}

func TestPythonCoverageConfig_Custom(t *testing.T) {
	cfg := PythonCoverageConfig("python3", "cov.json", 3*time.Minute)
	assert.Equal(t, "cov.json", cfg.OutputPath)
	assert.Equal(t, ".coverage", cfg.DatabasePath)
	assert.Equal(t, 3*time.Minute, cfg.Timeout)
	assert.Equal(t, ProjectTypePython, cfg.ProjectType)
}
