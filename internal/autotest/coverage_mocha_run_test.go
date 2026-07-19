package autotest

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMochaRunCoverage_NilConfig(t *testing.T) {
	p := NewMochaCoverageProvider(nil, func(context.Context, string) ([]byte, error) { return nil, nil }, nil)
	_, err := p.RunCoverage(context.Background(), ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config")
}

func TestMochaRunCoverage_NilRunner(t *testing.T) {
	p := NewMochaCoverageProvider(DefaultMochaCoverageConfig(), nil, nil)
	_, err := p.RunCoverage(context.Background(), ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runner")
}

func TestMochaRunCoverage_RunnerError(t *testing.T) {
	p := NewMochaCoverageProvider(DefaultMochaCoverageConfig(),
		func(context.Context, string) ([]byte, error) { return nil, errors.New("boom") }, nil)
	_, err := p.RunCoverage(context.Background(), ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

func TestMochaRunCoverage_ValidJSON(t *testing.T) {
	p := NewMochaCoverageProvider(DefaultMochaCoverageConfig(),
		func(context.Context, string) ([]byte, error) { return []byte(jestValidJSON), nil }, nil)
	rep, err := p.RunCoverage(context.Background(), ".")
	require.NoError(t, err)
	assert.True(t, rep.Pass)
	assert.Equal(t, "function", rep.CoverageUnit)
	assert.Equal(t, 2, rep.TotalFuncs)
	assert.Equal(t, 1, rep.CoveredFuncs)
}

func TestMochaRunCoverage_Garbage(t *testing.T) {
	p := NewMochaCoverageProvider(DefaultMochaCoverageConfig(),
		func(context.Context, string) ([]byte, error) { return []byte("not json"), nil }, nil)
	_, err := p.RunCoverage(context.Background(), ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse")
}
