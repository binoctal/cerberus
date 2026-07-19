package autotest

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const jestValidJSON = `{
  "/f.js": {
    "statementMap": {
      "0": {"start": {"line": 1, "column": 0}, "end": {"line": 1, "column": 5}},
      "1": {"start": {"line": 2, "column": 0}, "end": {"line": 2, "column": 5}}
    },
    "s": {"0": 1, "1": 0}
  }
}`

func TestNodeRunCoverage_NilConfig(t *testing.T) {
	p := NewNodeCoverageProvider(nil, func(context.Context, string) ([]byte, error) { return nil, nil }, nil)
	_, err := p.RunCoverage(context.Background(), ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config")
}

func TestNodeRunCoverage_NilRunner(t *testing.T) {
	p := NewNodeCoverageProvider(DefaultNodeCoverageConfig(), nil, nil)
	_, err := p.RunCoverage(context.Background(), ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runner")
}

func TestNodeRunCoverage_RunnerError(t *testing.T) {
	p := NewNodeCoverageProvider(DefaultNodeCoverageConfig(),
		func(context.Context, string) ([]byte, error) { return nil, errors.New("boom") }, nil)
	_, err := p.RunCoverage(context.Background(), ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

func TestNodeRunCoverage_ValidJSON(t *testing.T) {
	p := NewNodeCoverageProvider(DefaultNodeCoverageConfig(),
		func(context.Context, string) ([]byte, error) { return []byte(jestValidJSON), nil }, nil)
	rep, err := p.RunCoverage(context.Background(), ".")
	require.NoError(t, err)
	assert.True(t, rep.Pass)
	assert.Equal(t, "function", rep.CoverageUnit)
	assert.Equal(t, 2, rep.TotalFuncs)
	assert.Equal(t, 1, rep.CoveredFuncs)
}

func TestNodeRunCoverage_Garbage(t *testing.T) {
	p := NewNodeCoverageProvider(DefaultNodeCoverageConfig(),
		func(context.Context, string) ([]byte, error) { return []byte("not json"), nil }, nil)
	_, err := p.RunCoverage(context.Background(), ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse")
}
