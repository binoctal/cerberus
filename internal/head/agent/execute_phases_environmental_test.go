package agent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFinalizeResult_EnvironmentalSeenSurfacesUnreachable verifies that when a
// case hit an environmental failure on any attempt, the final result carries a
// "target unreachable" error — so the examiner classifies the whole case as
// environmental and excludes it from strategy effectiveness penalty, even if a
// later non-environmental attempt became the final result.
func TestFinalizeResult_EnvironmentalSeenSurfacesUnreachable(t *testing.T) {
	loop, _, _ := testLoop(t, nil, nil)
	se := &stepExecution{
		loop:              loop,
		ctx:               context.Background(),
		tc:                &TestCase{ID: "tc-1", Target: "/api/auth/login"},
		start:             time.Now(),
		environmentalSeen: true,
	}
	res := se.finalizeResult()
	require.Error(t, res.Error, "environmentalSeen must surface an unreachable error")
	assert.Contains(t, res.Error.Error(), "unreachable")
}

// TestFinalizeResult_NoEnvironmentalNoError confirms the error is only
// synthesized when an environmental failure was actually seen.
func TestFinalizeResult_NoEnvironmentalNoError(t *testing.T) {
	loop, _, _ := testLoop(t, nil, nil)
	se := &stepExecution{
		loop:  loop,
		ctx:   context.Background(),
		tc:    &TestCase{ID: "tc-2", Target: "/api/x"},
		start: time.Now(),
	}
	res := se.finalizeResult()
	assert.NoError(t, res.Error, "no environmental attempt → no synthesized error")
}
