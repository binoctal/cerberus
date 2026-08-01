# `protocol infer` Two-Pass Grounding (Option B) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development or superpowers:executing-plans, task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Land the verbatim handshake `await_type` (`devices:sync`) by splitting the work — pass 1 names candidate literals; code grep-extracts anchored source windows; pass 2 reads only those windows to select the guarded handshake and transcribe its literal off the anchored text.

**Architecture:** New `twopass.go` (`extractWindows`, `confirmSignalsTool`, `buildConfirmPrompt`, `refineSignals`, `parseConfirmation`). `inferOnce` runs pass 2 + merges when the pass-1 draft has a handshake/batch. The earlier block-quote `validateGrounding`/`normalizeWS`/`reasonUngrounded` are retired (replaced by code-extract + pass-2 confirmation). Composes with N-sample voting.

**Tech Stack:** Go 1.25, `internal/llm` (`DecideWithTools`, `Tool`), `internal/ai.Driver`, `internal/project`, `strings`.

## Global Constraints

- Commit author: `binoctal <binoctal@gmail.com>`, **no** `Co-Authored-By`.
- Commit messages and code comments in English.
- `make check` EXIT 0 + clean git tree after every task. No CGo. TDD.

## File Structure

- **Create** `internal/protocoldiscover/twopass.go` — `signalWindow`, `extractWindows`, `substringLineIndex`, `confirmSignalsTool`, `buildConfirmPrompt`, `signalConfirmation`, `parseConfirmation`, `refineSignals`, `joinCorpus`, `hasHardStructure`, `mergeConfirmation`.
- **Create** `internal/protocoldiscover/twopass_test.go` — unit tests for all of the above.
- **Modify** `internal/protocoldiscover/infer.go` — wire `refineSignals`+`mergeConfirmation` into `inferOnce`; retire `validateGrounding`/`normalizeWS`/`reasonUngrounded`; pass-1 prompt tweak.
- **Modify** `internal/protocoldiscover/infer_test.go` — replace grounding tests with two-pass tests.
- **Append** dogfood Run 22+ section.

**Mock note (read before Task 3):** a sample now makes TWO `DecideWithTools` calls with different prompts. Pass-1 prompt (from `buildInferPrompt`) contains the literal `"drafting a WebSocket protocol description"`; pass-2 prompt (from `buildConfirmPrompt`) contains `"ANCHORED SOURCE WINDOWS"`. Tests preset two `SetToolResponse` fixtures on these substrings so `MockClient.matchKey` routes each call correctly.

---

## Task 1: `extractWindows` (pure)

**Files:** Create `internal/protocoldiscover/twopass.go`, `twopass_test.go`.

- [ ] **Step 1: Write the failing test**

`internal/protocoldiscover/twopass_test.go`:

```go
package protocoldiscover

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractWindows_FoundAndAbsent(t *testing.T) {
	corpus := "line0\nline1 devices:sync here\nline2\nline3\nline4\nline5"
	ws := extractWindows(corpus, []string{"devices:sync", "nope"}, 1)
	require.Len(t, ws, 1)
	assert.Equal(t, "devices:sync", ws[0].literal)
	assert.Equal(t, "line0\nline1 devices:sync here\nline2", ws[0].text)
}

func TestExtractWindows_RadiusClamps(t *testing.T) {
	// match on first line -> window cannot go below 0
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
```

- [ ] **Step 2: Verify RED** — `go test ./internal/protocoldiscover/ -run TestExtractWindows -v` → undefined.

- [ ] **Step 3: Implement**

`internal/protocoldiscover/twopass.go`:

```go
package protocoldiscover

import (
	"context"
	"fmt"
	"strings"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

// signalWindow is a slice of source extracted around a candidate literal,
// fed to pass 2 so the model reads a small anchored region instead of the
// whole file.
type signalWindow struct {
	literal string
	text    string
}

// windowRadius is the number of source lines above and below each candidate
// literal that extractWindows includes. A few lines of context are enough to
// judge whether a send is guarded or whether an emit is a timer flush.
const windowRadius = 3

// extractWindows returns one window per distinct literal that appears in the
// corpus (±radius lines around its first match). Literals absent from the
// corpus (invented) yield no window. Pure.
func extractWindows(corpus string, literals []string, radius int) []signalWindow {
	lines := strings.Split(corpus, "\n")
	var out []signalWindow
	seen := map[string]bool{}
	for _, lit := range literals {
		if lit == "" || seen[lit] {
			continue
		}
		idx := substringLineIndex(lines, lit)
		if idx < 0 {
			continue
		}
		lo := idx - radius
		if lo < 0 {
			lo = 0
		}
		hi := idx + radius + 1
		if hi > len(lines) {
			hi = len(lines)
		}
		out = append(out, signalWindow{literal: lit, text: strings.Join(lines[lo:hi], "\n")})
		seen[lit] = true
	}
	return out
}

// substringLineIndex returns the 0-based index of the first line containing
// sub, or -1.
func substringLineIndex(lines []string, sub string) int {
	for i, l := range lines {
		if strings.Contains(l, sub) {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 4: Verify GREEN** — `go test ./internal/protocoldiscover/ -run TestExtractWindows -v`.
- [ ] **Step 5: `make check` + commit** — `feat(protocoldiscover): extractWindows pulls anchored source windows`. (`extractWindows`/`substringLineIndex` are used by tests; `signalWindow`/constants are referenced by later tasks.)

> If `make check` flags `signalWindow`/`windowRadius`/imports as unused (not test-referenced yet), temporarily reference them from the test (e.g. add `var _ = signalWindow{}` / `var _ = windowRadius`) and drop the guards in Task 3.

---

## Task 2: `confirmSignalsTool` + `buildConfirmPrompt`

**Files:** Modify `twopass.go`, `twopass_test.go`.

- [ ] **Step 1: Write the failing test** (append to `twopass_test.go`):

```go
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
	assert.Contains(t, p, "guarded") // selection cue
}
```

- [ ] **Step 2: Verify RED**.
- [ ] **Step 3: Implement** (append to `twopass.go`):

```go
// confirmSignalsTool is the pass-2 tool. The model has the anchored windows in
// its prompt (not in the tool input) and reports whether a guarded post-connect
// handshake and/or a timer-flush batch is present among them, transcribing the
// exact literals off the windows.
func confirmSignalsTool() llm.Tool {
	str := func() map[string]any { return map[string]any{"type": "string"} }
	return llm.Tool{
		Name:        "confirm_signals",
		Description: "Confirm the guarded post-connect handshake and the timer-flush batch by reading the provided anchored source windows. Call once.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"handshake": map[string]any{"type": "object", "properties": map[string]any{
					"present":    map[string]any{"type": "boolean", "description": "true if a window shows a post-connect send guarded by a condition (peer-gated handshake)."},
					"await_type": str(),
				}},
				"batch": map[string]any{"type": "object", "properties": map[string]any{
					"present":    map[string]any{"type": "boolean", "description": "true if a window shows a timer-flush emit coalescing items under a different routing key."},
					"flush_key":  str(),
					"item_type":  str(),
					"items_path": str(),
				}},
			},
			"required": []any{"handshake", "batch"},
		},
	}
}

// buildConfirmPrompt renders the anchored windows and instructs pass-2
// selection. The literal "ANCHORED SOURCE WINDOWS" doubles as the MockClient
// routing key in tests (it does not appear in pass-1 prompts).
func buildConfirmPrompt(windows []signalWindow) string {
	var b strings.Builder
	b.WriteString("ANCHORED SOURCE WINDOWS\n\n")
	b.WriteString("You are reading ONLY the anchored source windows below (each extracted around a candidate literal the first pass named). Judge them in isolation.\n\n")
	for i, w := range windows {
		fmt.Fprintf(&b, "--- window %d (around %q) ---\n%s\n\n", i+1, w.literal, w.text)
	}
	b.WriteString("Call confirm_signals once:\n")
	b.WriteString("- handshake.present=true ONLY if a window shows a post-connect send guarded by a condition (e.g. `if (peers.length > 0) ws.send(...)`); set await_type to the EXACT type: literal that guarded send emits. Otherwise present=false.\n")
	b.WriteString("- batch.present=true ONLY if a window shows a timer/flush emit that coalesces items under a DIFFERENT routing key; set flush_key (the flush routing key), item_type (the pre-batch routing key), and items_path (dotted path to the array). Otherwise present=false.\n")
	return b.String()
}
```

- [ ] **Step 4: Verify GREEN**.
- [ ] **Step 5: `make check` + commit** — `feat(protocoldiscover): confirm_signals tool + pass-2 prompt`.

---

## Task 3: `refineSignals` + `parseConfirmation` + merge helpers

**Files:** Modify `twopass.go`, `twopass_test.go`.

- [ ] **Step 1: Write the failing test** (append):

```go
func TestRefineSignals_ParsesConfirmation(t *testing.T) {
	draft := &project.Protocol{
		Roles: map[string]*project.ProtocolRole{"web": {Handshake: &project.RoleHandshake{AwaitType: "device:online"}}},
	}
	corpus := "if (onlineDevices.length > 0) ws.send type devices:sync\nbroadcastToWeb type device:online"
	// pass-2 mock: model selects the guarded devices:sync.
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
	// await_type not in corpus -> no window -> pass 2 not even called -> absent.
	draft := &project.Protocol{Roles: map[string]*project.ProtocolRole{"web": {Handshake: &project.RoleHandshake{AwaitType: "invented"}}}}
	conf, failed := refineSignals(context.Background(), ai.NewDriver(llm.NewMockClient(nil), ai.NewTokenBudget(1, 1)), draft, []SourceFile{{Content: "unrelated"}})
	require.False(t, failed)
	assert.False(t, conf.handshakePresent)
}

func TestMergeConfirmation_KeepsGroundedHandshakeDropsUngrounded(t *testing.T) {
	p := &project.Protocol{Roles: map[string]*project.ProtocolRole{"web": {Handshake: &project.RoleHandshake{AwaitType: "device:online", Timeout: 5}}}}
	mergeConfirmation(p, signalConfirmation{handshakePresent: true, awaitType: "devices:sync"}, "type devices:sync here")
	require.Contains(t, p.Roles, "web")
	assert.Equal(t, "devices:sync", p.Roles["web"].Handshake.AwaitType)
	assert.Equal(t, 5, p.Roles["web"].Handshake.Timeout, "timeout preserved from pass 1")

	mergeConfirmation(p, signalConfirmation{handshakePresent: false}, "x")
	assert.Nil(t, p.Roles["web"].Handshake, "unconfirmed handshake dropped")
}

func TestMergeConfirmation_Batch(t *testing.T) {
	p := &project.Protocol{}
	mergeConfirmation(p, signalConfirmation{batchPresent: true, flushKey: "session:output-batch", itemType: "session:output", itemsPath: "payload.lines"}, "session:output-batch session:output")
	require.Contains(t, p.Batches, "session:output-batch")
	assert.Equal(t, "payload.lines", p.Batches["session:output-batch"].ItemsPath)

	mergeConfirmation(p, signalConfirmation{batchPresent: false}, "x")
	assert.Empty(t, p.Batches)
}
```

- [ ] **Step 2: Verify RED**.
- [ ] **Step 3: Implement** (append to `twopass.go`; drop any temp unused-guards from Task 1):

```go
// signalConfirmation is the pass-2 verdict on the hard literals.
type signalConfirmation struct {
	handshakePresent bool
	awaitType        string
	batchPresent     bool
	flushKey         string
	itemType         string
	itemsPath        string
}

// joinCorpus concatenates input file contents into the search corpus.
func joinCorpus(inputs []SourceFile) string {
	var b strings.Builder
	for _, f := range inputs {
		b.WriteString(f.Content)
	}
	return b.String()
}

// hasHardStructure reports whether the draft carries anything pass 2 can
// refine (a role handshake or a batch).
func hasHardStructure(p *project.Protocol) bool {
	if len(p.Batches) > 0 {
		return true
	}
	for _, r := range p.Roles {
		if r != nil && r.Handshake != nil {
			return true
		}
	}
	return false
}

// refineSignals runs pass 2: gather candidate literals from the pass-1 draft,
// code-extract anchored windows, and (if any) ask the model to select the
// guarded handshake / flush off those windows. Returns (confirmation, failed):
// failed=true means the pass-2 call itself errored or drifted (the sample is
// dropped by voting). An empty candidate set (invented literals) returns a
// zero confirmation with failed=false — absence, not failure.
func refineSignals(ctx context.Context, driver *ai.Driver, draft *project.Protocol, inputs []SourceFile) (signalConfirmation, bool) {
	var literals []string
	for _, r := range draft.Roles {
		if r != nil && r.Handshake != nil && r.Handshake.AwaitType != "" {
			literals = append(literals, r.Handshake.AwaitType)
		}
	}
	for key := range draft.Batches {
		literals = append(literals, key)
	}
	corpus := joinCorpus(inputs)
	windows := extractWindows(corpus, literals, windowRadius)
	if len(windows) == 0 {
		return signalConfirmation{}, false
	}
	res, err := driver.DecideWithTools(ctx, buildConfirmPrompt(windows), []llm.Tool{confirmSignalsTool()})
	if err != nil || len(res.ToolCalls) == 0 {
		return signalConfirmation{}, true
	}
	return parseConfirmation(res.ToolCalls[0].Input, corpus), false
}

// parseConfirmation reads the confirm_signals tool input into a
// signalConfirmation, rejecting literals not present in the corpus (the model
// may still invent off-window). Leak-safe: carries no raw payload.
func parseConfirmation(input map[string]any, corpus string) signalConfirmation {
	var c signalConfirmation
	if hs, ok := input["handshake"].(map[string]any); ok {
		c.handshakePresent, _ = hs["present"].(bool)
		c.awaitType, _ = hs["await_type"].(string)
		if c.handshakePresent && c.awaitType != "" && !strings.Contains(corpus, c.awaitType) {
			c.handshakePresent = false
		}
	}
	if b, ok := input["batch"].(map[string]any); ok {
		c.batchPresent, _ = b["present"].(bool)
		c.flushKey, _ = b["flush_key"].(string)
		c.itemType, _ = b["item_type"].(string)
		c.itemsPath, _ = b["items_path"].(string)
		if c.batchPresent {
			if c.flushKey != "" && !strings.Contains(corpus, c.flushKey) {
				c.batchPresent = false
			}
			if c.itemType != "" && !strings.Contains(corpus, c.itemType) {
				c.batchPresent = false
			}
		}
	}
	return c
}

// mergeConfirmation applies the pass-2 verdict to the draft: keep a handshake
// only if confirmed (overwriting await_type, preserving timeout/optional), and
// replace batches with the single confirmed batch (or drop them).
func mergeConfirmation(p *project.Protocol, c signalConfirmation, corpus string) {
	_ = corpus
	for _, r := range p.Roles {
		if r == nil || r.Handshake == nil {
			continue
		}
		if c.handshakePresent && c.awaitType != "" {
			r.Handshake.AwaitType = c.awaitType
		} else {
			r.Handshake = nil
		}
	}
	if c.batchPresent && c.flushKey != "" && c.itemType != "" && c.itemsPath != "" {
		p.Batches = map[string]*project.ProtocolBatch{c.flushKey: {ItemType: c.itemType, ItemsPath: c.itemsPath}}
	} else {
		p.Batches = nil
	}
}
```

- [ ] **Step 4: Verify GREEN**.
- [ ] **Step 5: `make check` + commit** — `feat(protocoldiscover): refineSignals pass-2 + mergeConfirmation`.

---

## Task 4: Wire into `inferOnce`; retire block-quote grounding

**Files:** Modify `internal/protocoldiscover/infer.go`, `infer_test.go`.

- [ ] **Step 1: Write the failing test** (append to `infer_test.go`):

```go
// mockTwoPassDriver presets pass-1 (protocol_draft) and pass-2 (confirm_signals)
// fixtures. pass1 routes on the pass-1 prompt substring; pass2 on the pass-2
// "ANCHORED SOURCE WINDOWS" substring.
func mockTwoPassDriver(t *testing.T, pass1 map[string]any, pass2 map[string]any) *ai.Driver {
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
```

Also DELETE the now-obsolete tests: `TestValidateGrounding_*` (5), `TestInferOnce_UngroundedHandshake`, `TestInferOnce_GroundedHandshake`. And migrate `TestInfer_RolesPopulated` and `TestInfer_Voting_PicksHigherScored` back to NOT requiring `source` quotes (pass 1 no longer requires them; handshakes/batches now flow through pass 2). Simplest migration: those two tests' drafts have handshakes/batches → inferOnce will now attempt pass 2 via the single-fixture `mockToolDriver`, whose fixture matches the pass-1 prompt only; pass 2 gets no fixture → drift → sample failed. To keep those tests focused on their original concern (roles mapping / voting scoring) WITHOUT pass 2, give their drivers a pass-2 fixture too, OR remove the handshake/batch from their pass-1 fixtures. The latter is cleanest:

- `TestInfer_RolesPopulated`: drop the `handshake` block from the role (the test is about roles/params mapping, not handshake); keep `credential_ref` + `params`. Remove the now-extra `source` and input-file args added earlier; pass `nil` inputs and `, 1` samples.
- `TestInfer_Voting_PicksHigherScored`: the "complete" draft's edge is the handshake+batch. Under two-pass, pass 2 must confirm them. Give `mockSequenceDriver`'s infer call a pass-2 fixture by switching to `mockTwoPassDriver`-style: but sequence + two-pass is complex. Simpler: keep the test about scoring by making "complete" win on ROLES count instead of handshake/batch (give complete 2 roles vs partial's 1, no handshake/batch). Update assertion to check the extra role.

Apply those two migrations as part of this task's Step 4.

- [ ] **Step 2: Verify RED** — `go test ./internal/protocoldiscover/ -run "TestInferOnce_TwoPass" -v` (compile: functions exist from Task 3; the assertions fail because inferOnce does not yet call refineSignals).

- [ ] **Step 3: Wire `inferOnce`** — in `internal/protocoldiscover/infer.go`, replace the `validateGrounding` call site:

```go
	if err := project.ValidateProtocol(p, actorsOf(cfg)); err != nil {
		// The validation error references actor names, not credential values,
		// so it is safe to surface as actionable detail.
		return sample{outcome: outcomeFailed, reason: reasonInvalid, detail: err.Error()}
	}
	if hasHardStructure(p) {
		conf, failed := refineSignals(ctx, driver, p, inputs)
		if failed {
			return sample{outcome: outcomeFailed, reason: reasonInfra}
		}
		mergeConfirmation(p, conf, joinCorpus(inputs))
	}
	return sample{outcome: outcomeFound, proto: p}
}
```

- [ ] **Step 3b: Retire block-quote grounding** — delete `validateGrounding`, `normalizeWS`, the `reasonUngrounded` constant, and remove `reasonUngrounded` from `summarizeFailures`'s ordered list. Remove the now-unused `strings.Builder`/etc. only if they become unused (they will not — `joinCorpus` uses `strings.Builder` in twopass.go; infer.go keeps `strings` for `buildInferPrompt`).

- [ ] **Step 3c: Pass-1 prompt tweak** — in `buildInferPrompt`, drop the stern "MUST set handshake.source … rejected" and "MUST set the batch ... source" sentences (source no longer required). Replace with: `"For the handshake await_type and batch flush details, name your best-guess literals; a second pass verifies them against the source.\n"`. Keep the token-slot steer (Fix A). Update `TestBuildInferPrompt_RecognitionGuidance` accordingly: remove the `handshake.source` / `batch ... source` assertions; keep `verbatim`, `token slot`, and add `assert.Contains(prompt, "second pass")`.

- [ ] **Step 4: Migrate the 2 tests + run all** — apply the `TestInfer_RolesPopulated` and `TestInfer_Voting_PicksHigherScored` migrations described above; delete the obsolete grounding tests. Run `go test ./internal/protocoldiscover/ -v` → all PASS.

- [ ] **Step 5: `make check` + commit** — `feat(protocoldiscover): two-pass grounding in inferOnce; retire block-quote grounding`.

---

## Task 5: Dogfood two-pass against `open-agents`

**Files:** Append `cerberus-docs/technical/dogfood/2026-07-31-m3-3-protocol-infer-dogfood.md`.

- [ ] **Step 1: `make build`; start wrangler; confirm `/health`.
- [ ] **Step 2:** Run `cerberus protocol infer --name open-agents --from apps/api/src/realtime --service api --dry-run` 5–6 times. For each run record: did the draft carry a handshake, and was its `await_type` the verbatim `devices:sync`? Did it carry a batch with the correct flush key / items_path?
- [ ] **Step 3:** Append `## Two-pass grounding (Option B) — YYYY-MM-DD` with the per-run table and an honest verdict on whether `devices:sync` landed.
- [ ] **Step 4:** `git add ... && git commit -m "docs(dogfood): two-pass grounding run against open-agents"`.

---

## Self-Review

**Spec coverage:** §extractWindows → T1. §confirmSignalsTool + §buildConfirmPrompt → T2. §refineSignals + §parseConfirmation + §merge + §hasHardStructure + §joinCorpus → T3. §inferOnce merge + §retire validateGrounding + §prompt tweak → T4. §Testing → T1–T4. §Dogfood → T5. §Non-goals (no envelope/roles/auth change; voting/score/select unchanged) respected.

**Placeholder scan:** no TBD/TODO. T4's two migrations are spelled out concretely. The "temp unused-guards" note in T1 is explicit, not a placeholder.

**Type consistency:** `refineSignals(ctx, *ai.Driver, *project.Protocol, []SourceFile) (signalConfirmation, bool)` (T3) matches the `inferOnce` call (T4) and the `twopass_test` calls (T3). `mergeConfirmation(*project.Protocol, signalConfirmation, corpus)` (T3) matches T4. `extractWindows(corpus, []string, radius int) []signalWindow` (T1) matches refineSignals (T3) and its tests. `confirmSignalsTool() llm.Tool` (T2) matches refineSignals (T3). `signalConfirmation` fields (T3) match the test assertions and merge. The MockClient routing substrings (`"drafting a WebSocket protocol description"`, `"ANCHORED SOURCE WINDOWS"`) match the actual prompt literals in `buildInferPrompt`/`buildConfirmPrompt`.
