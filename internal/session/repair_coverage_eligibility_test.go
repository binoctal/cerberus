package session

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/autotest"
	"github.com/binoctal/cerberus/internal/head/contract"
)

func TestHasCoverageGap(t *testing.T) {
	rp, cleanup := newTestRunPhase(t)
	defer cleanup()

	// No contract/assessment → false.
	assert.False(t, rp.hasCoverageGap())

	rp.session.Contract = &contract.Contract{}
	rp.session.Assessment = &contract.Assessment{
		Reached: false,
		Gaps:    []contract.Gap{{Kind: "scope", Detail: "missing module"}},
	}
	// scope-only Reached=false does NOT qualify — coverage axis recovers only a
	// known coverage shortfall (negative: triggering on !Reached would over-fire).
	assert.False(t, rp.hasCoverageGap())

	rp.session.Assessment.Gaps = append(rp.session.Assessment.Gaps,
		contract.Gap{Kind: "coverage", Detail: "below threshold"})
	assert.True(t, rp.hasCoverageGap())
}

func TestHasCoverageGap_NilGuards(t *testing.T) {
	rp, cleanup := newTestRunPhase(t)
	defer cleanup()

	rp.session.Contract = &contract.Contract{}
	rp.session.Assessment = nil
	assert.False(t, rp.hasCoverageGap())

	rp.session.Contract = nil
	rp.session.Assessment = &contract.Assessment{Gaps: []contract.Gap{{Kind: "coverage"}}}
	assert.False(t, rp.hasCoverageGap())
}

// goTempDir makes a tempdir containing a .go file so detectLanguage ⇒ "go"
// (the estimated-gain ranking path). Returns the dir.
func goTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "dummy.go"), []byte("package p"), 0o644))
	return dir
}

func TestCoverageEligibility_DropsTargetedAndEmptyFile_RanksByGain(t *testing.T) {
	rp, cleanup := newTestRunPhase(t)
	defer cleanup()
	rp.session.ProjectDir = goTempDir(t)

	// before profile: a.go has 3 zero-cover blocks, b.go has 1, c.go covered.
	before := &autotest.CoverageReport{CoverageUnit: "line", Profile: []autotest.CoverageLine{
		{File: "a.go", Start: 1, Count: 0},
		{File: "a.go", Start: 5, Count: 0},
		{File: "a.go", Start: 9, Count: 0},
		{File: "b.go", Start: 1, Count: 0},
		{File: "c.go", Start: 1, Count: 1},
	}}
	injected := []autotest.CoverageGap{
		{File: "c.go", Func: "C", Reason: autotest.ReasonZeroCover},       // 0 gain
		{File: "", Func: "X", Reason: autotest.ReasonZeroCover},           // empty File → dropped
		{File: "b.go", Func: "B", Reason: autotest.ReasonZeroCover},       // 1 gain
		{File: "a.go", Func: "a.go:L1", Reason: autotest.ReasonZeroCover}, // 3 gain
		{File: "a.go", Func: "a.go:L5", Reason: autotest.ReasonZeroCover}, // targeted → dropped
	}
	rp.coverageGapFn = func(_ *autotest.CoverageReport) []autotest.CoverageGap { return injected }

	targeted := map[coverKey]bool{{File: "a.go", Func: "a.go:L5"}: true}
	got := rp.coverageEligibility(targeted, before)

	// a.go:L5 (targeted) and "" dropped; remaining 3 ranked a.go(3) > b.go(1) > c.go(0).
	require.Len(t, got, 3)
	assert.Equal(t, "a.go", got[0].File)
	assert.Equal(t, "a.go:L1", got[0].Func)
	assert.Equal(t, "b.go", got[1].File)
	assert.Equal(t, "c.go", got[2].File)
}

func TestCoverageEligibility_CappedAtDispatchMax(t *testing.T) {
	rp, cleanup := newTestRunPhase(t)
	defer cleanup()
	rp.session.ProjectDir = goTempDir(t)

	var injected []autotest.CoverageGap
	for i := 0; i < 5; i++ {
		injected = append(injected, autotest.CoverageGap{
			File: "f.go", Func: fmt.Sprintf("f.go:L%d", i), Reason: autotest.ReasonZeroCover,
		})
	}
	rp.coverageGapFn = func(_ *autotest.CoverageReport) []autotest.CoverageGap { return injected }
	got := rp.coverageEligibility(nil, &autotest.CoverageReport{CoverageUnit: "line"})
	assert.Len(t, got, defaultCoverageDispatchGaps)
}

func TestCoverageEligibility_AnchorKeyedRaw(t *testing.T) {
	rp, cleanup := newTestRunPhase(t)
	defer cleanup()
	rp.session.ProjectDir = goTempDir(t)

	rp.coverageGapFn = func(_ *autotest.CoverageReport) []autotest.CoverageGap {
		return []autotest.CoverageGap{{File: "a.go", Func: "a.go:L42", Reason: autotest.ReasonZeroCover}}
	}
	// Targeted by the RAW file:line anchor → gap dropped.
	targeted := map[coverKey]bool{{File: "a.go", Func: "a.go:L42"}: true}
	got := rp.coverageEligibility(targeted, &autotest.CoverageReport{CoverageUnit: "line"})
	assert.Empty(t, got, "raw file:line anchor matched → gap dropped")

	// A normalized Func (e.g. just the file) does NOT match the raw anchor → kept.
	targeted2 := map[coverKey]bool{{File: "a.go", Func: "a.go"}: true}
	got2 := rp.coverageEligibility(targeted2, &autotest.CoverageReport{CoverageUnit: "line"})
	assert.Len(t, got2, 1, "normalized Func does not match raw anchor → gap kept")
}
