package session

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/head/contract"
)

func TestSessionSummary_CoverageRecoveredAnnotation(t *testing.T) {
	s := &SessionSummary{
		Failed:            1,
		Passed:            2,
		Assessment:        &contract.Assessment{Reached: false, CoveragePct: 0.5},
		RepairedCoverage:  &contract.CoverageMeasurement{Pct: 0.85, Unit: "line", Known: true},
		CoverageRecovered: true,
	}
	out := s.String()
	assert.Contains(t, out, "50% (not reached)")
	assert.Contains(t, out, "85% (recovered)")

	// Observability-only (D1 invariant, spec §6.6 [R10]): rendering the
	// annotation must NOT flip the gate verdict or any case count.
	assert.Equal(t, 1, s.Failed, "Failed count unchanged by recovery")
	assert.Equal(t, 2, s.Passed, "Passed count unchanged by recovery")
	assert.False(t, s.Assessment.Reached, "Agent gate Reached stays false")
}

func TestSessionSummary_NoAnnotationWhenNotRecovered(t *testing.T) {
	s := &SessionSummary{
		Assessment:        &contract.Assessment{Reached: false, CoveragePct: 0.5},
		RepairedCoverage:  &contract.CoverageMeasurement{Pct: 0.6, Known: true},
		CoverageRecovered: false, // repaired but below threshold → not recovered
	}
	out := s.String()
	assert.NotContains(t, out, "(recovered)")
}

func TestSessionSummary_RecoveredRoundTrip(t *testing.T) {
	// RepairedCoverage/CoverageRecovered persist via the stats JSON blob on the
	// session row (UpdateSessionStats marshals SessionSummary). They must
	// round-trip; repairTargeted is NOT part of the summary (not persisted).
	s := &SessionSummary{
		Assessment:        &contract.Assessment{Reached: false, CoveragePct: 0.5},
		RepairedCoverage:  &contract.CoverageMeasurement{Pct: 0.85, Unit: "line", Known: true},
		CoverageRecovered: true,
	}
	raw, err := json.Marshal(s)
	require.NoError(t, err)

	var back SessionSummary
	require.NoError(t, json.Unmarshal(raw, &back))
	assert.True(t, back.CoverageRecovered)
	require.NotNil(t, back.RepairedCoverage)
	assert.InDelta(t, 0.85, back.RepairedCoverage.Pct, 0.001)
	assert.Equal(t, "line", back.RepairedCoverage.Unit)
}
