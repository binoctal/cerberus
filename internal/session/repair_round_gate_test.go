package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/autotest"
	"github.com/binoctal/cerberus/internal/escalation"
)

// abortGate is an escalation.Gate that always aborts the loop.
type abortGate struct{}

func (abortGate) Check(context.Context, escalation.Event) escalation.Decision {
	return escalation.Decision{Action: escalation.DecisionAbort}
}

func TestExecuteRepairLoop_GateAbort_Stops(t *testing.T) {
	rp, cleanup := coverageAxisSetup(t)
	defer cleanup()
	rp.session.Gate = abortGate{}
	rp.coverageProvider = &rpStubCov{reports: []*autotest.CoverageReport{lineReport(80)}}
	rp.coverageGapFn = func(_ *autotest.CoverageReport) []autotest.CoverageGap {
		return []autotest.CoverageGap{{File: "a.go", Func: "a.go:L1", Reason: autotest.ReasonZeroCover}}
	}
	rp.session.Config.Settings.ReplanMaxRounds = 3

	require.NoError(t, rp.executeRepairLoop())

	// The gate aborts at the round check, before any axis dispatches.
	assert.Nil(t, rp.session.RepairedCoverage, "gate Abort stops the loop before dispatch")
}

func TestExecuteRepairLoop_BudgetExhausted_Stops(t *testing.T) {
	rp, cleanup := coverageAxisSetup(t)
	defer cleanup()

	// Exhaust the token budget so the explicit backstop fires.
	b := rp.session.Driver.Budget()
	b.Record(b.SessionTotal)

	rp.coverageProvider = &rpStubCov{reports: []*autotest.CoverageReport{lineReport(80)}}
	rp.coverageGapFn = func(_ *autotest.CoverageReport) []autotest.CoverageGap {
		return []autotest.CoverageGap{{File: "a.go", Func: "a.go:L1", Reason: autotest.ReasonZeroCover}}
	}
	rp.session.Config.Settings.ReplanMaxRounds = 3

	require.NoError(t, rp.executeRepairLoop())

	assert.Nil(t, rp.session.RepairedCoverage, "budget exhausted → loop stops before dispatching")
}
