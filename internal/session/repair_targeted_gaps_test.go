package session

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/autotest"
)

// TestTargetedGaps_ConvertsRepairTargeted: the in-memory targeted set is
// projected to CoverageGaps for AutoTest.ExcludeTargets (D1 §6.7 wiring).
func TestTargetedGaps_ConvertsRepairTargeted(t *testing.T) {
	rp, cleanup := newTestRunPhase(t)
	defer cleanup()

	rp.session.repairTargeted = map[coverKey]bool{
		{File: "a.go", Func: "a.go:L1"}: true,
		{File: "b.go", Func: "B"}:       true,
	}
	gaps := rp.targetedGaps()
	assert.Len(t, gaps, 2)

	seen := map[string]bool{}
	for _, g := range gaps {
		seen[g.File+"\x00"+g.Func] = true
	}
	assert.True(t, seen["a.go\x00a.go:L1"])
	assert.True(t, seen["b.go\x00B"])

	// Empty set → nil (ExcludeTargets no-op).
	rp.session.repairTargeted = nil
	assert.Nil(t, rp.targetedGaps())

	// Ensure the autotest-level exclusion consumes these directly.
	at := autotest.NewAutoTest(nil, nil, nil, nil, autotest.SafetyDryRun, nil)
	at.ExcludeTargets(gaps)
	assert.True(t, at.HasExcluded("a.go", "a.go:L1"))
	assert.False(t, at.HasExcluded("c.go", "C"))
}
