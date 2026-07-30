package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/autotest"
)

// TestCoverageRepairLoop_ClosedLoop_EndToEnd drives the full coverage repair
// loop with mocked provider/generator (no real toolchain): a coverage gap
// dispatches AutoTest, which writes a real test file to disk via FSWriter, the
// re-measure rises past the gate, and CoverageRecovered is set — while the
// Agent-only Assessment stays unchanged (D1 invariant). Spec test #11.
func TestCoverageRepairLoop_ClosedLoop_EndToEnd(t *testing.T) {
	rp, cleanup := coverageAxisSetup(t) // threshold 0.7, Assessment.CoveragePct 0.5
	defer cleanup()

	// Put a real source file in ProjectDir so processGap can read it.
	require.NoError(t, os.WriteFile(filepath.Join(rp.session.ProjectDir, "a.go"),
		[]byte("package p\n\nfunc A() int { return 1 }\n"), 0o644))

	rp.coverageGapFn = func(_ *autotest.CoverageReport) []autotest.CoverageGap {
		return []autotest.CoverageGap{{File: "a.go", Func: "a.go:L1", Reason: autotest.ReasonZeroCover}}
	}
	cov := &rpStubCov{reports: []*autotest.CoverageReport{
		lineReport(50), // before measure
		lineReport(80), // processGap verify (gain → kept on disk)
		lineReport(80), // after measure
	}}
	rp.coverageProvider = cov
	rp.autotestGenerator = &rpStubGen{dir: rp.session.ProjectDir}
	rp.session.Config.Settings.ReplanMaxRounds = 2

	require.NoError(t, rp.executeRepairLoop())

	// The generated test file was written to disk by FSWriter and kept (gain).
	_, err := os.Stat(filepath.Join(rp.session.ProjectDir, "a_test.go"))
	assert.NoError(t, err, "AutoTest wrote and kept the generated test file")

	// Closed loop: coverage recovered, repaired measurement set.
	require.NotNil(t, rp.session.RepairedCoverage)
	assert.True(t, rp.session.CoverageRecovered, "80% >= 70% gate → recovered")
	assert.InDelta(t, 0.8, rp.session.RepairedCoverage.Pct, 0.001)
	// D1 invariant: the Agent-only Assessment is never overwritten.
	assert.Equal(t, 0.5, rp.session.Assessment.CoveragePct)
	assert.False(t, rp.session.Assessment.Reached)
	// Gap recorded as targeted (Phase 4 will exclude it).
	assert.True(t, rp.session.repairTargeted[coverKey{File: "a.go", Func: "a.go:L1"}])
}

// TestCoverageRepairLoop_NoProgressStops_EndToEnd: when the dispatch does not
// move coverage, the loop stalls after one round — no recovered flag, no spin.
func TestCoverageRepairLoop_NoProgressStops_EndToEnd(t *testing.T) {
	rp, cleanup := coverageAxisSetup(t)
	defer cleanup()
	rp.session.Contract.CoverageGate.LineThreshold = 0.99 // never recovered

	require.NoError(t, os.WriteFile(filepath.Join(rp.session.ProjectDir, "a.go"),
		[]byte("package p\n\nfunc A() int { return 1 }\n"), 0o644))
	rp.coverageGapFn = func(_ *autotest.CoverageReport) []autotest.CoverageGap {
		return []autotest.CoverageGap{{File: "a.go", Func: "a.go:L1", Reason: autotest.ReasonZeroCover}}
	}
	// Every measure is 50% (== Assessment baseline) → delta 0 → the verify has
	// no gain (test reverted) and the axis stalls after round 1.
	cov := &rpStubCov{}
	for i := 0; i < 8; i++ {
		cov.reports = append(cov.reports, lineReport(50))
	}
	rp.coverageProvider = cov
	rp.autotestGenerator = &rpStubGen{dir: rp.session.ProjectDir}
	rp.session.Config.Settings.ReplanMaxRounds = 3

	require.NoError(t, rp.executeRepairLoop())

	assert.False(t, rp.session.CoverageRecovered, "no progress → not recovered")
	// Round 1 ran the axis (before + verify + after = 3 runs); delta 0 stalled
	// it, so rounds 2 and 3 skipped the coverage axis (no spin to the cap).
	assert.Equal(t, 3, cov.calls, "coverage axis ran once then stalled")
}
