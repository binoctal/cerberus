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
	var d *types.Dimension
	for i := range dims {
		if dims[i].Kind == "membership" {
			d = &dims[i]
		}
	}
	require.NotNil(t, d, "membership dim missing: %+v", dims)
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
	var m *types.Dimension
	for i := range dims {
		if dims[i].Kind == "membership" {
			m = &dims[i]
		}
	}
	require.NotNil(t, m, "membership dim missing: %+v", dims)
	assert.Equal(t, "c-web", m.Sender)
	assert.ElementsMatch(t, []string{"c-bridge"}, m.Recipients)
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
	var m *types.Dimension
	for i := range dims {
		if dims[i].Kind == "membership" {
			m = &dims[i]
		}
	}
	require.NotNil(t, m, "membership dim missing: %+v", dims)
	assert.ElementsMatch(t, []string{"c-bridge"}, m.Recipients, "unmatched receive is not a recipient")
}

func TestDeriveDimensions_EmptyTrace(t *testing.T) {
	j := &Judge{}
	dims := j.deriveDimensions(agent.StepResult{TestCase: &agent.TestCase{ID: "x"}})
	assert.Empty(t, dims)
}

// TestDeriveDimensions_SendOnlyRecipientsNotProbed verifies that a sent type
// no test connection observed back is marked as NOT directly probed rather
// than left as a bare recipients=[] (which the judge misreads as a measured
// delivery failure — dogfood run 10, ws-realtime-wf-task-assign: the real
// bridge's task_started/task_progress/task_output round-trip frames were in
// the raw evidence yet the empty membership list sank the verdict).
func TestDeriveDimensions_SendOnlyRecipientsNotProbed(t *testing.T) {
	j := &Judge{}
	res := agent.StepResult{
		TestCase: &agent.TestCase{ID: "assign"},
		Evidence: []agent.Evidence{
			{Action: "ws_send", ConnectionID: "c-web", MatchedType: "workflow:task_assign"},
			// Unrelated type observed back; task_assign itself is send-only.
			{Action: "ws_receive", ConnectionID: "c-web", MatchedType: "workflow:task_progress", Matched: true},
		},
	}
	dims := j.deriveDimensions(res)
	var m *types.Dimension
	for i := range dims {
		if dims[i].Kind == "membership" && dims[i].Label == "workflow:task_assign recipients" {
			m = &dims[i]
		}
	}
	require.NotNil(t, m, "membership dim missing: %+v", dims)
	assert.Empty(t, m.Recipients)
	assert.Equal(t, recipientsNotProbedNote, m.Note)

	rendered := renderDimensions(dims)
	assert.Contains(t, rendered, recipientsNotProbedNote)

	// A type that WAS observed back carries no such note.
	for i := range dims {
		if dims[i].Kind == "membership" && dims[i].Label == "workflow:task_progress recipients" {
			assert.Empty(t, dims[i].Note)
		}
	}
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
			var m *types.Dimension
			for i := range dims {
				if dims[i].Kind == "membership" {
					m = &dims[i]
				}
			}
			require.NotNil(t, m, "membership dim missing: %+v", dims)
			require.NotNil(t, m.Excluded, "Excluded must be set when a probe ran")
			assert.Equal(t, tc.want, *m.Excluded)
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
	var m *types.Dimension
	for i := range dims {
		if dims[i].Kind == "membership" {
			m = &dims[i]
		}
	}
	require.NotNil(t, m, "membership dim missing: %+v", dims)
	assert.Nil(t, m.Excluded, "non-sender probe must not settle Excluded")
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

// TestDeriveDimensions_CountAndOrdering: positive receives accumulate a count
// dimension per (type, connection) and an ordering dimension when the same
// connection observed the same type at least twice. Count reports the
// observed total only — comparison against the claim belongs to the judge.
func TestDeriveDimensions_CountAndOrdering(t *testing.T) {
	j := &Judge{}
	res := agent.StepResult{
		TestCase: &agent.TestCase{ID: "batch"},
		Evidence: []agent.Evidence{
			{Action: "ws_receive", ConnectionID: "c1", MatchedType: "session:output", Matched: true,
				MatchedCount: 2, MatchedOrder: []string{"1", "2"}},
			{Action: "ws_receive", ConnectionID: "c1", MatchedType: "session:output", Matched: true,
				MatchedCount: 3, MatchedOrder: []string{"3", "4", "5"}},
			{Action: "ws_receive", ConnectionID: "c2", MatchedType: "session:output", Matched: true},
		},
	}
	dims := j.deriveDimensions(res)
	var c1, c2, ord *types.Dimension
	for i := range dims {
		switch {
		case dims[i].Kind == "count" && dims[i].Label == "session:output on c1":
			c1 = &dims[i]
		case dims[i].Kind == "count" && dims[i].Label == "session:output on c2":
			c2 = &dims[i]
		case dims[i].Kind == "ordering" && dims[i].Label == "session:output order on c1":
			ord = &dims[i]
		}
	}
	require.NotNil(t, c1, "c1 count dim missing: %+v", dims)
	assert.Equal(t, 5, c1.Count, "c1 accumulates 2+3 across receive steps")
	require.NotNil(t, c2, "c2 count dim missing")
	assert.Equal(t, 1, c2.Count, "single non-MatchAll match counts 1")
	require.NotNil(t, ord, "c1 ordering dim missing")
	assert.Equal(t, []string{"1", "2", "3", "4", "5"}, ord.Order, "orders concatenate in evidence order")
	for _, d := range dims {
		if d.Kind == "ordering" && d.Label == "session:output order on c2" {
			t.Fatal("single-frame observation must not derive an ordering dim (noise)")
		}
	}
}

// TestDeriveDimensions_ServerPushWithoutSender: count/ordering derive without
// any ws_send — server-pushed types (e.g. batched session:output) have no
// sender in the trace. Membership stays absent; the early-return that used to
// key on senders must not swallow these facts.
func TestDeriveDimensions_ServerPushWithoutSender(t *testing.T) {
	j := &Judge{}
	res := agent.StepResult{
		TestCase: &agent.TestCase{ID: "push"},
		Evidence: []agent.Evidence{
			{Action: "ws_receive", ConnectionID: "c1", MatchedType: "session:output", Matched: true,
				MatchedCount: 2, MatchedOrder: []string{"1", "2"}},
		},
	}
	dims := j.deriveDimensions(res)
	require.Len(t, dims, 2, "count + ordering, no membership: %+v", dims)
	assert.Equal(t, "count", dims[0].Kind)
	assert.Equal(t, "ordering", dims[1].Kind)
}

// TestDeriveDimensions_PlaceholderOrderNote: a positional (placeholder)
// ordering list carries a Note so the judge does not compare "#0"/"#1" as ids.
func TestDeriveDimensions_PlaceholderOrderNote(t *testing.T) {
	j := &Judge{}
	res := agent.StepResult{
		TestCase: &agent.TestCase{ID: "push"},
		Evidence: []agent.Evidence{
			{Action: "ws_receive", ConnectionID: "c1", MatchedType: "session:output", Matched: true,
				MatchedCount: 2, MatchedOrder: []string{"#0", "#1"}},
		},
	}
	dims := j.deriveDimensions(res)
	for _, d := range dims {
		if d.Kind == "ordering" {
			assert.Contains(t, d.Note, "positional",
				"placeholder order must be labeled positional: %+v", d)
			return
		}
	}
	t.Fatal("no ordering dim derived")
}
