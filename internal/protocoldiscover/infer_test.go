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
				"handshake":      map[string]any{"await_type": "ready", "timeout": 5, "source": "if (ready) ws.send({type: 'ready'})"},
			},
		},
	})
	p, err := Infer(context.Background(), driver, cfg, "rt", []SourceFile{{Path: "room.ts", Content: "if (ready) ws.send({type: 'ready'})"}}, 1)
	require.NoError(t, err)
	require.NotNil(t, p)
	require.Contains(t, p.Roles, "web")
	role := p.Roles["web"]
	assert.Equal(t, "web", role.CredentialRef)
	assert.Equal(t, map[string]string{"type": "web"}, role.Params)
	require.NotNil(t, role.Handshake)
	assert.Equal(t, "ready", role.Handshake.AwaitType)
	assert.Equal(t, 5, role.Handshake.Timeout)
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

// TestInfer_Voting_PicksHigherScored: a complete draft beats a partial one when
// both are present.
func TestInfer_Voting_PicksHigherScored(t *testing.T) {
	partial := map[string]any{
		"found": true, "framing": "json", "type_path": "type",
		"auth": map[string]any{"strategy": "query", "param": "token", "credential_ref": "web"},
	}
	complete := validInput()
	handshakeQuote := "if (onlineDevices.length > 0) { ws.send({type: 'devices:sync'})"
	batchQuote := "type: 'session:output-batch', payload: { lines }"
	complete["roles"] = map[string]any{
		"web": map[string]any{
			"credential_ref": "web",
			"handshake":      map[string]any{"await_type": "devices:sync", "timeout": 5, "source": handshakeQuote},
		},
	}
	complete["batches"] = map[string]any{
		"session:output-batch": map[string]any{"item_type": "session:output", "items_path": "payload.lines", "source": batchQuote},
	}
	driver := mockSequenceDriver(t, []map[string]any{partial, complete, partial})
	corpus := []SourceFile{{Content: handshakeQuote + "\n" + batchQuote}}
	p, err := Infer(context.Background(), driver, cfgWithService(), "rt", corpus, 3)
	require.NoError(t, err)
	require.NotNil(t, p)
	require.Contains(t, p.Roles, "web", "must pick the complete (scored-higher) draft")
	require.NotNil(t, p.Roles["web"].Handshake)
}

// TestInfer_Voting_AllNotFoundIsErrNoProtocol: unanimous found=false is still a
// clean not-found, not a hard error.
func TestInfer_Voting_AllNotFoundIsErrNoProtocol(t *testing.T) {
	driver := mockSequenceDriver(t, []map[string]any{
		{"found": false}, {"found": false}, {"found": false},
	})
	_, err := Infer(context.Background(), driver, cfgWithService(), "rt", nil, 3)
	assert.ErrorIs(t, err, ErrNoProtocol)
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

func TestValidateGrounding_HandshakeSourcePresent(t *testing.T) {
	input := map[string]any{
		"roles": map[string]any{"web": map[string]any{
			"handshake": map[string]any{"await_type": "devices:sync", "source": "if (onlineDevices.length > 0) { ws.send({type: 'devices:sync'})"},
		}},
	}
	inputs := []SourceFile{{Content: "if (onlineDevices.length > 0) { ws.send({type: 'devices:sync'})"}}
	assert.NoError(t, validateGrounding(input, inputs))
}

func TestValidateGrounding_HandshakeSourceAbsent(t *testing.T) {
	input := map[string]any{
		"roles": map[string]any{"web": map[string]any{
			"handshake": map[string]any{"await_type": "devices:sync", "source": "if (onlineDevices.length > 0) { ws.send({type: 'devices:sync'})"},
		}},
	}
	err := validateGrounding(input, []SourceFile{{Content: "totally unrelated source"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ungrounded")
	// Leak guard: the raw quote must not echo back in the error.
	assert.NotContains(t, err.Error(), "devices:sync")
}

func TestValidateGrounding_HandshakeMissingSource(t *testing.T) {
	input := map[string]any{
		"roles": map[string]any{"web": map[string]any{
			"handshake": map[string]any{"await_type": "devices:sync"},
		}},
	}
	err := validateGrounding(input, []SourceFile{{Content: "whatever"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ungrounded")
}

func TestValidateGrounding_BatchSourceChecked(t *testing.T) {
	good := map[string]any{
		"batches": map[string]any{"session:output-batch": map[string]any{
			"item_type": "session:output", "items_path": "payload.lines",
			"source": "type: 'session:output-batch', payload: { lines }",
		}},
	}
	assert.NoError(t, validateGrounding(good, []SourceFile{{Content: "type: 'session:output-batch', payload: { lines }"}}))

	bad := map[string]any{
		"batches": map[string]any{"session:output-batch": map[string]any{
			"source": "type: 'session:output-batch', payload: { lines }",
		}},
	}
	err := validateGrounding(bad, []SourceFile{{Content: "no match here"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ungrounded")
}

func TestValidateGrounding_NoHardLiterals(t *testing.T) {
	// No handshake, no batches -> nothing to ground -> nil regardless of inputs.
	assert.NoError(t, validateGrounding(map[string]any{"found": true, "framing": "json"}, nil))
}

func TestInferOnce_UngroundedHandshake(t *testing.T) {
	cfg := cfgWithService()
	driver := mockToolDriver(t, map[string]any{
		"found":     true,
		"framing":   "json",
		"type_path": "type",
		"roles": map[string]any{"web": map[string]any{
			"credential_ref": "web",
			"handshake":      map[string]any{"await_type": "devices:sync", "timeout": 5, "source": "NOT IN INPUTS"},
		}},
	})
	s := inferOnce(context.Background(), driver, cfg, "rt", []SourceFile{{Content: "unrelated"}})
	assert.Equal(t, outcomeFailed, s.outcome)
	assert.Equal(t, reasonUngrounded, s.reason)
}

func TestInferOnce_GroundedHandshake(t *testing.T) {
	cfg := cfgWithService()
	quote := "if (onlineDevices.length > 0) { ws.send({type: 'devices:sync'})"
	driver := mockToolDriver(t, map[string]any{
		"found":     true,
		"framing":   "json",
		"type_path": "type",
		"roles": map[string]any{"web": map[string]any{
			"credential_ref": "web",
			"handshake":      map[string]any{"await_type": "devices:sync", "timeout": 5, "source": quote},
		}},
	})
	s := inferOnce(context.Background(), driver, cfg, "rt", []SourceFile{{Content: quote}})
	assert.Equal(t, outcomeFound, s.outcome)
	require.NotNil(t, s.proto)
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
	// await_type must be the verbatim type literal, not paraphrased (dogfood:
	// model emitted "device:online" instead of the source's "devices:sync").
	assert.Contains(t, prompt, "verbatim", "handshake cue must demand the verbatim type literal")
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
