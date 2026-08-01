package protocoldiscover

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

func TestExtractWindows_FoundAndAbsent(t *testing.T) {
	corpus := "line0\nline1 devices:sync here\nline2\nline3\nline4\nline5"
	ws := extractWindows(corpus, []string{"devices:sync", "nope"}, 1)
	require.Len(t, ws, 1)
	assert.Equal(t, "devices:sync", ws[0].literal)
	assert.Equal(t, "line0\nline1 devices:sync here\nline2", ws[0].text)
}

func TestExtractWindows_RadiusClamps(t *testing.T) {
	ws := extractWindows("top here\nb\nc", []string{"top"}, 5)
	require.Len(t, ws, 1)
	assert.Equal(t, "top here\nb\nc", ws[0].text)
}

func TestExtractWindows_Dedups(t *testing.T) {
	ws := extractWindows("a x b\n c \n x again", []string{"x", "x"}, 0)
	require.Len(t, ws, 1, "duplicate literal deduped to one window")
}

func TestExtractWindows_EmptyLiteralSkipped(t *testing.T) {
	ws := extractWindows("body", []string{""}, 2)
	assert.Empty(t, ws)
}

func TestConfirmSignalsTool_Schema(t *testing.T) {
	tool := confirmSignalsTool()
	assert.Equal(t, "confirm_signals", tool.Name)
	props := tool.InputSchema["properties"].(map[string]any)
	hs := props["handshake"].(map[string]any)["properties"].(map[string]any)
	assert.Contains(t, hs, "present")
	assert.Contains(t, hs, "await_type")
	batch := props["batch"].(map[string]any)["properties"].(map[string]any)
	for _, f := range []string{"present", "flush_key", "item_type", "items_path"} {
		assert.Contains(t, batch, f)
	}
}

func TestBuildConfirmPrompt_ContainsWindowsAndSteer(t *testing.T) {
	p := buildConfirmPrompt([]signalWindow{{literal: "devices:sync", text: "if (onlineDevices.length > 0) ws.send({type:'devices:sync'})"}})
	assert.Contains(t, p, "ANCHORED SOURCE WINDOWS")
	assert.Contains(t, p, "devices:sync")
	assert.Contains(t, p, "guarded")
	// items_path must be the full frame-root dotted path, not a buffer var.
	assert.Contains(t, p, "FRAME ROOT")
	assert.Contains(t, p, "batch.lines", "prompt must show the buffer-variable anti-example")
}

func TestRefineSignals_ParsesConfirmation(t *testing.T) {
	draft := &project.Protocol{
		Roles: map[string]*project.ProtocolRole{"web": {Handshake: &project.RoleHandshake{AwaitType: "device:online"}}},
	}
	corpus := "if (onlineDevices.length > 0) ws.send type devices:sync\nbroadcastToWeb type device:online"
	mock := llm.NewMockClient(nil)
	mock.SetToolResponse("ANCHORED SOURCE WINDOWS", []llm.ToolCall{{Name: "confirm_signals", Input: map[string]any{
		"handshake": map[string]any{"present": true, "await_type": "devices:sync"},
		"batch":     map[string]any{"present": false},
	}}})
	driver := ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000))

	conf, failed := refineSignals(context.Background(), driver, draft, []SourceFile{{Content: corpus}})
	require.False(t, failed)
	assert.True(t, conf.handshakePresent)
	assert.Equal(t, "devices:sync", conf.awaitType)
}

func TestRefineSignals_NoCandidatesIsAbsent(t *testing.T) {
	draft := &project.Protocol{Roles: map[string]*project.ProtocolRole{"web": {Handshake: &project.RoleHandshake{AwaitType: "invented"}}}}
	conf, failed := refineSignals(context.Background(), ai.NewDriver(llm.NewMockClient(nil), ai.NewTokenBudget(1, 1)), draft, []SourceFile{{Content: "unrelated"}})
	require.False(t, failed)
	assert.False(t, conf.handshakePresent)
}

func TestMergeConfirmation_KeepsGroundedHandshakeDropsUngrounded(t *testing.T) {
	p := &project.Protocol{Roles: map[string]*project.ProtocolRole{"web": {Handshake: &project.RoleHandshake{AwaitType: "device:online", Timeout: 5}}}}
	mergeConfirmation(p, signalConfirmation{handshakePresent: true, awaitType: "devices:sync"})
	require.Contains(t, p.Roles, "web")
	assert.Equal(t, "devices:sync", p.Roles["web"].Handshake.AwaitType)
	assert.Equal(t, 5, p.Roles["web"].Handshake.Timeout, "timeout preserved from pass 1")

	mergeConfirmation(p, signalConfirmation{handshakePresent: false})
	assert.Nil(t, p.Roles["web"].Handshake, "unconfirmed handshake dropped")
}

func TestMergeConfirmation_Batch(t *testing.T) {
	p := &project.Protocol{}
	mergeConfirmation(p, signalConfirmation{batchPresent: true, flushKey: "session:output-batch", itemType: "session:output", itemsPath: "payload.lines"})
	require.Contains(t, p.Batches, "session:output-batch")
	assert.Equal(t, "payload.lines", p.Batches["session:output-batch"].ItemsPath)

	mergeConfirmation(p, signalConfirmation{batchPresent: false})
	assert.Empty(t, p.Batches)
}

func TestCoherentBatch_FlushKeyAndLeafCooccur(t *testing.T) {
	corpus := "case 'session:output': batchOutput\n...\nflushBatch emits:\n  type: 'session:output-batch', payload: { lines: batch.lines }\n"
	assert.True(t, coherentBatch(corpus, "session:output-batch", "payload.lines"))
}

func TestCoherentBatch_LeafFarFromFlushKey(t *testing.T) {
	// devices lives in a different function from the session:output-batch flush
	// (the Run-24 mis-fire: pass 2 leaked a handshake literal into the batch).
	corpus := "sendOnlineDevices:\n  payload: { devices: onlineDevices }\n\n... many lines ...\n\nflushBatch:\n  type: 'session:output-batch', payload: { lines: batch.lines }\n"
	assert.False(t, coherentBatch(corpus, "session:output-batch", "payload.devices"),
		"a batch whose items_path leaf does not co-occur with the flush key is incoherent")
}

func TestCoherentBatch_FlushKeyAbsent(t *testing.T) {
	assert.False(t, coherentBatch("unrelated", "session:output-batch", "payload.lines"))
}

func TestParseConfirmation_DropsIncoherentBatch(t *testing.T) {
	corpus := "sendOnlineDevices payload devices\n\nflushBatch type session:output-batch payload lines"
	input := map[string]any{
		"handshake": map[string]any{"present": false},
		"batch": map[string]any{
			"present":    true,
			"flush_key":  "session:output-batch",
			"item_type":  "devices:sync",    // leaked handshake literal
			"items_path": "payload.devices", // leaf not near the flush key
		},
	}
	c := parseConfirmation(input, corpus)
	assert.False(t, c.batchPresent, "incoherent batch (handshake literal leaked) must be dropped")
}

func TestHasHardStructure(t *testing.T) {
	assert.False(t, hasHardStructure(&project.Protocol{}))
	assert.True(t, hasHardStructure(&project.Protocol{Roles: map[string]*project.ProtocolRole{"w": {Handshake: &project.RoleHandshake{}}}}))
	assert.True(t, hasHardStructure(&project.Protocol{Batches: map[string]*project.ProtocolBatch{"b": {}}}))
}
