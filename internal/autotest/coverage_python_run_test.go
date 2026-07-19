package autotest

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const pythonValidJSON = `{
  "files": {
    "/f.py": {
      "summary": {"num_statements": 2, "covered_lines": 1, "percent_covered": 50.0, "missing_lines": 1},
      "executed_lines": [1],
      "missing_lines": [2]
    }
  },
  "meta": {"branch_coverage": false, "timestamp": "2026-07-19T00:00:00"}
}`

func TestPythonRunCoverage_NilConfig(t *testing.T) {
	p := NewPythonCoverageProvider(nil, func(context.Context, string) ([]byte, error) { return nil, nil }, nil)
	_, err := p.RunCoverage(context.Background(), ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config")
}

func TestPythonRunCoverage_NilRunner(t *testing.T) {
	p := NewPythonCoverageProvider(DefaultPythonCoverageConfig(), nil, nil)
	_, err := p.RunCoverage(context.Background(), ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runner")
}

func TestPythonRunCoverage_RunnerError(t *testing.T) {
	p := NewPythonCoverageProvider(DefaultPythonCoverageConfig(),
		func(context.Context, string) ([]byte, error) { return nil, errors.New("boom") }, nil)
	_, err := p.RunCoverage(context.Background(), ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

func TestPythonRunCoverage_ValidJSON(t *testing.T) {
	p := NewPythonCoverageProvider(DefaultPythonCoverageConfig(),
		func(context.Context, string) ([]byte, error) { return []byte(pythonValidJSON), nil }, nil)
	rep, err := p.RunCoverage(context.Background(), ".")
	require.NoError(t, err)
	assert.True(t, rep.Pass)
	assert.Equal(t, "function", rep.CoverageUnit)
	assert.Equal(t, 2, rep.TotalFuncs)
	assert.Equal(t, 1, rep.CoveredFuncs)
}

func TestPythonRunCoverage_Garbage(t *testing.T) {
	p := NewPythonCoverageProvider(DefaultPythonCoverageConfig(),
		func(context.Context, string) ([]byte, error) { return []byte("not json"), nil }, nil)
	_, err := p.RunCoverage(context.Background(), ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse")
}
