package autotest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestRankByGain_LineProfile: with a line-coverage profile, gaps rank by
// zero-cover block count per file (descending, stable). This is the
// estimated-gain signal reused by Phase-4 Run and the coverage repair dispatch.
func TestRankByGain_LineProfile(t *testing.T) {
	before := &CoverageReport{CoverageUnit: "line", Profile: []CoverageLine{
		{File: "b.go", Start: 1, Count: 0},
		{File: "b.go", Start: 5, Count: 0},
		{File: "b.go", Start: 9, Count: 0},
		{File: "a.go", Start: 1, Count: 0},
		{File: "c.go", Start: 1, Count: 1}, // covered
	}}
	gaps := []CoverageGap{
		{File: "a.go", Func: "A", Reason: ReasonZeroCover}, // 1 zero block
		{File: "b.go", Func: "B", Reason: ReasonZeroCover}, // 3 zero blocks
		{File: "c.go", Func: "C", Reason: ReasonZeroCover}, // 0
	}
	ranked := RankByGain(gaps, before)
	require.Len(t, ranked, 3)
	assert.Equal(t, "b.go", ranked[0].File, "highest gain first")
	assert.Equal(t, "a.go", ranked[1].File)
	assert.Equal(t, "c.go", ranked[2].File)
}

// TestRankByGain_NoProfileKeepsOrder: without a line profile (Node/Python
// function-level), ranking is uniform — the input order is preserved.
func TestRankByGain_NoProfileKeepsOrder(t *testing.T) {
	gaps := []CoverageGap{
		{File: "a.js", Func: "A"},
		{File: "b.js", Func: "B"},
		{File: "c.js", Func: "C"},
	}
	ranked := RankByGain(gaps, &CoverageReport{CoverageUnit: "function"})
	require.Len(t, ranked, 3)
	assert.Equal(t, []string{"a.js", "b.js", "c.js"}, []string{ranked[0].File, ranked[1].File, ranked[2].File})

	// Nil before → unchanged too.
	ranked2 := RankByGain(gaps, nil)
	assert.Equal(t, []string{"a.js", "b.js", "c.js"}, []string{ranked2[0].File, ranked2[1].File, ranked2[2].File})
}

// TestRankByGain_StableForEqualGain: equal-gain gaps keep their input order.
func TestRankByGain_StableForEqualGain(t *testing.T) {
	before := &CoverageReport{CoverageUnit: "line", Profile: []CoverageLine{
		{File: "a.go", Start: 1, Count: 0},
		{File: "b.go", Start: 1, Count: 0},
	}}
	gaps := []CoverageGap{
		{File: "a.go", Func: "A"},
		{File: "b.go", Func: "B"},
	}
	ranked := RankByGain(gaps, before)
	assert.Equal(t, "a.go", ranked[0].File, "equal gain → stable input order")
	assert.Equal(t, "b.go", ranked[1].File)
}

// profileGapProvider returns a line-profile before report (with controlled
// zero-cover blocks per file) and a fixed gap list, so AutoTest.Run's ranking
// is observable.
type profileGapProvider struct {
	gaps    []CoverageGap
	profile []CoverageLine
}

func (p *profileGapProvider) RunCoverage(context.Context, string) (*CoverageReport, error) {
	return &CoverageReport{Pass: true, CoverageUnit: "line", LineCoveragePct: 10, Profile: p.profile}, nil
}
func (p *profileGapProvider) Gaps(*CoverageReport) []CoverageGap { return p.gaps }

// TestAutoTest_Run_RanksByGainBeforeCap: Phase-4 Run ranks gaps by estimated
// gain BEFORE the MaxGaps cap, so the highest-gain gaps survive over FIFO.
// Replaces the FIFO "future revision" noted at autotest_run.go:32.
func TestAutoTest_Run_RanksByGainBeforeCap(t *testing.T) {
	provider := &profileGapProvider{
		profile: []CoverageLine{
			{File: "big.go", Start: 1, Count: 0}, {File: "big.go", Start: 2, Count: 0}, {File: "big.go", Start: 3, Count: 0},
			{File: "mid.go", Start: 1, Count: 0}, {File: "mid.go", Start: 2, Count: 0},
			{File: "low.go", Start: 1, Count: 0},
		},
		gaps: []CoverageGap{
			{File: "low.go", Func: "low.go:L1"}, // FIFO would take this first
			{File: "mid.go", Func: "mid.go:L1"},
			{File: "big.go", Func: "big.go:L1"}, // highest gain
		},
	}
	a := NewAutoTest(provider, stubGen{"package p"}, allowGate{}, &memoryWriter{}, SafetyDryRun, zap.NewNop())
	a.MaxGaps = 2

	rep, err := a.Run(context.Background(), ".")
	require.NoError(t, err)
	require.Len(t, rep.Gaps, 2, "capped to MaxGaps=2")
	// big.go (3) and mid.go (2) survive; low.go (1) is dropped under the cap.
	assert.Equal(t, "big.go", rep.Gaps[0].File)
	assert.Equal(t, "mid.go", rep.Gaps[1].File)
}
