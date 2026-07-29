package session

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/binoctal/cerberus/internal/memory"
)

// TestComputeStuck_SameHintRefailMarksStuck: a replacement that re-fails with
// the SAME RedispatchHint as its predecessor makes no progress — its target is
// marked stuck and dropped from later rounds. A hint CHANGE (drift->auth) is
// progress and stays eligible. computeStuck is a pure function over verdicts,
// so this test needs no LLM/executor harness.
func TestComputeStuck_SameHintRefailMarksStuck(t *testing.T) {
	// Two target histories:
	//  /users: original fail(drift) tc-1 + replacement fail(drift) tc-2 -> stuck
	//  /login: original fail(drift) tc-3 + replacement fail(auth)  tc-4 -> eligible
	verdicts := []examiner.FinalVerdict{
		{
			Status:         examiner.StatusFail,
			RedispatchHint: agent.HintEndpointDrift,
			StepResult: agent.StepResult{
				TestCase: &agent.TestCase{ID: "tc-1", Target: "/users", Method: "GET"},
			},
		},
		{
			Status:         examiner.StatusFail,
			RedispatchHint: agent.HintEndpointDrift, // same hint as predecessor -> no progress
			StepResult: agent.StepResult{
				TestCase: &agent.TestCase{ID: "tc-2", Target: "/users", Method: "GET", Replaces: "tc-1"},
			},
		},
		{
			Status:         examiner.StatusFail,
			RedispatchHint: agent.HintEndpointDrift,
			StepResult: agent.StepResult{
				TestCase: &agent.TestCase{ID: "tc-3", Target: "/login", Method: "POST"},
			},
		},
		{
			Status:         examiner.StatusFail,
			RedispatchHint: agent.HintAuth, // hint changed (drift->auth) -> progress
			StepResult: agent.StepResult{
				TestCase: &agent.TestCase{ID: "tc-4", Target: "/login", Method: "POST", Replaces: "tc-3"},
			},
		},
	}

	stuck := computeStuck(verdicts)

	// /users is stuck (same-hint re-fail); /login is not (hint changed).
	assert.True(t, stuck[memory.NormalizeTarget("/users")], "/users must be stuck after same-hint re-fail")
	assert.False(t, stuck[memory.NormalizeTarget("/login")], "/login must NOT be stuck after hint change")
	assert.Len(t, stuck, 1, "exactly one stuck target")
}
