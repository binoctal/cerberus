package protocoldiscover

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

func TestInfer_ReturnsValidatedProtocol(t *testing.T) {
	cfg := &project.Config{
		Services: []project.Service{{Name: "rt", URL: "http://x"}},
		Actors:   []project.Actor{{Name: "web"}},
	}
	driver := mockToolDriver(t, map[string]any{
		"found":     true,
		"framing":   "json",
		"type_path": "type",
		"auth":      map[string]any{"strategy": "query", "param": "token", "credential_ref": "web"},
		"notes":     "ok",
	})
	p, err := Infer(context.Background(), driver, cfg, "rt", []SourceFile{{Path: "docs.md", Content: "..."}}, 1)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "json", p.Framing)
	assert.Equal(t, "type", p.TypePath)
	require.NotNil(t, p.Auth)
	assert.Equal(t, "web", p.Auth.CredentialRef)
}

func TestInfer_FoundFalse_ReturnsErrNoProtocol(t *testing.T) {
	driver := mockToolDriver(t, map[string]any{"found": false})
	_, err := Infer(context.Background(), driver, cfgWithService(), "rt", nil, 1)
	assert.ErrorIs(t, err, ErrNoProtocol)
}

func TestInfer_InvalidCredentialRefFailsValidation(t *testing.T) {
	driver := mockToolDriver(t, map[string]any{
		"found": true, "framing": "json",
		"auth": map[string]any{"strategy": "query", "param": "token", "credential_ref": "ghost"},
	})
	_, err := Infer(context.Background(), driver, cfgWithService(), "rt", nil, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match any actor")
}

// TestInfer_RolesPopulated exercises the roles mapping path. Without the
// p.Roles initialization this would panic on the nil-map write, so this test
// guards the fix for that latent bug (the model returned roles but the brief's
// original code constructed p with a nil Roles map).
func TestInfer_RolesPopulated(t *testing.T) {
	cfg := cfgWithService()
	driver := mockToolDriver(t, map[string]any{
		"found":     true,
		"framing":   "json",
		"type_path": "type",
		"auth":      map[string]any{"strategy": "query", "param": "token", "credential_ref": "web"},
		"roles": map[string]any{
			"web": map[string]any{
				"credential_ref": "web",
				"params":         map[string]any{"type": "web"},
			},
		},
	})
	p, err := Infer(context.Background(), driver, cfg, "rt", nil, 1)
	require.NoError(t, err)
	require.NotNil(t, p)
	require.Contains(t, p.Roles, "web")
	role := p.Roles["web"]
	assert.Equal(t, "web", role.CredentialRef)
	assert.Equal(t, map[string]string{"type": "web"}, role.Params)
}

// TestInfer_ZeroToolCalls_IsHardErrorNotErrNoProtocol is the negative-verification
// guard for drift: when the model returns no tool call, Infer must surface a hard
// error rather than collapsing into the clean not-found path. This RED-fails if
// a future change reports drift as ErrNoProtocol.
func TestInfer_ZeroToolCalls_IsHardErrorNotErrNoProtocol(t *testing.T) {
	driver := mockEmptyDriver(t)
	_, err := Infer(context.Background(), driver, cfgWithService(), "rt", nil, 1)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrNoProtocol, "drift (zero tool calls) is a hard error, not a clean not-found")
}

// TestInfer_InvalidCredentialRef_IsHardError guards the validation-failure path:
// the tool call parsed but referenced a nonexistent actor. The error must carry
// the validation cause, must NOT be ErrNoProtocol, and must not leak the raw
// tool input.
func TestInfer_InvalidCredentialRef_IsHardError(t *testing.T) {
	driver := mockToolDriver(t, map[string]any{
		"found":     true,
		"framing":   "json",
		"type_path": "type",
		"auth":      map[string]any{"strategy": "query", "param": "token", "credential_ref": "ghost"},
	})
	_, err := Infer(context.Background(), driver, cfgWithService(), "rt", nil, 1)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrNoProtocol)
	assert.NotContains(t, err.Error(), "raw", "error must not leak raw LLM response")
}

func cfgWithService() *project.Config {
	return &project.Config{
		Services: []project.Service{{Name: "rt", URL: "http://x"}},
		Actors:   []project.Actor{{Name: "web"}},
	}
}

// sampleFromInput drives inferOnce with a single-fixture mock preset to `input`
// (returned for every call). It is the per-sample classifier test helper.
func sampleFromInput(t *testing.T, cfg *project.Config, input map[string]any) sample {
	t.Helper()
	return inferOnce(context.Background(), mockToolDriver(t, input), cfg, "rt", nil)
}

func TestInferOnce_Found(t *testing.T) {
	s := sampleFromInput(t, cfgWithService(), map[string]any{
		"found":     true,
		"framing":   "json",
		"type_path": "type",
		"auth":      map[string]any{"strategy": "query", "param": "token", "credential_ref": "web"},
	})
	assert.Equal(t, outcomeFound, s.outcome)
	require.NotNil(t, s.proto)
	assert.Equal(t, "json", s.proto.Framing)
}

func TestInferOnce_NotFound(t *testing.T) {
	s := sampleFromInput(t, cfgWithService(), map[string]any{"found": false})
	assert.Equal(t, outcomeNotFound, s.outcome)
}

func TestInferOnce_Drift(t *testing.T) {
	// No tool fixture -> zero tool calls -> drift.
	s := inferOnce(context.Background(), mockEmptyDriver(t), cfgWithService(), "rt", nil)
	assert.Equal(t, outcomeFailed, s.outcome)
	assert.Equal(t, reasonDrift, s.reason)
}

func TestInferOnce_ParseError(t *testing.T) {
	// found=true but the tool args cannot round-trip into inferOutput: a string
	// (not a map) under roles breaks json.Unmarshal -> parse failure.
	s := sampleFromInput(t, cfgWithService(), map[string]any{
		"found": true,
		"roles": map[string]any{"web": "not-a-map"},
	})
	assert.Equal(t, outcomeFailed, s.outcome)
	assert.Equal(t, reasonParse, s.reason)
}

func TestInferOnce_Invalid(t *testing.T) {
	s := sampleFromInput(t, cfgWithService(), map[string]any{
		"found": true,
		"auth":  map[string]any{"strategy": "query", "param": "token", "credential_ref": "ghost"},
	})
	assert.Equal(t, outcomeFailed, s.outcome)
	assert.Equal(t, reasonInvalid, s.reason)
}

// mockToolDriver presets a single protocol_draft tool call returned regardless
// of prompt (key "default" matches any prompt per MockClient.matchKey).
func mockToolDriver(t *testing.T, input map[string]any) *ai.Driver {
	t.Helper()
	mock := llm.NewMockClient(nil)
	mock.SetToolResponse("default", []llm.ToolCall{
		{Name: "protocol_draft", Input: input},
	})
	return ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000))
}

// mockEmptyDriver returns no tool calls (simulates drift).
func mockEmptyDriver(t *testing.T) *ai.Driver {
	t.Helper()
	mock := llm.NewMockClient(nil) // no SetToolResponse -> empty ToolCalls
	return ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000))
}

// mockSequenceDriver presets a rotating tool-call sequence (returned across
// successive calls with any prompt), so a single Infer N-sample run observes
// variance. Each element is the protocol_draft payload for one sample.
func mockSequenceDriver(t *testing.T, sequence []map[string]any) *ai.Driver {
	t.Helper()
	calls := make([][]llm.ToolCall, len(sequence))
	for i, in := range sequence {
		calls[i] = []llm.ToolCall{{Name: "protocol_draft", Input: in}}
	}
	mock := llm.NewMockClient(nil)
	mock.SetToolResponseSequence("default", calls)
	return ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000))
}

func validInput() map[string]any {
	return map[string]any{
		"found":     true,
		"framing":   "json",
		"type_path": "type",
		"auth":      map[string]any{"strategy": "query", "param": "token", "credential_ref": "web"},
	}
}

// TestInfer_Voting_AbsorbsFalseNegative: one false-negative + two good drafts
// -> the result is a protocol, NOT ErrNoProtocol. The false-negative tail is
// outvoted.
func TestInfer_Voting_AbsorbsFalseNegative(t *testing.T) {
	driver := mockSequenceDriver(t, []map[string]any{
		{"found": false}, // false negative
		validInput(),     // good
		validInput(),     // good
	})
	p, err := Infer(context.Background(), driver, cfgWithService(), "rt", nil, 3)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "json", p.Framing)
}

// TestInfer_Voting_AbsorbsParseFailure: one malformed-args parse failure is
// skipped; the two good drafts carry the result.
func TestInfer_Voting_AbsorbsParseFailure(t *testing.T) {
	driver := mockSequenceDriver(t, []map[string]any{
		{"found": true, "roles": map[string]any{"web": "not-a-map"}}, // parse fail
		validInput(),
		validInput(),
	})
	p, err := Infer(context.Background(), driver, cfgWithService(), "rt", nil, 3)
	require.NoError(t, err)
	require.NotNil(t, p)
}

// TestInfer_Voting_PicksHigherScored: a draft covering more roles beats a
// thinner one when both are present (scoreProtocol rewards role coverage).
// Avoids handshake/batch so pass 2 is not engaged — this test is about voting.
func TestInfer_Voting_PicksHigherScored(t *testing.T) {
	partial := map[string]any{
		"found": true, "framing": "json", "type_path": "type",
		"auth":  map[string]any{"strategy": "query", "param": "token", "credential_ref": "web"},
		"roles": map[string]any{"web": map[string]any{"credential_ref": "web"}},
	}
	complete := validInput()
	complete["roles"] = map[string]any{
		"web":    map[string]any{"credential_ref": "web"},
		"bridge": map[string]any{"credential_ref": ""},
	}
	driver := mockSequenceDriver(t, []map[string]any{partial, complete, partial})
	p, err := Infer(context.Background(), driver, cfgWithService(), "rt", nil, 3)
	require.NoError(t, err)
	require.NotNil(t, p)
	require.Contains(t, p.Roles, "bridge", "must pick the complete (more-roles) draft")
}

// TestInfer_Voting_AllNotFoundIsErrNoProtocol: unanimous found=false is still a
// clean not-found, not a hard error. (Infer retries once on unanimous false; the
// mock holds the last element, so the retry is also unanimous false.)
func TestInfer_Voting_AllNotFoundIsErrNoProtocol(t *testing.T) {
	driver := mockSequenceDriver(t, []map[string]any{
		{"found": false}, {"found": false}, {"found": false},
	})
	_, err := Infer(context.Background(), driver, cfgWithService(), "rt", nil, 3)
	assert.ErrorIs(t, err, ErrNoProtocol)
}

// TestInfer_RetriesOnUnanimousNotFound: when every sample in the first batch
// returns found=false, Infer tries one more batch before concluding — a real
// protocol target where all samples give up is variance, not absence. The retry
// finds the protocol.
func TestInfer_RetriesOnUnanimousNotFound(t *testing.T) {
	driver := mockSequenceDriver(t, []map[string]any{
		{"found": false}, {"found": false}, {"found": false}, // attempt 0
		validInput(), validInput(), validInput(), // attempt 1
	})
	p, err := Infer(context.Background(), driver, cfgWithService(), "rt", nil, 3)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "json", p.Framing)
}

// TestInfer_Voting_AllFailedIsHardError: unanimous failure is a hard error with
// reason counts, NOT ErrNoProtocol, and leaks no raw payload.
func TestInfer_Voting_AllFailedIsHardError(t *testing.T) {
	driver := mockSequenceDriver(t, []map[string]any{
		{"found": true, "roles": map[string]any{"web": "not-a-map"}}, // parse
		{"found": true, "roles": map[string]any{"web": "not-a-map"}}, // parse
	})
	_, err := Infer(context.Background(), driver, cfgWithService(), "rt", nil, 2)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrNoProtocol)
	assert.Contains(t, err.Error(), "parse")
}

// TestInfer_SamplesClampsToOne: samples < 1 is single-shot and still works.
func TestInfer_SamplesClampsToOne(t *testing.T) {
	driver := mockToolDriver(t, validInput())
	p, err := Infer(context.Background(), driver, cfgWithService(), "rt", nil, 0)
	require.NoError(t, err)
	require.NotNil(t, p)
}

// mockTwoPassDriver presets pass-1 (protocol_draft) and pass-2 (confirm_signals)
// fixtures. pass1 routes on the pass-1 prompt substring; pass2 on the pass-2
// "ANCHORED SOURCE WINDOWS" substring.
func mockTwoPassDriver(t *testing.T, pass1, pass2 map[string]any) *ai.Driver {
	t.Helper()
	mock := llm.NewMockClient(nil)
	mock.SetToolResponse("drafting a WebSocket protocol description", []llm.ToolCall{{Name: "protocol_draft", Input: pass1}})
	mock.SetToolResponse("ANCHORED SOURCE WINDOWS", []llm.ToolCall{{Name: "confirm_signals", Input: pass2}})
	return ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000))
}

// TestInferOnce_TwoPassLandsGroundedAwaitType: pass 1 named the wrong literal
// (device:online); pass 2 selects the guarded devices:sync off the windows.
func TestInferOnce_TwoPassLandsGroundedAwaitType(t *testing.T) {
	corpus := "setTimeout connect\nif (onlineDevices.length > 0) ws.send type devices:sync\nbroadcastToWeb type device:online"
	pass1 := map[string]any{
		"found": true, "framing": "json", "type_path": "type",
		"roles": map[string]any{"web": map[string]any{
			"credential_ref": "web",
			"handshake":      map[string]any{"await_type": "device:online", "timeout": 5},
		}},
	}
	pass2 := map[string]any{
		"handshake": map[string]any{"present": true, "await_type": "devices:sync"},
		"batch":     map[string]any{"present": false},
	}
	driver := mockTwoPassDriver(t, pass1, pass2)
	s := inferOnce(context.Background(), driver, cfgWithService(), "rt", []SourceFile{{Content: corpus}})
	assert.Equal(t, outcomeFound, s.outcome)
	require.NotNil(t, s.proto)
	require.NotNil(t, s.proto.Roles["web"].Handshake)
	assert.Equal(t, "devices:sync", s.proto.Roles["web"].Handshake.AwaitType, "pass 2 overrode the wrong pass-1 literal")
}

// TestInferOnce_TwoPassDropsUnconfirmedHandshake: pass 2 says no guarded send
// -> handshake dropped, sample still Found.
func TestInferOnce_TwoPassDropsUnconfirmedHandshake(t *testing.T) {
	corpus := "broadcastToWeb type device:online"
	pass1 := map[string]any{
		"found": true, "framing": "json", "type_path": "type",
		"roles": map[string]any{"web": map[string]any{
			"credential_ref": "web",
			"handshake":      map[string]any{"await_type": "device:online", "timeout": 5},
		}},
	}
	pass2 := map[string]any{
		"handshake": map[string]any{"present": false},
		"batch":     map[string]any{"present": false},
	}
	driver := mockTwoPassDriver(t, pass1, pass2)
	s := inferOnce(context.Background(), driver, cfgWithService(), "rt", []SourceFile{{Content: corpus}})
	assert.Equal(t, outcomeFound, s.outcome)
	assert.Nil(t, s.proto.Roles["web"].Handshake, "unconfirmed handshake dropped")
}

// TestInferOnce_TwoPassInventsNoCandidate: pass-1 await_type is not in the
// corpus -> no window -> pass 2 not called -> handshake dropped (absent, not
// failure), sample Found.
func TestInferOnce_TwoPassInventsNoCandidate(t *testing.T) {
	pass1 := map[string]any{
		"found": true, "framing": "json", "type_path": "type",
		"roles": map[string]any{"web": map[string]any{
			"credential_ref": "web",
			"handshake":      map[string]any{"await_type": "totally-made-up", "timeout": 5},
		}},
	}
	// Only a pass-1 fixture; pass 2 must not be reached.
	mock := llm.NewMockClient(nil)
	mock.SetToolResponse("drafting a WebSocket protocol description", []llm.ToolCall{{Name: "protocol_draft", Input: pass1}})
	driver := ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000))
	s := inferOnce(context.Background(), driver, cfgWithService(), "rt", []SourceFile{{Content: "unrelated source"}})
	assert.Equal(t, outcomeFound, s.outcome)
	assert.Nil(t, s.proto.Roles["web"].Handshake, "invented await_type has no window -> handshake dropped")
}

// TestInferOnce_TwoPassAddsStructuresWhenPass1Omitted: the recall fix. Pass 1
// emitted only envelope+auth+roles (no handshake/batch), but the corpus carries
// the literals; pass 2 anchors off corpus-seeded windows and the confirmed
// handshake + batch are ADDED to the draft.
func TestInferOnce_TwoPassAddsStructuresWhenPass1Omitted(t *testing.T) {
	corpus := "if (peers.length > 0) ws.send({type: 'devices:sync'})\nsetTimeout flush {type: 'session:output-batch', payload: {lines: batch.lines}}"
	pass1 := map[string]any{
		"found": true, "framing": "json", "type_path": "type",
		"auth":  map[string]any{"strategy": "query", "param": "token", "credential_ref": "web"},
		"roles": map[string]any{"web": map[string]any{"credential_ref": "web", "params": map[string]any{"type": "web"}}},
	}
	pass2 := map[string]any{
		"handshake": map[string]any{"present": true, "await_type": "devices:sync"},
		"batch":     map[string]any{"present": true, "flush_key": "session:output-batch", "item_type": "session:output", "items_path": "payload.lines"},
	}
	driver := mockTwoPassDriver(t, pass1, pass2)
	s := inferOnce(context.Background(), driver, cfgWithService(), "rt", []SourceFile{{Content: corpus}})
	assert.Equal(t, outcomeFound, s.outcome)
	require.NotNil(t, s.proto)
	require.NotNil(t, s.proto.Roles["web"].Handshake, "handshake added though pass 1 omitted it")
	assert.Equal(t, "devices:sync", s.proto.Roles["web"].Handshake.AwaitType)
	// Deterministic handshake fallback: pass 1 omitted the handshake, pass 2
	// declines it, but the corpus shows a guarded send -> the code-side
	// detector attaches it. This is the recall fix that does not depend on the
	// LLM judging the guarded send.
	corpus2 := "if (onlineDevices.length > 0) {\n  ws.send(JSON.stringify({ type: 'devices:sync' }))\n}\n"
	pass1b := map[string]any{
		"found": true, "framing": "json", "type_path": "type",
		"auth":  map[string]any{"strategy": "query", "param": "token", "credential_ref": "web"},
		"roles": map[string]any{"web": map[string]any{"credential_ref": "web", "params": map[string]any{"type": "web"}}},
	}
	pass2b := map[string]any{
		"handshake": map[string]any{"present": false},
		"batch":     map[string]any{"present": false},
	}
	driver2 := mockTwoPassDriver(t, pass1b, pass2b)
	s2 := inferOnce(context.Background(), driver2, cfgWithService(), "rt", []SourceFile{{Content: corpus2}})
	require.Equal(t, outcomeFound, s2.outcome)
	require.NotNil(t, s2.proto.Roles["web"].Handshake, "guarded-send fallback attached a handshake pass 2 declined")
	assert.Equal(t, "devices:sync", s2.proto.Roles["web"].Handshake.AwaitType)
	assert.True(t, s2.proto.Roles["web"].Handshake.Optional)
}

// TestCorrectRoleDiscriminators_FixesBridgeValue: a bridge role whose
// discriminator the model set to "web" (a sibling role name) is corrected to
// "bridge"; a correct web role is untouched.
func TestCorrectRoleDiscriminators_FixesBridgeValue(t *testing.T) {
	p := &project.Protocol{Roles: map[string]*project.ProtocolRole{
		"web":    {Params: map[string]string{"type": "web"}},
		"bridge": {Params: map[string]string{"type": "web"}}, // wrong value
	}}
	correctRoleDiscriminators(p)
	assert.Equal(t, "web", p.Roles["web"].Params["type"], "correct web discriminator untouched")
	assert.Equal(t, "bridge", p.Roles["bridge"].Params["type"], "wrong bridge discriminator corrected")
}

// TestCorrectRoleDiscriminators_PreservesLegitValue: a discriminator that does
// NOT name a sibling role (e.g. type="browser") is a legitimate value and must
// NOT be overwritten — guards the over-correction risk flagged in review.
func TestCorrectRoleDiscriminators_PreservesLegitValue(t *testing.T) {
	p := &project.Protocol{Roles: map[string]*project.ProtocolRole{
		"web": {Params: map[string]string{"type": "browser"}},
	}}
	correctRoleDiscriminators(p)
	assert.Equal(t, "browser", p.Roles["web"].Params["type"], "legitimate non-sibling value must not be overwritten")
}

func TestScoreProtocol_CompleteBeatsPartial(t *testing.T) {
	complete := &project.Protocol{
		Framing: "json", TypePath: "type",
		Auth: &project.ProtocolAuth{CredentialRef: "web"},
		Roles: map[string]*project.ProtocolRole{
			"web": {Handshake: &project.RoleHandshake{AwaitType: "devices:sync"}},
		},
		Batches: map[string]*project.ProtocolBatch{"session:output-batch": {ItemType: "session:output"}},
	}
	partial := &project.Protocol{
		Framing: "json", TypePath: "type",
		Roles: map[string]*project.ProtocolRole{"web": {}},
	}
	// Pass matching modal fields so the consensus bonus does not confound the
	// comparison; the coverage weights alone must rank complete > partial.
	gotComplete := scoreProtocol(complete, "json", "type")
	gotPartial := scoreProtocol(partial, "json", "type")
	assert.Greater(t, gotComplete, gotPartial, "a complete draft must outscore a partial one")
}

func TestScoreProtocol_BatchAndHandshakeWeighted(t *testing.T) {
	// TypePath +1, Roles +1, Batchesx2 +2, handshakex2 +2, framing consensus +1,
	// typepath consensus +1 = 8.
	p := &project.Protocol{
		Framing: "json", TypePath: "type",
		Roles: map[string]*project.ProtocolRole{
			"web": {Handshake: &project.RoleHandshake{AwaitType: "x"}},
		},
		Batches: map[string]*project.ProtocolBatch{"b": {ItemType: "i"}},
	}
	assert.Equal(t, 8, scoreProtocol(p, "json", "type"))
}

func TestScoreProtocol_NilSafe(t *testing.T) {
	assert.Equal(t, 0, scoreProtocol(nil, "json", "type"))
}

func TestSummarizeFailures_Deterministic(t *testing.T) {
	got := summarizeFailures([]sample{
		{outcome: outcomeFailed, reason: reasonParse},
		{outcome: outcomeFailed, reason: reasonDrift},
		{outcome: outcomeFailed, reason: reasonDrift},
		{outcome: outcomeFound, proto: &project.Protocol{}},
	})
	assert.Equal(t, "2 drift, 1 parse", got)
}

func TestBuildInferPrompt_RecognitionGuidance(t *testing.T) {
	prompt := buildInferPrompt("rt", []string{"web", "bridge"}, []SourceFile{{Path: "room.ts", Content: "..."}})

	// The four in-scope structures must be called out so the model knows to
	// populate the corresponding tool fields.
	for _, want := range []string{"envelope", "batch", "handshake", "role"} {
		assert.Contains(t, prompt, want, "prompt must guide recognition of %q", want)
	}
	// The prompt must no longer hand-write a JSON shape block (the tool schema
	// now carries the shape). The legacy marker was the literal `"found":`.
	assert.NotContains(t, prompt, `"found":`, "prompt must not hand-write the JSON shape; the tool schema owns it")
	// credential_ref safety constraint must remain.
	assert.Contains(t, prompt, "credential_ref")

	// The available actor list is injected so the model names a real
	// credential_ref instead of inventing one (M3-3 dogfood: model guessed
	// "user", which failed validation).
	assert.Contains(t, prompt, "web, bridge", "prompt must list the available actors for credential_ref")

	// Batching cue names the timer/flush pattern (dogfood: 50ms setTimeout flush
	// to a different routing key was missed despite being in the source).
	assert.Contains(t, prompt, "setTimeout", "batching cue must name the timer-flush pattern")
	// items_path must be the full dotted path from the frame root (dogfood: model
	// emitted "lines"/"payload.items" instead of "payload.lines").
	assert.Contains(t, prompt, "frame root", "batching cue must stress the full dotted path from the frame root")

	// Handshake cue names the conditional/guarded-send pattern (dogfood: a
	// peer-gated devices:sync guarded by `if (peers.length > 0)` was missed).
	assert.Contains(t, prompt, "guarded", "handshake cue must name the conditional-send pattern")
	// await_type cue: pass 1 names a best-guess literal; pass 2 verifies.

	// Two-pass grounding: pass 1 names candidate literals; a second pass
	// verifies them. The prompt must not demand a verbatim source block quote
	// (the copy burden moved to code-extract + pass 2).
	assert.NotContains(t, prompt, "handshake.source", "pass-1 prompt must not require a handshake source quote")
	assert.Contains(t, prompt, "second pass", "prompt must mention the verifying second pass")

	// Token-slot steer: when auth carries a param (e.g. ?token=), a role must
	// not also declare that name as a param/header/subprotocol — ValidateProtocol
	// rejects the collision. Dogfood: model put `token` in bridge.params.
	assert.Contains(t, prompt, "token slot", "prompt must steer roles off the auth param name")

	// Role discriminator value must match the role name (dogfood N=18: bridge
	// role sometimes carried type=web instead of type=bridge).
	assert.Contains(t, prompt, "discriminator value must match", "prompt must steer role discriminator to match role name")
}

// TestBuildInferPrompt_NoActors guards the empty-actor-list path: the prompt
// must still build (no panic) and steer the model to leave credential_ref blank
// when no actors are declared.
func TestBuildInferPrompt_NoActors(t *testing.T) {
	prompt := buildInferPrompt("rt", nil, []SourceFile{{Path: "room.ts", Content: "..."}})
	assert.Contains(t, prompt, "credential_ref", "credential_ref guidance must still appear with no actors")
}

// TestTruncateContent_RuneSafe guards against byte-slicing a multi-byte rune at
// the truncation point: the output must remain valid UTF-8. "世界" is 2 runes /
// 6 bytes; byte-slicing at an odd boundary would split the second rune.
func TestTruncateContent_RuneSafe(t *testing.T) {
	s := strings.Repeat("世界", 10) // 20 runes, 60 bytes
	out := truncateContent(s, 5)
	if !utf8.ValidString(out) {
		t.Errorf("truncated output is not valid UTF-8: %q", out)
	}
	// The rune prefix before the marker must be exactly 5 runes.
	marker := strings.Index(out, "\n…[truncated]")
	if marker < 0 {
		t.Fatalf("missing truncation marker in %q", out)
	}
	prefix := out[:marker]
	if got := utf8.RuneCountInString(prefix); got != 5 {
		t.Errorf("prefix rune count = %d, want 5", got)
	}
}
