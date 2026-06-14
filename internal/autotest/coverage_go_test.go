package autotest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A minimal cover.out: one fully-covered span, one zero-covered span.
const fixtureCoverOut = `mode: set
example.com/pkg/foo.go:10.1,12.2 2 1
example.com/pkg/foo.go:20.1,22.2 2 0
`

func TestGoCoverage_RunCoverage_ParsesProfile(t *testing.T) {
	p := NewGoCoverageProvider(func(_ context.Context, _ string) ([]byte, error) {
		return []byte(fixtureCoverOut), nil
	}, nil)
	rep, err := p.RunCoverage(context.Background(), ".")
	require.NoError(t, err)
	assert.True(t, rep.Pass)
	assert.Len(t, rep.Profile, 2)
	assert.Equal(t, 1, rep.Profile[0].Count) // covered
	assert.Equal(t, 0, rep.Profile[1].Count) // uncovered
}

func TestGoCoverage_Gaps_ZeroCover(t *testing.T) {
	p := NewGoCoverageProvider(nil, nil)
	rep := &CoverageReport{Profile: []CoverageLine{
		{File: "pkg/a.go", Start: 10, End: 12, Count: 0},
		{File: "pkg/a.go", Start: 20, End: 22, Count: 1},
	}}
	gaps := p.Gaps(rep)
	require.Len(t, gaps, 1)
	assert.Equal(t, "pkg/a.go", gaps[0].File)
	assert.Equal(t, ReasonZeroCover, gaps[0].Reason)
}
