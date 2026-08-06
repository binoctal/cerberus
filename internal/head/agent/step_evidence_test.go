package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/types"
)

func TestStepEvidenceReceiveCarriesConnectionTypeMatch(t *testing.T) {
	s := TestStep{Action: "ws_receive", ConnectionID: "c-web", Type: "workflow:task_progress"}
	res := types.WSResult{OK: true, MatchedMessage: `{"type":"workflow:task_progress"}`, MatchedCount: 1}
	ev := stepEvidence(s, res)
	assert.Equal(t, "ws_receive", ev.Action)
	assert.Equal(t, "c-web", ev.ConnectionID)
	assert.Equal(t, "workflow:task_progress", ev.MatchedType)
	assert.True(t, ev.Matched)
}

func TestStepEvidenceSendParsesType(t *testing.T) {
	s := TestStep{Action: "ws_send", ConnectionID: "c-web", Message: `{"type":"session:start","payload":{}}`}
	ev := stepEvidence(s, types.WSResult{OK: true})
	assert.Equal(t, "session:start", ev.MatchedType)
	assert.Equal(t, "c-web", ev.ConnectionID)
	assert.False(t, ev.Matched, "ws_send is never a receive match")
}

func TestStepEvidenceConnectNoMatchedType(t *testing.T) {
	s := TestStep{Action: "ws_connect", ConnectionID: "c-bridge", Role: "bridge"}
	ev := stepEvidence(s, types.WSResult{OK: true})
	assert.Equal(t, "c-bridge", ev.ConnectionID)
	assert.Empty(t, ev.MatchedType)
	assert.False(t, ev.Matched)
}
