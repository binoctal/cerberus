package autotest

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// stubCov is a call-counting coverage provider that returns controlled reports
// in order, one per RunCoverage call. It exists to assert RepairGaps runs
// exactly len(gaps) verifications and NO baseline — the caller supplies before.
type stubCov struct {
	reports []*CoverageReport
	calls   int
}

func (s *stubCov) RunCoverage(context.Context, string) (*CoverageReport, error) {
	if s.calls >= len(s.reports) {
		// Defensive: a redundant baseline call would read here and still bump
		// calls, making the count assertions below fail as intended.
		s.calls++
		return withProfile(0), nil
	}
	r := s.reports[s.calls]
	s.calls++
	return r, nil
}

func (s *stubCov) Gaps(*CoverageReport) []CoverageGap { return nil }

// withProfile builds a passing line-coverage report at the given 0–100 pct.
func withProfile(pct float64) *CoverageReport {
	return &CoverageReport{
		Pass:            true,
		LineCoveragePct: pct,
		Profile:         []CoverageLine{{File: "x.go", Start: 1, End: 2, Count: 1}},
	}
}

// perGapGen returns a distinct test path per gap so two gaps don't clobber the
// same file in the memoryWriter.
type perGapGen struct{}

func (perGapGen) Generate(_ context.Context, gap CoverageGap, _ []byte) (TestFile, error) {
	return TestFile{Path: strings.TrimSuffix(gap.File, ".go") + "_test.go", Content: []byte("package p")}, nil
}

func TestRepairGaps_DirectProcessGap_KeepAndRevert(t *testing.T) {
	before := withProfile(50.0)
	gaps := []CoverageGap{
		{File: "a.go", Func: "F", Reason: ReasonZeroCover},
		{File: "b.go", Func: "G", Reason: ReasonZeroCover},
	}
	// gap#0 verify: higher pct → kept (written). gap#1 verify: flat pct → reverted.
	cov := &stubCov{reports: []*CoverageReport{withProfile(75.0), withProfile(50.0)}}
	w := &memoryWriter{}
	a := NewAutoTest(cov, perGapGen{}, allowGate{}, w, SafetyAuto, zap.NewNop())

	rep := a.RepairGaps(context.Background(), ".", before, gaps)

	require.Len(t, rep.Items, 2)
	assert.Equal(t, "written", rep.Items[0].Status)
	assert.Equal(t, "reverted", rep.Items[1].Status)
	// [R3] no own baseline: only the per-gap verify runs → calls == len(gaps).
	assert.Equal(t, len(gaps), cov.calls)
	// processGap-direct carries status on items; it does NOT populate the
	// aggregate Written/Reverted slices. Routing through executeSerial would
	// populate them — this is the [R2] divergence guard.
	assert.Empty(t, rep.Written)
	assert.Empty(t, rep.Reverted)
	// kept test stays on disk; reverted test is removed.
	assert.Contains(t, w.written, "a_test.go")
	assert.NotContains(t, w.written, "b_test.go")
}

func TestRepairGaps_NoOwnBaseline(t *testing.T) {
	// The caller supplies `before`; RepairGaps must NOT run its own baseline.
	// A redundant baseline would push RunCoverage calls to 2.
	before := withProfile(40.0)
	gaps := []CoverageGap{{File: "a.go", Func: "F", Reason: ReasonZeroCover}}
	cov := &stubCov{reports: []*CoverageReport{withProfile(80.0)}}
	a := NewAutoTest(cov, perGapGen{}, allowGate{}, &memoryWriter{}, SafetyAuto, zap.NewNop())

	a.RepairGaps(context.Background(), ".", before, gaps)

	assert.Equal(t, 1, cov.calls, "exactly one RunCoverage (per-gap verify), no baseline")
}

func TestRepairGaps_GateDenied_Skips(t *testing.T) {
	before := withProfile(40.0)
	gaps := []CoverageGap{{File: "a.go", Func: "F", Reason: ReasonZeroCover}}
	w := &memoryWriter{}
	a := NewAutoTest(&stubCov{}, perGapGen{}, denyGate{}, w, SafetyApprove, zap.NewNop())

	rep := a.RepairGaps(context.Background(), ".", before, gaps)

	require.Len(t, rep.Items, 1)
	assert.Equal(t, "skipped", rep.Items[0].Status)
	assert.Empty(t, w.written)
}
