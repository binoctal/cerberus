package examiner

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/types"
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

// TestDeriveDimensionsProbeSetsExcluded verifies a sender negative-probe
// settles Dimension.Excluded: no echo ⇒ *true (sender excluded), echo ⇒ *false
// (server wrongly echoed to the sender).
func TestDeriveDimensionsProbeSetsExcluded(t *testing.T) {
	const sender = "c-web"
	tests := []struct {
		name       string
		probeMatch bool // probe outcome: did the sender receive its own broadcast?
		want       bool
	}{
		{"probe timed out - sender excluded", false, true},
		{"probe echoed - sender NOT excluded", true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			j := &Judge{}
			res := agent.StepResult{
				TestCase: &agent.TestCase{ID: "relay"},
				Evidence: []agent.Evidence{
					{Action: "ws_send", ConnectionID: sender, MatchedType: "workflow:task_progress"},
					{Action: "ws_receive", ConnectionID: "c-bridge", MatchedType: "workflow:task_progress", Matched: true},
					{Action: "ws_receive", ConnectionID: sender, MatchedType: "workflow:task_progress", Matched: tc.probeMatch, ExpectAbsent: true},
				},
			}
			dims := j.deriveDimensions(res)
			require.Len(t, dims, 1)
			require.NotNil(t, dims[0].Excluded, "Excluded must be set when a probe ran")
			assert.Equal(t, tc.want, *dims[0].Excluded)
		})
	}
}

// TestDeriveDimensionsNonSenderProbeIgnored verifies a negative probe on a
// non-sender connection does not settle Excluded (it is not a sender-exclusion
// signal); Excluded stays nil.
func TestDeriveDimensionsNonSenderProbeIgnored(t *testing.T) {
	j := &Judge{}
	res := agent.StepResult{
		TestCase: &agent.TestCase{ID: "relay"},
		Evidence: []agent.Evidence{
			{Action: "ws_send", ConnectionID: "c-web", MatchedType: "workflow:task_progress"},
			{Action: "ws_receive", ConnectionID: "c-bridge", MatchedType: "workflow:task_progress", Matched: true},
			// Probe on a recipient, not the sender - must be ignored.
			{Action: "ws_receive", ConnectionID: "c-bridge", MatchedType: "workflow:task_progress", Matched: false, ExpectAbsent: true},
		},
	}
	dims := j.deriveDimensions(res)
	require.Len(t, dims, 1)
	assert.Nil(t, dims[0].Excluded, "non-sender probe must not settle Excluded")
}

// TestRenderDimensions_NilExcludedIsNeutralScope locks the over-trigger fix:
// an unprobed Excluded renders as a neutral "not measured" scope note (not a
// gap-sounding "not probed"), and the guidance tells the judge an unmeasured
// sub-fact the claim does not reference must not lower confidence. Together
// these stop recipient-only claims (e.g. fanout) from drifting on an
// irrelevant sender-exclusion sub-fact (measured 2026-08-06).
func TestRenderDimensions_NilExcludedIsNeutralScope(t *testing.T) {
	out := renderDimensions([]types.Dimension{{
		Kind: "membership", Label: "broadcast recipients",
		Recipients: []string{"c-bridge", "c-web-2"}, Sender: "c-web", // Excluded nil
	}})
	assert.Contains(t, out, "sender-exclusion not measured",
		"nil Excluded must render as the neutral scope note")
	assert.NotContains(t, out, "not probed",
		"the old gap-sounding phrasing must be gone")

	assert.Contains(t, dimensionGuidance, "not measured",
		"guidance must reference the render's 'not measured' phrasing")
	assert.Contains(t, dimensionGuidance, "specifically requires",
		"guidance must scope uncertainty to sub-facts the claim requires")
}
