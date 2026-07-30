package autotest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestAutoTest_Run_ExcludesTargetedGaps: Phase 4's AutoTest.Run drops gaps the
// coverage repair loop already targeted (D1 §6.7). Negative: not calling
// ExcludeTargets leaves all discovered gaps selected (RED).
func TestAutoTest_Run_ExcludesTargetedGaps(t *testing.T) {
	w := &memoryWriter{}
	provider := &mockCoverageProvider{pass: true, gaps: []CoverageGap{
		{File: "a.go", Func: "F", Reason: ReasonZeroCover},
		{File: "b.go", Func: "G", Reason: ReasonZeroCover},
		{File: "c.go", Func: "H", Reason: ReasonZeroCover},
	}}
	a := NewAutoTest(provider, stubGen{"package p"}, allowGate{}, w, SafetyDryRun, zap.NewNop())
	a.MaxGaps = 0 // no cap, so all surviving gaps are visible
	a.ExcludeTargets([]CoverageGap{{File: "b.go", Func: "G"}})

	rep, err := a.Run(context.Background(), ".")
	require.NoError(t, err)

	assert.Len(t, rep.Gaps, 2, "targeted gap dropped; the other two remain")
	for _, g := range rep.Gaps {
		assert.NotEqual(t, "b.go", g.File, "Phase 4 must not reselect a loop-targeted gap")
	}
}

// TestAutoTest_Run_NoExclusionKeepsAll: with no ExcludeTargets, Run keeps every
// discovered gap (the negative reference for the exclusion behavior).
func TestAutoTest_Run_NoExclusionKeepsAll(t *testing.T) {
	w := &memoryWriter{}
	provider := &mockCoverageProvider{pass: true, gaps: []CoverageGap{
		{File: "a.go", Func: "F", Reason: ReasonZeroCover},
		{File: "b.go", Func: "G", Reason: ReasonZeroCover},
	}}
	a := NewAutoTest(provider, stubGen{"package p"}, allowGate{}, w, SafetyDryRun, zap.NewNop())
	a.MaxGaps = 0

	rep, err := a.Run(context.Background(), ".")
	require.NoError(t, err)
	assert.Len(t, rep.Gaps, 2, "no exclusion → all gaps selected")
}
