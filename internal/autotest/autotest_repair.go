package autotest

import "context"

// RepairGaps generates + writes + verifies unit tests for the given explicit
// gaps, calling processGap DIRECTLY with the caller-supplied before report. It
// bypasses Run/executeSerial/executeParallel: the caller already measured
// `before` and selected the gaps, so there is no baseline run here. This
// canonicalizes on processGap (not executeSerial) so the safety rails the D1
// spec relies on — revert on no-gain, destructive_risk escalation — are exactly
// the ones in effect. Per-gap outcomes become item statuses; nothing aborts.
func (a *AutoTest) RepairGaps(ctx context.Context, projectDir string, before *CoverageReport, gaps []CoverageGap) *AutoTestReport {
	rep := &AutoTestReport{}
	for _, g := range gaps {
		item := a.processGap(ctx, g, projectDir, before, rep)
		rep.Items = append(rep.Items, *item)
	}
	return rep
}
