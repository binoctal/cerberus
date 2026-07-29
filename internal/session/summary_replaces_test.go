package session_test

import (
	"testing"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/binoctal/cerberus/internal/session"
	"github.com/stretchr/testify/assert"
)

// TestFromResults_ReplacesRecovered: a passed replacement recovers its original
// fail, mirroring FallbackFor semantics — the primary is reclassified OUT of
// Failed into Recovered (counted once, as Recovered), and the replacement is
// not an independent unit (TotalCases excludes it).
func TestFromResults_ReplacesRecovered(t *testing.T) {
	primary := &agent.TestCase{ID: "tc-1", Target: "/users", Method: "GET", Service: "api"}
	rep := &agent.TestCase{ID: "repair-tc-1", Target: "/users", Method: "GET", Service: "api", Replaces: "tc-1"}

	results := []agent.StepResult{
		{TestCase: primary, Status: agent.StepFailed},
		{TestCase: rep, Status: agent.StepPassed},
	}
	verdicts := []examiner.FinalVerdict{
		{Status: examiner.StatusFail, StepResult: results[0]},
		{Status: examiner.StatusPass, StepResult: results[1]},
	}

	s := session.FromResults("g", "http://x", 1, results, verdicts, 0, 0, 0)
	assert.Equal(t, 1, s.Recovered, "passed replacement recovers the primary")
	assert.Equal(t, 0, s.Failed, "primary reclassified out of Failed into Recovered")
	assert.Equal(t, 0, s.Passed, "replacement is not an independent pass unit")
	assert.Equal(t, 1, s.TotalCases, "replacement is not an independent unit")
}
