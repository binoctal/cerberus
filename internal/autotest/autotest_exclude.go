package autotest

// gapKey is the (File,Func) identity of a gap, matching session.coverKey.
func gapKey(g CoverageGap) string { return g.File + "\x00" + g.Func }

// ExcludeTargets marks gaps already covered by the coverage repair loop; Run
// drops matching discovered gaps so Phase 4 does not redo them (D1 spec §6.7).
func (a *AutoTest) ExcludeTargets(gaps []CoverageGap) {
	if len(gaps) == 0 {
		return
	}
	if a.excludedTargets == nil {
		a.excludedTargets = map[string]bool{}
	}
	for _, g := range gaps {
		a.excludedTargets[gapKey(g)] = true
	}
}

// withoutExcluded drops gaps whose (File,Func) was marked via ExcludeTargets.
func (a *AutoTest) withoutExcluded(gaps []CoverageGap) []CoverageGap {
	if len(a.excludedTargets) == 0 {
		return gaps
	}
	out := make([]CoverageGap, 0, len(gaps))
	for _, g := range gaps {
		if !a.excludedTargets[gapKey(g)] {
			out = append(out, g)
		}
	}
	return out
}

// HasExcluded reports whether a (File,Func) tuple is marked excluded. Test helper.
func (a *AutoTest) HasExcluded(file, fn string) bool {
	return a.excludedTargets[file+"\x00"+fn]
}
