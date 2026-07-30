package autotest

import "sort"

// RankByGain returns gaps ordered by estimated coverage gain: when `before`
// carries a line-coverage profile (Go), gaps targeting a file with more
// zero-cover blocks rank first (more recoverable lines); otherwise (no profile
// / function-level Node/Python) the input order is preserved (uniform). The
// sort is stable, so equal-gain gaps keep their relative order.
//
// Reused by Phase-4 AutoTest.Run (before the MaxGaps cap) and by the coverage
// repair-loop dispatch (session.coverageEligibility), so both gap-selection
// paths share one ranking rule.
func RankByGain(gaps []CoverageGap, before *CoverageReport) []CoverageGap {
	if before == nil || len(before.Profile) == 0 || len(gaps) <= 1 {
		return gaps
	}
	gain := map[string]int{}
	for _, ln := range before.Profile {
		if ln.Count == 0 {
			gain[ln.File]++
		}
	}
	out := make([]CoverageGap, len(gaps))
	copy(out, gaps)
	sort.SliceStable(out, func(i, j int) bool {
		return gain[out[i].File] > gain[out[j].File]
	})
	return out
}
