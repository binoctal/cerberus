package autotest

import (
	"time"
)

// CoverageConfig holds configuration for running coverage tests
type CoverageConfig struct {
	// Test command and arguments
	TestCommand []string
	CoverageArgs []string

	// Output configuration
	OutputPath   string // Coverage report output path
	DatabasePath string // For coverage.py SQLite database

	// Execution configuration
	Timeout time.Duration
	Env     []string

	// Provider-specific configuration
	ProjectType ProjectType
}

// DefaultNodeCoverageConfig returns default configuration for Node.js Jest projects
func DefaultNodeCoverageConfig() *CoverageConfig {
	return &CoverageConfig{
		TestCommand:  []string{"npm", "test"},
		CoverageArgs: []string{"--", "--coverage", "--coverageReporters=json"},
		OutputPath:   "coverage/coverage-final.json",
		Timeout:      5 * time.Minute,
		Env:          []string{"NODE_ENV=test"},
		ProjectType:  ProjectTypeNode,
	}
}

// DefaultMochaCoverageConfig returns default configuration for Node.js Mocha projects
func DefaultMochaCoverageConfig() *CoverageConfig {
	return &CoverageConfig{
		TestCommand:  []string{"npm", "test"},
		CoverageArgs: []string{"--", "--coverage", "--coverage-reporter=json"},
		OutputPath:   "coverage/coverage-final.json",
		Timeout:      5 * time.Minute,
		Env:          []string{"NODE_ENV=test"},
		ProjectType:  ProjectTypeMocha,
	}
}

// DefaultPythonCoverageConfig returns default configuration for Python pytest projects
func DefaultPythonCoverageConfig() *CoverageConfig {
	return &CoverageConfig{
		TestCommand:  []string{"pytest", "--cov", "--cov-report=term"},
		CoverageArgs: []string{"--cov-report=json"},
		OutputPath:   "coverage.json",
		DatabasePath: ".coverage",
		Timeout:      5 * time.Minute,
		Env:          nil,
		ProjectType:  ProjectTypePython,
	}
}

// NodeCoverageConfig creates custom Node configuration
func NodeCoverageConfig(testCommand []string, outputPath string, timeout time.Duration) *CoverageConfig {
	return &CoverageConfig{
		TestCommand:  testCommand,
		CoverageArgs: []string{"--", "--coverage", "--coverageReporters=json"},
		OutputPath:   outputPath,
		Timeout:      timeout,
		Env:          []string{"NODE_ENV=test"},
		ProjectType:  ProjectTypeNode,
	}
}

// PythonCoverageConfig creates custom Python configuration
func PythonCoverageConfig(pythonCmd string, outputPath string, timeout time.Duration) *CoverageConfig {
	return &CoverageConfig{
		TestCommand:  []string{"coverage", "run", "-m", "pytest"},
		CoverageArgs: []string{"report", "--json", "-o", outputPath},
		OutputPath:   outputPath,
		DatabasePath: ".coverage",
		Timeout:      timeout,
		Env:          nil,
		ProjectType:  ProjectTypePython,
	}
}
