# `protocol infer` N-Sample Voting — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Absorb `protocol infer` run-to-run variance by running N drafts (default 3) and selecting the strongest validated one, without changing the three-state error model or the underlying model.

**Architecture:** Split the single-shot `Infer` into a pure per-sample classifier `inferOnce` (returns a `sample` descriptor, never an error) and a voting orchestrator. `Infer` loops `inferOnce` N times sequentially, short-circuiting only on `ctx.Err()`, then `selectProtocol` applies the rules: ≥1 Found → highest-scoring; 0 Found + ≥1 NotFound → `ErrNoProtocol`; all Failed → hard error with reason counts. `scoreProtocol` rewards drafts that recognized more (and harder) structures. The `llm.MockClient` gains an additive `SetToolResponseSequence` so tests can represent run-to-run variance.

**Tech Stack:** Go 1.25, `github.com/binoctal/cerberus`, `internal/llm` (`MockClient`, `ToolCall`), `internal/ai.Driver.DecideWithTools`, `internal/project.ValidateProtocol`, `spf13/cobra` CLI.

## Global Constraints

- Commit author: `binoctal <binoctal@gmail.com>`, **no** `Co-Authored-By`.
- Commit messages and code comments in English.
- `make check` (fmt + lint + test) EXIT 0 + clean git tree after every task.
- No CGo. Follow existing comment density and naming idiom.
- Documents only in `cerberus-docs/` (never `docs/`).
- TDD: write the failing test, run it RED, implement, run it GREEN, commit.

## File Structure

- **Modify** `internal/llm/mock.go` — `SetToolResponseSequence` + sequence store + per-key counter.
- **Modify** `internal/llm/mock_test.go` — sequence rotation test.
- **Modify** `internal/protocoldiscover/infer.go` — `sample` type, `inferOnce`, `scoreProtocol`, `modalFields`, `modeOf`, `summarizeFailures`, `selectProtocol`, voting `Infer`, `samples` param, `DefaultInferSamples`.
- **Modify** `internal/protocoldiscover/infer_test.go` — migrate existing calls to the new signature; add `inferOnce`/`scoreProtocol`/`selectProtocol`/voting tests.
- **Modify** `cmd/cerberus/main_protocol.go` — `--samples` flag, `protocolInferOpts.Samples`, normalize + plumb.
- **Modify** `cmd/cerberus/main_protocol_test.go` — assert the `samples` flag exists.
- **Append** `cerberus-docs/technical/dogfood/2026-07-31-m3-3-protocol-infer-dogfood.md` — N=3 Run 9+ section.

---

## Task 1: `MockClient` response sequence

**Files:**
- Modify: `internal/llm/mock.go`
- Test: `internal/llm/mock_test.go`

**Interfaces:**
- Produces: `(*MockClient).SetToolResponseSequence(key string, sequence [][]ToolCall)` — consumed by the voting tests in Task 4.

- [ ] **Step 1: Write the failing test**

Append to `internal/llm/mock_test.go`:

```go
// TestMockClient_ToolResponseSequence verifies that successive Complete calls
// for the same key rotate through a preset sequence and then hold on the last
// element. This is how N-sample voting tests represent run-to-run variance:
// the mock must return DIFFERENT drafts across calls with an identical prompt.
func TestMockClient_ToolResponseSequence(t *testing.T) {
	mock := NewMockClient(nil)
	mock.SetToolResponseSequence("default", [][]ToolCall{
		{{Name: "protocol_draft", Input: map[string]any{"found": false}}},        // false negative
		{{Name: "protocol_draft", Input: map[string]any{"found": true, "v": 1}}}, // good draft
		{{Name: "protocol_draft", Input: map[string]any{"found": true, "v": 2}}}, // good draft
	})

	call := func() *Response {
		resp, err := mock.Complete(context.Background(), Request{
			Messages: []Message{{Role: "user", Content: "anything"}},
		})
		if err != nil {
			t.Fatalf("Complete: %v", err)
		}
		return resp
	}

	r1 := call()
	if found, _ := r1.ToolCalls[0].Input["found"].(bool); found {
		t.Fatalf("call 1: want first sequence element (found=false), got %+v", r1.ToolCalls)
	}
	r2 := call()
	if v, _ := r2.ToolCalls[0].Input["v"].(int); v != 1 {
		t.Fatalf("call 2: want v=1, got %v", r2.ToolCalls[0].Input["v"])
	}
	r3 := call()
	if v, _ := r3.ToolCalls[0].Input["v"].(int); v != 2 {
		t.Fatalf("call 3: want v=2, got %v", r3.ToolCalls[0].Input["v"])
	}
	// Exhausted sequence holds on the last element.
	r4 := call()
	if v, _ := r4.ToolCalls[0].Input["v"].(int); v != 2 {
		t.Fatalf("call 4: want held last (v=2), got %v", r4.ToolCalls[0].Input["v"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/llm/ -run TestMockClient_ToolResponseSequence -v`
Expected: FAIL / compile error — `SetToolResponseSequence` undefined.

- [ ] **Step 3: Write minimal implementation**

Modify `internal/llm/mock.go`. Add a sequence store and counter to the struct, the setter, and a `nextToolCalls` helper; wire it into `Complete` so a sequence (when present for the matched key) takes precedence over the single-fixture map:

```go
type MockClient struct {
	responses     map[string]string
	toolResponses map[string][]ToolCall
	toolSequences map[string][][]ToolCall
	toolSeqIdx    map[string]int
}

func NewMockClient(responses map[string]string) *MockClient {
	return &MockClient{responses: responses}
}

// SetToolResponseSequence presets a rotating tool_use sequence for a prompt
// key. Each matching Complete call advances to the next element; when the
// sequence is exhausted the last element is held. A sequence takes precedence
// over a single-fixture SetToolResponse for the same key. This lets tests
// represent run-to-run variance (successive identical prompts yielding
// different drafts), which the single-fixture setter cannot.
func (m *MockClient) SetToolResponseSequence(key string, sequence [][]ToolCall) {
	if m.toolSequences == nil {
		m.toolSequences = map[string][][]ToolCall{}
		m.toolSeqIdx = map[string]int{}
	}
	m.toolSequences[key] = sequence
}
```

In `Complete`, replace the current tool-response lookup block:

```go
	if calls, ok := m.toolResponses[key]; ok {
		return &Response{
			Content:    "",
			ToolCalls:  calls,
			StopReason: "tool_use",
			Usage: TokenUsage{
				InputTokens:  len(req.Messages) * 10,
				OutputTokens: 0,
				TotalTokens:  len(req.Messages) * 10,
			},
		}, nil
	}
```

with a sequence-aware lookup (sequence first, then single fixture):

```go
	if calls, ok := m.nextToolCalls(key); ok {
		return &Response{
			Content:    "",
			ToolCalls:  calls,
			StopReason: "tool_use",
			Usage: TokenUsage{
				InputTokens:  len(req.Messages) * 10,
				OutputTokens: 0,
				TotalTokens:  len(req.Messages) * 10,
			},
		}, nil
	}
```

and add the helper:

```go
// nextToolCalls resolves the tool calls for a matched key. A rotating sequence
// (if present) takes precedence over a single-fixture response; the sequence
// index advances per call and clamps to the last element when exhausted.
func (m *MockClient) nextToolCalls(key string) ([]ToolCall, bool) {
	if seq, ok := m.toolSequences[key]; ok {
		idx := m.toolSeqIdx[key]
		if idx >= len(seq) {
			idx = len(seq) - 1
		} else {
			m.toolSeqIdx[key] = idx + 1
		}
		return seq[idx], true
	}
	if calls, ok := m.toolResponses[key]; ok {
		return calls, true
	}
	return nil, false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/llm/ -run TestMockClient -v`
Expected: PASS (including the existing `SetToolResponse` and `MatchKeyConsultsToolResponses` tests — single-fixture behaviour is unchanged because no sequence is set for those keys).

- [ ] **Step 5: `make check` + commit**

```bash
make check
git add internal/llm/mock.go internal/llm/mock_test.go
git commit -m "feat(llm): MockClient tool-call sequence for variance tests"
```

---

## Task 2: `sample` type + `inferOnce` + single-sample `selectProtocol` (refactor, no behaviour change)

**Files:**
- Modify: `internal/protocoldiscover/infer.go`
- Test: `internal/protocoldiscover/infer_test.go`

**Interfaces:**
- Produces: `sample`, `inferOnce`, `selectProtocol`. `Infer` keeps its 5-arg signature this task and delegates to them with a single sample, so behaviour is identical and all existing tests stay green.

- [ ] **Step 1: Write the failing tests**

Append to `internal/protocoldiscover/infer_test.go`:

```go
// sampleFromInput drives inferOnce with a single-fixture mock preset to `input`
// (returned for every call). It is the per-sample classifier test helper.
func sampleFromInput(t *testing.T, cfg *project.Config, input map[string]any) sample {
	t.Helper()
	return inferOnce(context.Background(), mockToolDriver(t, input), cfg, "rt", nil)
}

func TestInferOnce_Found(t *testing.T) {
	s := sampleFromInput(cfgWithService(), map[string]any{
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
	s := sampleFromInput(cfgWithService(), map[string]any{"found": false})
	assert.Equal(t, outcomeNotFound, s.outcome)
}

func TestInferOnce_Drift(t *testing.T) {
	// No tool fixture -> zero tool calls -> drift.
	s := inferOnce(context.Background(), mockEmptyDriver(t), cfgWithService(), "rt", nil)
	assert.Equal(t, outcomeFailed, s.outcome)
	assert.Equal(t, reasonDrift, s.reason)
}

func TestInferOnce_ParseError(t *testing.T) {
	// found=true but the tool args cannot round-trip into inferOutput: a struct
	// value (not a map) under roles breaks json.Unmarshal -> parse failure.
	s := sampleFromInput(cfgWithService(), map[string]any{
		"found": true,
		"roles": map[string]any{"web": "not-a-map"},
	})
	assert.Equal(t, outcomeFailed, s.outcome)
	assert.Equal(t, reasonParse, s.reason)
}

func TestInferOnce_Invalid(t *testing.T) {
	s := sampleFromInput(cfgWithService(), map[string]any{
		"found": true,
		"auth":  map[string]any{"strategy": "query", "param": "token", "credential_ref": "ghost"},
	})
	assert.Equal(t, outcomeFailed, s.outcome)
	assert.Equal(t, reasonInvalid, s.reason)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/protocoldiscover/ -run TestInferOnce -v`
Expected: FAIL / compile error — `sample`, `inferOnce`, `outcomeFound`, `reasonDrift`, etc. undefined.

- [ ] **Step 3: Add the `sample` type and refactor `Infer`**

In `internal/protocoldiscover/infer.go`, add (above `Infer`):

```go
// sampleOutcome classifies one inference attempt so the voter can count
// categories across samples instead of collapsing them into a single error.
type sampleOutcome int

const (
	outcomeFound    sampleOutcome = iota // a validated *project.Protocol
	outcomeNotFound                      // the model signalled found=false
	outcomeFailed                        // drift / parse / invalid / infra failure
)

// failReason is a non-leaking diagnostic tag for the all-failed error message.
// It never carries raw model output.
type failReason string

const (
	reasonDrift   failReason = "drift"
	reasonParse   failReason = "parse"
	reasonInvalid failReason = "invalid"
	reasonInfra   failReason = "infra"
)

// sample is the result of one inference attempt.
type sample struct {
	outcome sampleOutcome
	proto   *project.Protocol // set only when outcome == outcomeFound
	score   int               // set by selectProtocol when outcome == outcomeFound
	reason  failReason        // set only when outcome == outcomeFailed
}
```

Add `inferOnce` (the current body of `Infer`, but classifying into a `sample` and never returning an error):

```go
// inferOnce runs a single protocol_draft inference and classifies the outcome.
// It never returns an error: every adverse result (drift, parse failure,
// validation failure, or a DecideWithTools error such as budget/retry/ctx
// exhaustion) becomes outcomeFailed so the voter can continue with the next
// sample. Systemic cancellation is handled by the voter's ctx.Err() check
// between samples, not here.
func inferOnce(ctx context.Context, driver *ai.Driver, cfg *project.Config, serviceName string, inputs []SourceFile) sample {
	prompt := buildInferPrompt(serviceName, actorNames(cfg), inputs)
	res, err := driver.DecideWithTools(ctx, prompt, []llm.Tool{protocolDraftTool()})
	if err != nil {
		return sample{outcome: outcomeFailed, reason: reasonInfra}
	}
	if len(res.ToolCalls) == 0 {
		return sample{outcome: outcomeFailed, reason: reasonDrift}
	}
	input := res.ToolCalls[0].Input
	if found, _ := input["found"].(bool); !found {
		return sample{outcome: outcomeNotFound}
	}
	p, err := argsToProtocol(input)
	if err != nil {
		return sample{outcome: outcomeFailed, reason: reasonParse}
	}
	if err := project.ValidateProtocol(p, actorsOf(cfg)); err != nil {
		return sample{outcome: outcomeFailed, reason: reasonInvalid}
	}
	return sample{outcome: outcomeFound, proto: p}
}
```

Add `selectProtocol` (single-sample-capable; full voting rules wired in Task 3/4, but it is already correct for any slice):

```go
// selectProtocol applies the voting rules to N samples:
//   - >=1 Found  -> the highest-scoring Found (ties broken by earliest index).
//   - 0 Found, >=1 NotFound -> ErrNoProtocol.
//   - 0 Found, 0 NotFound (all Failed) -> a hard error with reason counts.
//
// Scoring is computed here (not in inferOnce) so the modal fields across all
// Found samples are known for the consensus tie-break.
func selectProtocol(samples []sample) (*project.Protocol, error) {
	modalFraming, modalTypePath := modalFields(samples)

	var best *sample
	for i := range samples {
		s := &samples[i]
		if s.outcome != outcomeFound {
			continue
		}
		s.score = scoreProtocol(s.proto, modalFraming, modalTypePath)
		if best == nil || s.score > best.score {
			best = s
		}
	}
	if best != nil {
		return best.proto, nil
	}
	for _, s := range samples {
		if s.outcome == outcomeNotFound {
			return nil, ErrNoProtocol
		}
	}
	return nil, fmt.Errorf("protocol inference failed across all samples: %s", summarizeFailures(samples))
}
```

Add the three helpers `selectProtocol` references (full implementations — they are pure and unit-tested in Task 3, but defined now so the package compiles):

```go
// modalFields returns the most common Framing and TypePath across Found
// samples, for the consensus tie-break in scoreProtocol. Empty when no Found
// samples. Ties are broken deterministically (see modeOf).
func modalFields(samples []sample) (framing, typePath string) {
	fcount := map[string]int{}
	tcount := map[string]int{}
	for _, s := range samples {
		if s.outcome != outcomeFound || s.proto == nil {
			continue
		}
		fcount[s.proto.Framing]++
		tcount[s.proto.TypePath]++
	}
	return modeOf(fcount), modeOf(tcount)
}

// modeOf returns the key with the highest count, breaking ties by the
// lexicographically smallest key so the result is deterministic regardless of
// map iteration order. Returns "" when no key has a positive count.
func modeOf(counts map[string]int) string {
	var best string
	bestCount := 0
	for k, c := range counts {
		switch {
		case c > bestCount:
			best, bestCount = k, c
		case c == bestCount && c > 0 && (best == "" || k < best):
			best = k
		}
	}
	return best
}

// summarizeFailures renders a deterministic "N <reason>" summary of the Failed
// samples for the all-failed error message. It leaks no raw model output.
func summarizeFailures(samples []sample) string {
	counts := map[failReason]int{}
	for _, s := range samples {
		if s.outcome == outcomeFailed {
			counts[s.reason]++
		}
	}
	var parts []string
	for _, r := range []failReason{reasonInfra, reasonDrift, reasonParse, reasonInvalid} {
		if counts[r] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[r], r))
		}
	}
	if len(parts) == 0 {
		return "no samples"
	}
	return strings.Join(parts, ", ")
}
```

Now rewrite `Infer` to delegate (5-arg signature unchanged this task):

```go
// Infer asks the LLM to draft a protocol description from the given inputs and
// returns the strongest validated draft. See inferOnce for the per-sample
// states and selectProtocol for the voting rules.
//
// TEMPORARY (Task 2): runs a single sample, so behaviour is identical to the
// pre-voting Infer. Task 4 adds the `samples` parameter and the N-sample loop.
func Infer(ctx context.Context, driver *ai.Driver, cfg *project.Config, serviceName string, inputs []SourceFile) (*project.Protocol, error) {
	if driver == nil {
		return nil, errors.New("nil driver")
	}
	return selectProtocol([]sample{inferOnce(ctx, driver, cfg, serviceName, inputs)})
}
```

Note `scoreProtocol` is referenced by `selectProtocol` but not yet defined — add a minimal stub now so the package compiles, to be fully implemented and tested in Task 3:

```go
// scoreProtocol ranks a validated draft so the voter can pick the strongest.
// Full weighting is implemented in Task 3; this stub returns 0 so single-sample
// selection (Task 2) is unaffected.
func scoreProtocol(p *project.Protocol, modalFraming, modalTypePath string) int {
	_ = p
	_ = modalFraming
	_ = modalTypePath
	return 0
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/protocoldiscover/ -v`
Expected: all PASS — the new `TestInferOnce_*` tests pass, and every existing `TestInfer_*` test still passes because single-sample `selectProtocol` reproduces the prior three-state semantics exactly.

- [ ] **Step 5: `make check` + commit**

```bash
make check
git add internal/protocoldiscover/infer.go internal/protocoldiscover/infer_test.go
git commit -m "refactor(protocoldiscover): extract inferOnce + selectProtocol from Infer"
```

---

## Task 3: `scoreProtocol` weighting

**Files:**
- Modify: `internal/protocoldiscover/infer.go` (`scoreProtocol`)
- Test: `internal/protocoldiscover/infer_test.go`

**Interfaces:**
- Produces: the real `scoreProtocol` weighting, consumed by `selectProtocol`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/protocoldiscover/infer_test.go`:

```go
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
	// framing=json, modal framing/json -> +1 consensus, +1 TypePath, +0 Auth,
	// +0 Roles, +2 Batches, +2 handshake = 6.
	p := &project.Protocol{
		Framing: "json", TypePath: "type",
		Roles: map[string]*project.ProtocolRole{
			"web": {Handshake: &project.RoleHandshake{AwaitType: "x"}},
		},
		Batches: map[string]*project.ProtocolBatch{"b": {ItemType: "i"}},
	}
	assert.Equal(t, 6, scoreProtocol(p, "json", "type"))
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/protocoldiscover/ -run "TestScoreProtocol|TestSummarizeFailures" -v`
Expected: FAIL — the `scoreProtocol` stub returns 0, so the weighting/count assertions fail.

- [ ] **Step 3: Implement `scoreProtocol`**

Replace the stub body in `internal/protocoldiscover/infer.go`:

```go
// scoreProtocol ranks a validated draft so the voter can pick the strongest.
// The observed false-negative signature is omission — tail runs drop
// structures — so the score rewards drafts that recognized more (and harder)
// structures. Weights are opinionated but simple and intentionally untuned:
// "more structures beats fewer" is the dominant signal. The consensus bonuses
// (modalFraming/modalTypePath) only break ties; they never override coverage.
func scoreProtocol(p *project.Protocol, modalFraming, modalTypePath string) int {
	if p == nil {
		return 0
	}
	score := 0
	if p.TypePath != "" {
		score++
	}
	if p.Auth != nil {
		score++
	}
	score += len(p.Roles)
	score += len(p.Batches) * 2 // batching is a non-obvious structure; weight it
	handshakeRoles := 0
	for _, r := range p.Roles {
		if r != nil && r.Handshake != nil {
			handshakeRoles++
		}
	}
	score += handshakeRoles * 2 // handshake is the hardest structure; weight it
	if modalFraming != "" && p.Framing == modalFraming {
		score++
	}
	if modalTypePath != "" && p.TypePath == modalTypePath {
		score++
	}
	return score
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/protocoldiscover/ -v`
Expected: all PASS.

- [ ] **Step 5: `make check` + commit**

```bash
make check
git add internal/protocoldiscover/infer.go internal/protocoldiscover/infer_test.go
git commit -m "feat(protocoldiscover): scoreProtocol weights structure coverage"
```

---

## Task 4: N-sample voting loop + `samples` parameter

**Files:**
- Modify: `internal/protocoldiscover/infer.go` (`Infer`, `DefaultInferSamples`)
- Modify: `internal/protocoldiscover/infer_test.go` (migrate + voting tests)
- Modify: `cmd/cerberus/main_protocol.go` (`runProtocolInfer` Infer call site only)

**Interfaces:**
- Consumes: `MockClient.SetToolResponseSequence` (Task 1), `inferOnce` + `selectProtocol` (Tasks 2–3).
- Produces: `Infer(ctx, driver, cfg, serviceName string, inputs []SourceFile, samples int) (*project.Protocol, error)` and `DefaultInferSamples = 3`.

- [ ] **Step 1: Write the failing voting tests**

Append to `internal/protocoldiscover/infer_test.go`. First a sequence-driver helper, then the cases:

```go
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
		{"found": false},       // false negative
		validInput(),           // good
		validInput(),           // good
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
	complete["roles"] = map[string]any{
		"web": map[string]any{
			"credential_ref": "web",
			"handshake":      map[string]any{"await_type": "devices:sync", "timeout": 5},
		},
	}
	complete["batches"] = map[string]any{
		"session:output-batch": map[string]any{"item_type": "session:output", "items_path": "payload.lines"},
	}
	driver := mockSequenceDriver(t, []map[string]any{partial, complete, partial})
	p, err := Infer(context.Background(), driver, cfgWithService(), "rt", nil, 3)
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
```

- [ ] **Step 2: Migrate the existing `TestInfer_*` calls to the new signature**

In `internal/protocoldiscover/infer_test.go`, every existing `Infer(...)` call gains a trailing `1` (single sample, identical semantics with a constant mock). There are five call sites: `TestInfer_ReturnsValidatedProtocol`, `TestInfer_FoundFalse_ReturnsErrNoProtocol`, `TestInfer_InvalidCredentialRefFailsValidation`, `TestInfer_RolesPopulated`, `TestInfer_ZeroToolCalls_IsHardErrorNotErrNoProtocol`, `TestInfer_InvalidCredentialRef_IsHardError`. For example:

```go
p, err := Infer(context.Background(), driver, cfg, "rt", []SourceFile{...}, 1)
```

Apply the same trailing `, 1` to each of the six existing call sites. (Their assertions are unchanged — with a constant mock every sample agrees, so single-shot semantics hold.)

- [ ] **Step 3: Run tests to verify the new voting tests fail (compile: signature mismatch)**

Run: `go test ./internal/protocoldiscover/ -run TestInfer_Voting -v`
Expected: compile error — `Infer` still has the 5-arg signature; the new tests pass 6 args.

- [ ] **Step 4: Implement the voting `Infer` + `DefaultInferSamples`**

In `internal/protocoldiscover/infer.go`, replace the Task-2 `Infer` with:

```go
// DefaultInferSamples is the default number of drafts Infer runs before
// selecting the strongest. N>1 absorbs run-to-run variance (false negatives,
// parse failures); see 2026-08-01-protocol-infer-n-sample-voting-design.md.
// Overridable via the protocol infer --samples flag.
const DefaultInferSamples = 3

// Infer asks the LLM to draft a protocol description from the given inputs,
// runs `samples` drafts, and returns the strongest validated one. samples < 1
// is clamped to 1 (single-shot). See inferOnce for the per-sample states and
// selectProtocol for the voting rules. The three-state contract is preserved:
// ErrNoProtocol when the consensus is "no protocol here"; a hard error only
// when every sample failed. Systemic cancellation short-circuits via ctx.Err().
func Infer(ctx context.Context, driver *ai.Driver, cfg *project.Config, serviceName string, inputs []SourceFile, samples int) (*project.Protocol, error) {
	if driver == nil {
		return nil, errors.New("nil driver")
	}
	if samples < 1 {
		samples = 1
	}
	results := make([]sample, 0, samples)
	for i := 0; i < samples; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		results = append(results, inferOnce(ctx, driver, cfg, serviceName, inputs))
	}
	return selectProtocol(results)
}
```

- [ ] **Step 5: Update the `cmd/cerberus/main_protocol.go` call site**

In `runProtocolInfer` (`cmd/cerberus/main_protocol.go`), change the single call to pass a literal `1` for now (Task 5 makes it configurable):

```go
	p, err := protocoldiscover.Infer(ctx, driver, cfg, service, inputs, 1)
```

- [ ] **Step 6: Run all protocoldiscover + cmd tests**

Run: `go test ./internal/protocoldiscover/ ./cmd/cerberus/ -v`
Expected: all PASS — voting tests green, migrated single-sample tests green, command tests green (constant mock + samples=1).

- [ ] **Step 7: `make check` + commit**

```bash
make check
git add internal/protocoldiscover/infer.go internal/protocoldiscover/infer_test.go cmd/cerberus/main_protocol.go
git commit -m "feat(protocoldiscover): Infer N-sample voting with best-of-N selection"
```

---

## Task 5: CLI `--samples` flag

**Files:**
- Modify: `cmd/cerberus/main_protocol.go`
- Test: `cmd/cerberus/main_protocol_test.go`

**Interfaces:**
- Produces: a `--samples` flag (default `protocoldiscover.DefaultInferSamples`) plumbed into `Infer`.

- [ ] **Step 1: Write the failing test**

Append to `cmd/cerberus/main_protocol_test.go`. Add `"strconv"` to the import block if not present.

```go
// TestRunProtocolInfer_SamplesDefaultAndPlumbed verifies that runProtocolInfer
// normalizes a zero Samples to DefaultInferSamples and forwards it to Infer.
// With a sequence mock whose first element is a false negative and remaining
// elements are good drafts, the default (N=3) must absorb the false negative
// and return a protocol.
func TestRunProtocolInfer_SamplesDefaultAndPlumbed(t *testing.T) {
	workDir := t.TempDir()
	writeProtocolProjectYAML(t, workDir, protocolFixture)
	from := writeProtocolFrom(t, workDir, "doc.md", "# WS spec\n")

	good := map[string]any{
		"found":     true,
		"framing":   "json",
		"type_path": "type",
		"auth":      map[string]any{"strategy": "query", "param": "token", "credential_ref": "u"},
	}
	seq := [][]llm.ToolCall{
		{{Name: "protocol_draft", Input: map[string]any{"found": false}}}, // false negative
		{{Name: "protocol_draft", Input: good}},
		{{Name: "protocol_draft", Input: good}},
	}
	mock := llm.NewMockClient(nil)
	mock.SetToolResponseSequence("default", seq)
	driver := ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000))

	// Samples left zero -> runProtocolInfer must normalize to DefaultInferSamples.
	opts := protocolInferOpts{
		Name:    "ws",
		From:    from,
		DryRun:  true,
		confirm: func(string) bool { return true },
	}
	if err := runProtocolInfer(context.Background(), workDir, driver, opts); err != nil {
		t.Fatalf("err: %v", err)
	}
}
```

Also update `TestProtocolCmd_Tree`'s flag list to include `"samples"`:

```go
	for _, name := range []string{"name", "from", "service", "dry-run", "samples"} {
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/cerberus/ -run "TestRunProtocolInfer_SamplesDefaultAndPlumbed|TestProtocolCmd_Tree" -v`
Expected: FAIL — no `Samples` field, no `--samples` flag, no normalization; the false-negative-first sequence with samples=1 yields `ErrNoProtocol` reported (no error returned, but the test's intent — that N=3 absorbs the false negative — is unmet because the plumbing is absent; `TestProtocolCmd_Tree` fails on the missing flag).

- [ ] **Step 3: Implement the flag + plumbing**

In `cmd/cerberus/main_protocol.go`:

1. Add the var declaration alongside the others:

```go
var (
	protocolInferName    string
	protocolInferFrom    string
	protocolInferService string
	protocolInferDryRun  bool
	protocolInferSamples int
)
```

2. Add the field to `protocolInferOpts`:

```go
type protocolInferOpts struct {
	Name    string
	From    string
	Service string
	DryRun  bool
	Samples int
	confirm func(prompt string) bool
}
```

3. Register the flag in `protocolInferCmd()` (after the `--dry-run` line):

```go
	cmd.Flags().IntVar(&protocolInferSamples, "samples", protocoldiscover.DefaultInferSamples, "number of drafts to run (best-of-N absorbs variance)")
```

4. Pass it through in the `RunE`:

```go
		return runProtocolInfer(cmd.Context(), ".", driver, protocolInferOpts{
			Name:    protocolInferName,
			From:    protocolInferFrom,
			Service: protocolInferService,
			DryRun:  protocolInferDryRun,
			Samples: protocolInferSamples,
			confirm: promptConfirm(os.Stdin, os.Stdout),
		})
```

5. Normalize and forward in `runProtocolInfer`, replacing the Task-4 literal:

```go
	samples := opts.Samples
	if samples <= 0 {
		samples = protocoldiscover.DefaultInferSamples
	}
	p, err := protocoldiscover.Infer(ctx, driver, cfg, service, inputs, samples)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/cerberus/ -v`
Expected: all PASS.

- [ ] **Step 5: `make check` + commit**

```bash
make check
git add cmd/cerberus/main_protocol.go cmd/cerberus/main_protocol_test.go
git commit -m "feat(cmd): protocol infer --samples flag (default best-of-3)"
```

---

## Task 6: Dogfood N=3 against `open-agents`

**Files:**
- Append: `cerberus-docs/technical/dogfood/2026-07-31-m3-3-protocol-infer-dogfood.md`

This task is a manual run + record; no Go test. It produces the evidence that voting absorbs the variance documented in the value-accuracy pass.

- [ ] **Step 1: Build and start the target**

```
make build
cd /home/mason/Documents/code_projects/private/open-agents
fnm use 22
cd apps/api && npm run dev    # wrangler dev, port 8989
```

Confirm WS OPEN via a probe to `ws://localhost:8989/ws/demo_user?type=web&token=demo_token` (per the existing dogfood setup). If it does not OPEN, stop and record a setup blocker.

- [ ] **Step 2: Run `protocol infer` with the new default (N=3)**

From the `open-agents` repo root, reusing its `.cerberus/project.yaml` (actors `web`, `bridge`, `user`):

```
cerberus protocol infer --name open-agents \
  --from apps/api/src/realtime --service api --dry-run
```

Capture the full draft YAML. Then run it 3–4 times to observe whether N=3 voting stabilizes the four-structure coverage (envelope, multi-role, batching, conditional handshake) relative to the Run 5–8 variance table.

- [ ] **Step 3: Append the Run 9+ section**

Append a new section to `cerberus-docs/technical/dogfood/2026-07-31-m3-3-protocol-infer-dogfood.md` titled `## N-sample voting — YYYY-MM-DD` (use the run date) containing:
- The default `--samples 3` invocation.
- The per-run drafted YAML and the per-structure coverage table (the same four structures), across the 3–4 runs.
- A short verdict: did voting absorb the false-negative / parse-failure tails? Did the selected draft carry the verbatim `devices:sync` await_type more often than single-shot? (It raises the floor but need not guarantee it — that is the two-step follow-up.)
- Note the `await_type` outcome honestly whether improved or not.

- [ ] **Step 4: Commit**

```bash
git add cerberus-docs/technical/dogfood/2026-07-31-m3-3-protocol-infer-dogfood.md
git commit -m "docs(dogfood): protocol infer N=3 voting run against open-agents"
```

---

## Self-Review

**Spec coverage:** spec §Architecture (`inferOnce`/`selectProtocol`/`Infer` voting) → T2 + T4. §Aggregation rules → `selectProtocol` (T2/T4) + voting tests (T4). §`scoreProtocol` → T3. §Cost/budget/sequencing (sequential loop, `ctx.Err()` short-circuit, `<1` clamp, shared budget) → T4 `Infer`. §MockClient sequences → T1. §Testing (per-category `inferOnce`, score table, voting cases, leak guard) → T2/T3/T4. §File structure → every listed file is modified in a task. §Dogfood → T6. All spec sections mapped.

**Placeholder scan:** No TBD/TODO/"implement later"/"add validation". The Task 2 `scoreProtocol` stub is explicitly temporary and replaced verbatim in Task 3; every code block is complete and runnable. Task 6 is intentionally a manual run (no Go test) with a concrete capture/checklist, not a placeholder.

**Type consistency:** `inferOnce(...) sample` (T2) matches the call in `Infer` (T4) and the helper `sampleFromInput` (T2 test). `selectProtocol([]sample) (*project.Protocol, error)` (T2) matches `Infer`'s `return selectProtocol(results)` (T4). `scoreProtocol(p, modalFraming, modalTypePath string) int` (T3) matches `selectProtocol`'s call (T2). `failReason`/`sampleOutcome` constants (T2) match `summarizeFailures` (T3 test) and `inferOnce` (T2). `SetToolResponseSequence(key string, [][]ToolCall)` (T1) matches the voting-test helpers (T4) and the CLI test (T5). `DefaultInferSamples` (T4) matches the flag default + `runProtocolInfer` normalization (T5). `Infer(..., samples int)` (T4) matches the migrated test calls (T4) and the CLI call site (T5).
