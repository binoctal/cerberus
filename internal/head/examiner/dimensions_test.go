package examiner

import (
	"testing"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeriveDimensions_FanOutMembership verifies a ws_flow trace yields one
// membership dimension per broadcast type, with recipients and sender.
func TestDeriveDimensions_FanOutMembership(t *testing.T) {
	j := &Judge{}
	res := agent.StepResult{
		TestCase: &agent.TestCase{ID: "relay"},
		Evidence: []agent.Evidence{
			{Action: "ws_send", ConnectionID: "c-web", MatchedType: "workflow:task_progress"},
			{Action: "ws_receive", ConnectionID: "c-bridge", MatchedType: "workflow:task_progress", Matched: true},
			{Action: "ws_receive", ConnectionID: "c-web-2", MatchedType: "workflow:task_progress", Matched: true},
		},
	}
	dims := j.deriveDimensions(res)
	require.Len(t, dims, 1)
	d := dims[0]
	assert.Equal(t, "membership", d.Kind)
	assert.Equal(t, "c-web", d.Sender)
	assert.ElementsMatch(t, []string{"c-bridge", "c-web-2"}, d.Recipients)
	assert.Nil(t, d.Excluded, "exclusion not probed this spec")
}

// TestDeriveDimensions_SingleRecipientIsStillMembership verifies one sender +
// one recipient still yields a membership fact (the recipient set).
func TestDeriveDimensions_SingleRecipientIsStillMembership(t *testing.T) {
	j := &Judge{}
	res := agent.StepResult{
		TestCase: &agent.TestCase{ID: "x"},
		Evidence: []agent.Evidence{
			{Action: "ws_send", ConnectionID: "c-web", MatchedType: "workflow:task_progress"},
			{Action: "ws_receive", ConnectionID: "c-bridge", MatchedType: "workflow:task_progress", Matched: true},
		},
	}
	dims := j.deriveDimensions(res)
	require.Len(t, dims, 1)
	assert.Equal(t, "c-web", dims[0].Sender)
	assert.ElementsMatch(t, []string{"c-bridge"}, dims[0].Recipients)
}

// TestDeriveDimensions_UnmatchedReceiveExcluded verifies a ws_receive that did
// NOT match is not counted as a recipient.
func TestDeriveDimensions_UnmatchedReceiveExcluded(t *testing.T) {
	j := &Judge{}
	res := agent.StepResult{
		TestCase: &agent.TestCase{ID: "x"},
		Evidence: []agent.Evidence{
			{Action: "ws_send", ConnectionID: "c-web", MatchedType: "session:send"},
			{Action: "ws_receive", ConnectionID: "c-bridge", MatchedType: "session:send", Matched: true},
			{Action: "ws_receive", ConnectionID: "c-web-2", MatchedType: "session:send", Matched: false},
		},
	}
	dims := j.deriveDimensions(res)
	require.Len(t, dims, 1)
	assert.ElementsMatch(t, []string{"c-bridge"}, dims[0].Recipients, "unmatched receive is not a recipient")
}

func TestDeriveDimensions_EmptyTrace(t *testing.T) {
	j := &Judge{}
	dims := j.deriveDimensions(agent.StepResult{TestCase: &agent.TestCase{ID: "x"}})
	assert.Empty(t, dims)
}
