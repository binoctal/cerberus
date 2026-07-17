package autotest

import (
	"context"
	"os"
	"path/filepath"
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

func TestGoCoverage_NoTestFileGaps(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package p\nfunc A(){}\n"), 0o644))
	// a_test.go missing
	p := NewGoCoverageProvider(nil, nil)
	gaps := p.NoTestFileGaps(dir)
	require.Len(t, gaps, 1)
	assert.Equal(t, filepath.Join(dir, "a.go"), gaps[0].File)
	assert.Equal(t, ReasonNoTestFile, gaps[0].Reason)

	// adding a_test.go removes the gap
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a_test.go"), []byte("package p\n"), 0o644))
	assert.Empty(t, p.NoTestFileGaps(dir))
}

func TestParseCoverProfile_LineCoveragePct(t *testing.T) {
	// Two blocks: one covered (count>0, 10 stmts), one uncovered (count=0, 30 stmts).
	// => 10/40 covered = 25%.
	in := []byte("mode: set\n" +
		"foo/bar.go:1.1,2.2 10 1\n" +
		"foo/baz.go:5.1,6.2 30 0\n")
	rep, err := parseCoverProfile(in)
	require.NoError(t, err)
	assert.Equal(t, "line", rep.CoverageUnit)
	assert.InDelta(t, 25.0, rep.LineCoveragePct, 0.001)
}

func TestParseCoverProfile_ZeroCoveredIsKnown(t *testing.T) {
	// All blocks uncovered => 0% but still measured (denominator > 0).
	in := []byte("mode: set\nfoo/bar.go:1.1,2.2 10 0\n")
	rep, err := parseCoverProfile(in)
	require.NoError(t, err)
	assert.Equal(t, 0.0, rep.LineCoveragePct)
	// denominator present => caller treats as Known. Expose via Profile length.
	assert.NotEmpty(t, rep.Profile)
}
