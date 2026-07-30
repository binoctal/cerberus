package session

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/autotest"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/contract"
)

// rpStubCov is a call-counting coverage provider shared by the round
// measurement (measureCoverageReport) and the AutoTest dispatch (buildAutoTest)
// so the per-round RunCoverage cost is observable.
type rpStubCov struct {
	reports []*autotest.CoverageReport
	calls   int
}

func (s *rpStubCov) RunCoverage(context.Context, string) (*autotest.CoverageReport, error) {
	idx := s.calls
	s.calls++
	if idx >= len(s.reports) {
		return lineReport(0), nil
	}
	return s.reports[idx], nil
}
func (s *rpStubCov) Gaps(*autotest.CoverageReport) []autotest.CoverageGap { return nil }

// lineReport builds a passing line-coverage report at the given 0–100 pct.
func lineReport(pct100 float64) *autotest.CoverageReport {
	return &autotest.CoverageReport{
		Pass:            true,
		LineCoveragePct: pct100,
		CoverageUnit:    "line",
		Profile:         []autotest.CoverageLine{{File: "x.go", Start: 1, End: 2, Count: 1}},
	}
}

// rpStubGen writes generated tests into dir so FSWriter succeeds without an LLM.
type rpStubGen struct{ dir string }

func (g *rpStubGen) Generate(_ context.Context, gap autotest.CoverageGap, _ []byte) (autotest.TestFile, error) {
	return autotest.TestFile{
		Path:    filepath.Join(g.dir, strings.TrimSuffix(gap.File, ".go")+"_test.go"),
		Content: []byte("package p"),
	}, nil
}

func coverageAxisSetup(t *testing.T) (*runPhase, func()) {
	rp, cleanup := newTestRunPhase(t)
	rp.session.ProjectDir = goTempDir(t)
	rp.session.AutoTestSafety = "auto"
	rp.session.Contract = &contract.Contract{CoverageGate: contract.Gate{LineThreshold: 0.7}}
	rp.session.Assessment = &contract.Assessment{
		Reached:     false,
		CoveragePct: 0.5,
		Gaps:        []contract.Gap{{Kind: "coverage", Detail: "below gate"}},
	}
	rp.plan = &agent.TestPlan{Goal: "g"}
	return rp, cleanup
}

func TestExecuteRepairLoop_CoverageAxis_Rises(t *testing.T) {
	rp, cleanup := coverageAxisSetup(t)
	defer cleanup()

	rp.coverageGapFn = func(_ *autotest.CoverageReport) []autotest.CoverageGap {
		return []autotest.CoverageGap{{File: "a.go", Func: "a.go:L1", Reason: autotest.ReasonZeroCover}}
	}
	cov := &rpStubCov{reports: []*autotest.CoverageReport{
		lineReport(50), // before measure
		lineReport(80), // processGap verify (gain → kept)
		lineReport(80), // after measure
	}}
	rp.coverageProvider = cov
	rp.autotestGenerator = &rpStubGen{dir: rp.session.ProjectDir}
	rp.session.Config.Settings.ReplanMaxRounds = 1

	require.NoError(t, rp.executeRepairLoop())

	// Coverage recovered + repaired measurement set.
	require.NotNil(t, rp.session.RepairedCoverage)
	assert.True(t, rp.session.CoverageRecovered, "80% >= 70% threshold → recovered")
	assert.InDelta(t, 0.8, rp.session.RepairedCoverage.Pct, 0.001)
	// D1 invariant: the Agent-only Assessment is never overwritten by the loop.
	assert.Equal(t, 0.5, rp.session.Assessment.CoveragePct, "Assessment.CoveragePct unchanged")
	assert.False(t, rp.session.Assessment.Reached, "Agent Reached=false stays")
	// Dispatched gap recorded as targeted.
	assert.True(t, rp.session.repairTargeted[coverKey{File: "a.go", Func: "a.go:L1"}])
}

func TestExecuteRepairLoop_CoverageAxis_DryRun_Skipped(t *testing.T) {
	rp, cleanup := coverageAxisSetup(t)
	defer cleanup()
	rp.session.AutoTestSafety = "dry-run"

	cov := &rpStubCov{reports: []*autotest.CoverageReport{lineReport(80)}}
	rp.coverageProvider = cov
	rp.coverageGapFn = func(_ *autotest.CoverageReport) []autotest.CoverageGap {
		return []autotest.CoverageGap{{File: "a.go", Func: "a.go:L1", Reason: autotest.ReasonZeroCover}}
	}
	rp.session.Config.Settings.ReplanMaxRounds = 1

	require.NoError(t, rp.executeRepairLoop())

	// DryRun: axis skipped before any measurement → no misleading RepairedCoverage.
	assert.Nil(t, rp.session.RepairedCoverage)
	assert.False(t, rp.session.CoverageRecovered)
	assert.Equal(t, 0, cov.calls, "DryRun skips the axis entirely — no provider runs")
}

func TestExecuteRepairLoop_CoverageAxis_NoProgress_Stalls(t *testing.T) {
	rp, cleanup := coverageAxisSetup(t)
	defer cleanup()
	rp.session.Contract.CoverageGate.LineThreshold = 0.99 // never recovered

	rp.coverageGapFn = func(_ *autotest.CoverageReport) []autotest.CoverageGap {
		return []autotest.CoverageGap{{File: "a.go", Func: "a.go:L1", Reason: autotest.ReasonZeroCover}}
	}
	// Every measure returns 50% → delta vs Assessment baseline (0.5) is 0 → stall.
	cov := &rpStubCov{}
	for i := 0; i < 6; i++ {
		cov.reports = append(cov.reports, lineReport(50))
	}
	rp.coverageProvider = cov
	rp.autotestGenerator = &rpStubGen{dir: rp.session.ProjectDir}
	rp.session.Config.Settings.ReplanMaxRounds = 2

	require.NoError(t, rp.executeRepairLoop())

	// Round 1 runs the axis (before + verify + after = 3 runs); delta 0 stalls
	// it, so round 2 skips the coverage axis. Without the stall it would spin.
	assert.Equal(t, 3, cov.calls, "delta 0 stalls the coverage axis after one round")
}

func TestExecuteRepairLoop_CoverageAxis_ProviderCallCount(t *testing.T) {
	rp, cleanup := coverageAxisSetup(t)
	defer cleanup()
	rp.session.Assessment.CoveragePct = 0.4
	rp.session.Contract.CoverageGate.LineThreshold = 0.99 // avoid recovered short-circuit

	gaps := []autotest.CoverageGap{
		{File: "a.go", Func: "a.go:L1", Reason: autotest.ReasonZeroCover},
		{File: "b.go", Func: "b.go:L1", Reason: autotest.ReasonZeroCover},
		{File: "c.go", Func: "c.go:L1", Reason: autotest.ReasonZeroCover},
	}
	rp.coverageGapFn = func(_ *autotest.CoverageReport) []autotest.CoverageGap { return gaps }
	cov := &rpStubCov{}
	for i := 0; i < 12; i++ {
		cov.reports = append(cov.reports, lineReport(80)) // verifies show gain → kept
	}
	cov.reports[0] = lineReport(50) // before measure (lower baseline)
	rp.coverageProvider = cov
	rp.autotestGenerator = &rpStubGen{dir: rp.session.ProjectDir}
	rp.session.Config.Settings.ReplanMaxRounds = 1

	require.NoError(t, rp.executeRepairLoop())

	// Per round: 1 shared before + N per-gap verifies + 1 after = N+2 across the
	// shared provider. A redundant AutoTest baseline would make this N+3.
	assert.Equal(t, len(gaps)+2, cov.calls, "provider runs == N+2 (shared before, no own baseline)")
}
