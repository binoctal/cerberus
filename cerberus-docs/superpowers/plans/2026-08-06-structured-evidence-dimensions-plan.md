# Structured Evidence by Dimension Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the Examiner judge structured evidence organized by five fixed assertable dimensions (count/membership/ordering/value/presence), so drift from missing evidence becomes detectable (uncertain) or resolvable — starting with WS `ws_flow` fan-out (`membership`).

**Architecture:** Two sources, by data shape. Source 1 = single-step dimensions on `EvidenceData.Dimensions` (the result's own facts). Source 2 = flow-level dimensions derived examiner-side by `deriveDimensions(StepResult)` from a richer per-step trace. `buildEvidenceContext` merges both into one dimension block; `buildJudgePrompt` prepends a "missing dimension → uncertain" guidance only when dimensions exist. Empty on both sources ⇒ byte-identical prompt (zero regression). Sender-exclusion is deferred (option 2: `membership` states recipients + sender, not exclusion).

**Tech Stack:** Go 1.25, pure-Go SQLite, existing `httptest` WS harness.

## Global Constraints

- Module: `github.com/binoctal/cerberus`, Go 1.25, no CGo.
- Commit author: `binoctal <binoctal@gmail.com>`, NO `Co-Authored-By`.
- Code comments and commit messages in English; match existing comment density.
- Empty dimensions ⇒ byte-identical prompt (hard no-regression gate, unit-tested).
- The five dimension kinds are a closed set: `count|membership|ordering|value|presence`.
- All docs go in `cerberus-docs/`, never `docs/`.

---

## File Structure

- `internal/types/result_types.go` — `Dimension` struct; `EvidenceData.Dimensions` (source 1).
- `internal/head/agent/types.go` — `Evidence` gains structured WS fields (source-2 input).
- `internal/head/agent/execute_phases_steps.go` — `runSteps` populates the new fields.
- `internal/head/examiner/dimensions.go` — NEW. `deriveDimensions(StepResult) []Dimension` (source 2: ws_flow fan-out).
- `internal/head/examiner/judge.go` — merge+render dimension block in `buildEvidenceContext`; gated guidance in `buildJudgePrompt`.
- `internal/head/examiner/judge_test.go` / `dimensions_test.go` — tests.
- `internal/head/examiner/vocab_validation_manual_test.go` — fan-out validation case + dimension-strip condition.
- `cerberus-docs/technical/validation/2026-08-06-structured-evidence-validation.md` — results.

---

### Task 1: `Dimension` type + `EvidenceData.Dimensions` + render + guidance (P0 source 1)

**Files:**
- Modify: `internal/types/result_types.go`
- Modify: `internal/head/examiner/judge.go`
- Test: `internal/head/examiner/judge_test.go`

**Interfaces:**
- Produces: `types.Dimension` struct; `types.EvidenceData.Dimensions []Dimension`; `(*Judge).renderDimensions([]Dimension) string`; the judge prompt gains a guidance block when dimensions exist.

- [ ] **Step 1: Write the failing test for source-1 rendering + guidance**

Append to `internal/head/examiner/judge_test.go`:

```go
// TestBuildJudgePromptIncludesDimensions verifies that single-step dimensions
// carried on the result's Evidence() are rendered and trigger the guidance.
func TestBuildJudgePromptIncludesDimensions(t *testing.T) {
	j := &Judge{config: ExaminerConfig{}}
	res := agent.StepResult{
		TestCase: &agent.TestCase{Name: "perm", Target: "https://x", Expectation: "approved is true"},
		Status:   agent.StepPassed,
		// Result whose Evidence() carries a value dimension.
	}
	res.Result = dimResult{dims: []types.Dimension{{
		Kind: "value", Label: "approval", Value: "approved=true",
	}}}
	got := j.buildJudgePrompt(res)
	if !strings.Contains(got, "Structured Evidence (by dimension)") {
		t.Fatalf("prompt missing dimension block:\n%s", got)
	}
	if !strings.Contains(got, "approved=true") {
		t.Fatalf("prompt missing the dimension fact:\n%s", got)
	}
	if !strings.Contains(got, "organized by") || !strings.Contains(got, "uncertain") {
		t.Fatalf("prompt missing the dimension guidance:\n%s", got)
	}
}

// TestBuildJudgePromptNoDimensionsUnchanged verifies the zero-regression gate:
// no dimensions ⇒ no block, no guidance.
func TestBuildJudgePromptNoDimensionsUnchanged(t *testing.T) {
	j := &Judge{config: ExaminerConfig{}}
	res := agent.StepResult{
		TestCase: &agent.TestCase{Name: "n", Target: "https://x", Expectation: "ok"},
		Status:   agent.StepPassed,
	}
	got := j.buildJudgePrompt(res)
	if strings.Contains(got, "Structured Evidence") || strings.Contains(got, "organized by") {
		t.Fatalf("no-dimension prompt should not mention dimensions:\n%s", got)
	}
}

// dimResult is a minimal ExecutorResult for dimension tests; it returns the
// given dimensions from Evidence(). Other methods are stubbed.
type dimResult struct{ dims []types.Dimension }

func (d dimResult) Success() bool               { return true }
func (d dimResult) Duration() time.Duration      { return 0 }
func (d dimResult) Summary() string              { return "dim stub" }
func (d dimResult) Evidence() types.EvidenceData {
	return types.EvidenceData{Type: "stub", Content: "stub", Dimensions: d.dims}
}
```

Add `"time"` and `github.com/binoctal/cerberus/internal/types` to the test file's imports if missing.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/head/examiner/ -run TestBuildJudgePromptIncludesDimensions -v`
Expected: FAIL / compile error — `types.Dimension` undefined, `types.EvidenceData.Dimensions` unknown.

- [ ] **Step 3: Add `Dimension` and `EvidenceData.Dimensions`**

In `internal/types/result_types.go`:

```go
// Dimension is one structured observation an executor recorded, classified by
// the assertable dimension it speaks to. Populate only the fields for its Kind.
// The judge decides whether the fact satisfies a claim; a dimension never
// carries a verdict, only observed facts.
type Dimension struct {
	Kind     string   `json:"kind"`               // count|membership|ordering|value|presence
	Label    string   `json:"label"`              // human/LLM-readable
	Recipients []string `json:"recipients,omitempty"` // membership: connections that received
	Sender     string   `json:"sender,omitempty"`     // membership: connection that sent
	Excluded   *bool    `json:"excluded,omitempty"`   // membership: only set when actively probed
	Count    int      `json:"count,omitempty"`     // count
	Value    string   `json:"value,omitempty"`     // value: "status=200", "approved=true"
	Present  *bool    `json:"present,omitempty"`   // presence
	Order    []string `json:"order,omitempty"`     // ordering
	Note     string   `json:"note,omitempty"`      // short supplement, not the primary signal
}
```

Add to `EvidenceData`:

```go
type EvidenceData struct {
	Type       string      `json:"type"`
	Content    string      `json:"content"`
	Encoding   string      `json:"encoding,omitempty"`
	Dimensions []Dimension `json:"dimensions,omitempty"`
}
```

- [ ] **Step 4: Add `renderDimensions` + merge into `buildEvidenceContext`; add gated guidance**

In `internal/head/examiner/judge.go`, add a renderer:

```go
// renderDimensions formats a merged dimension set as a prompt block. Returns ""
// when empty (so the caller emits nothing — zero regression for results without
// dimensions).
func renderDimensions(dims []Dimension) string {
	if len(dims) == 0 {
		return ""
	}
	var b []byte
	b = append(b, "Structured Evidence (by dimension):\n"...)
	for _, d := range dims {
		line := ""
		switch d.Kind {
		case "membership":
			line = fmt.Sprintf("recipients=%v", d.Recipients)
			if d.Sender != "" {
				line += fmt.Sprintf("; sender=%s", d.Sender)
			}
			if d.Excluded == nil {
				line += "; sender exclusion not probed"
			} else if *d.Excluded {
				line += "; sender excluded"
			} else {
				line += "; sender NOT excluded"
			}
		case "count":
			line = fmt.Sprintf("count=%d", d.Count)
		case "value":
			line = d.Value
		case "presence":
			if d.Present != nil {
				line = fmt.Sprintf("present=%v", *d.Present)
			} else {
				line = "present unknown"
			}
		case "ordering":
			line = fmt.Sprintf("order=%v", d.Order)
		}
		if d.Note != "" {
			line += " (" + d.Note + ")"
		}
		b = append(b, fmt.Sprintf("  [%s] %s: %s\n", d.Kind, d.Label, line)...)
	}
	return string(b)
}
```

In `buildEvidenceContext`, after the existing type-specific section and before the step-trace section, merge source 1 (result) and source 2 (derivation, still empty in this task — wire the call but `deriveDimensions` returns nil for now):

```go
	// Dimensions: merge result-carried (source 1) and flow-derived (source 2).
	dims := mergeDimensions(r.Result.Evidence().Dimensions, j.deriveDimensions(r))
```

(Define `j.deriveDimensions` as a nil-returning method here; Task 3 overrides it. Define `mergeDimensions` to concat + de-dup by `(Kind, Label)` with source-1 winning.) Append the rendered block when non-empty:

```go
	if d := renderDimensions(dims); d != "" {
		b = append(b, d...)
	}
```

In `buildJudgePrompt`, prepend the guidance only when the merged set is non-empty. Compute the same merged set once and pass it through — simplest is a small helper `(j *Judge) dimensionsFor(result) []Dimension` used by both `buildEvidenceContext` (to render) and `buildJudgePrompt` (to decide guidance). The guidance text (prepend to evidence when non-empty):

```
The evidence below is organized by dimension: count, membership, ordering, value, presence. Map each claim in the expectation to its matching dimension and check the typed fact. If the expectation depends on a dimension for which no evidence is listed, return uncertain with low confidence — do not infer the outcome from unrelated evidence.
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/head/examiner/ -run TestBuildJudgePrompt -v`
Expected: PASS for both new tests.

- [ ] **Step 6: Run the full examiner package (no-regression)**

Run: `go test ./internal/head/examiner/`
Expected: PASS — existing tests unchanged (their results carry no dimensions).

- [ ] **Step 7: Commit**

```bash
git add internal/types/result_types.go internal/head/examiner/judge.go internal/head/examiner/judge_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "feat(examiner): dimension block + missing-dimension guidance (source 1)"
```

---

### Task 2: Enrich the WS step trace (source-2 input)

`runSteps` currently writes `Evidence{Type, Content: "<action>: <summary>"}` and drops `ConnectionID` and the matched type, so the examiner cannot derive fan-out. Add structured fields.

**Files:**
- Modify: `internal/head/agent/types.go:118`
- Modify: `internal/head/agent/execute_phases_steps.go:106`
- Test: `internal/head/agent/execute_phases_steps_test.go`

**Interfaces:**
- Produces: `agent.Evidence` gains `Action`, `ConnectionID`, `MatchedType`, `Matched` fields; `runSteps` populates them per WS step.

- [ ] **Step 1: Write the failing test**

Append to `internal/head/agent/execute_phases_steps_test.go`:

```go
// TestRunStepsEvidenceCarriesConnectionAndType verifies the per-step trace
// records connectionID + matched type so downstream fan-out derivation works.
func TestRunStepsEvidenceCarriesConnectionAndType(t *testing.T) {
	// Build a minimal 2-step flow on an in-memory echo/relay server, then
	// assert the resulting Evidence entries carry the structured WS fields.
	// Reuse the existing newWSRelayServer / newWSExecutor helpers (see
	// websocket_test.go). If wiring a full stepExecution is heavy, instead
	// unit-test the evidence-construction helper directly (Step 3 extracts it).
	res := runStepsEvidenceForFlowFixture(t) // helper added in Step 3
	require.NotEmpty(t, res.Evidence)
	var rcv *Evidence
	for i := range res.Evidence {
		if res.Evidence[i].Action == "ws_receive" {
			rcv = &res.Evidence[i]
			break
		}
	}
	require.NotNil(t, rcv, "need a ws_receive evidence entry")
	require.NotEmpty(t, rcv.ConnectionID, "ws_receive evidence must carry connectionID")
	require.NotEmpty(t, rcv.MatchedType, "ws_receive evidence must carry the expected type")
}
```

If `newWSRelayServer` cannot stand up inside this test cheaply, fall back to unit-testing the extracted helper directly (the helper takes a step + result and returns an `Evidence`); that is the part under test.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/head/agent/ -run TestRunStepsEvidenceCarriesConnectionAndType -v`
Expected: FAIL — `Action`/`ConnectionID`/`MatchedType` fields undefined.

- [ ] **Step 3: Add fields + populate them**

In `internal/head/agent/types.go`, extend `Evidence`:

```go
type Evidence struct {
	Type         string `json:"type"`
	Content      string `json:"content"`
	Action       string `json:"action,omitempty"`        // ws_connect|ws_send|ws_receive|...
	ConnectionID string `json:"connection_id,omitempty"` // WS step's connection
	MatchedType  string `json:"matched_type,omitempty"`   // ws_receive expected type
	Matched      bool   `json:"matched,omitempty"`        // ws_receive observed a match
}
```

Extract the evidence-building in `runSteps` (execute_phases_steps.go:106) into a helper so it is testable, and populate the new fields. For a `ws_receive`, `MatchedType = s.Type` and `Matched =` the result matched (a `WSResult` with `MatchedCount > 0`, or `Success()`). For a `ws_send`, `MatchedType =` the `type` parsed out of `s.Message` (best-effort; empty on parse failure). `Action` and `ConnectionID` come from `s` for all steps:

```go
// stepEvidence builds one trace entry with the structured WS facts downstream
// fan-out derivation needs. Kept as a helper so the field population is
// unit-testable without standing up a live WS server.
func stepEvidence(s TestStep, result types.ExecutorResult) Evidence {
	ev := Evidence{
		Type:         evidenceType(result),
		Content:      fmt.Sprintf("%s: %s", s.Action, result.Summary()),
		Action:       s.Action,
		ConnectionID: s.ConnectionID,
	}
	if s.Action == "ws_receive" {
		ev.MatchedType = s.Type
		ev.Matched = wsReceiveMatched(result)
	}
	if s.Action == "ws_send" {
		ev.MatchedType = typeOfSend(s.Message)
	}
	return ev
}

// wsReceiveMatched reports whether a ws_receive result actually observed a
// matching frame (MatchedCount>0), distinct from Success() which can be true
// for a non-decisive receive.
func wsReceiveMatched(result types.ExecutorResult) bool {
	if wr, ok := result.(types.WSResult); ok {
		return wr.MatchedCount > 0 || wr.MatchedMessage != ""
	}
	return false
}

// typeOfSend best-effort extracts the "type" field from a ws_send JSON message
// so fan-out can correlate sender and recipients by message type.
func typeOfSend(msg string) string {
	var m map[string]any
	if json.Unmarshal([]byte(msg), &m) == nil {
		if t, ok := m["type"].(string); ok {
			return t
		}
	}
	return ""
}
```

Replace line 106's inline `Evidence{...}` with `stepEvidence(s, result)`. Add `"encoding/json"` to the file's imports.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/head/agent/ -run TestRunStepsEvidenceCarriesConnectionAndType -v`
Expected: PASS.

- [ ] **Step 5: Run the agent package (no-regression)**

Run: `go test ./internal/head/agent/`
Expected: PASS — new fields are `omitempty`, existing evidence consumers unaffected.

- [ ] **Step 6: Commit**

```bash
git add internal/head/agent/types.go internal/head/agent/execute_phases_steps.go internal/head/agent/execute_phases_steps_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "feat(agent): carry connectionID + matched type in step evidence"
```

---

### Task 3: `deriveDimensions` — ws_flow fan-out → `membership` (source 2)

**Files:**
- Create: `internal/head/examiner/dimensions.go`
- Test: `internal/head/examiner/dimensions_test.go`
- Modify: `internal/head/examiner/judge.go` (replace the nil stub from Task 1)

**Interfaces:**
- Consumes: `agent.Evidence` structured fields (Task 2).
- Produces: `(j *Judge) deriveDimensions(agent.StepResult) []Dimension` — for each broadcast type present, one `membership` Dimension `{Kind:"membership", Label:"<type> recipients", Recipients:[...], Sender:"..."}`. `Excluded` left nil (option 2 — not probed this spec).

- [ ] **Step 1: Write the failing test**

Create `internal/head/examiner/dimensions_test.go`:

```go
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

func TestDeriveDimensions_NoFanOutEmpty(t *testing.T) {
	j := &Judge{}
	res := agent.StepResult{
		TestCase: &agent.TestCase{ID: "x"},
		Evidence: []agent.Evidence{
			{Action: "ws_send", ConnectionID: "c-web", MatchedType: "workflow:task_progress"},
			{Action: "ws_receive", ConnectionID: "c-bridge", MatchedType: "workflow:task_progress", Matched: true},
		},
	}
	// One sender, one recipient → still a membership fact (recipient set).
	dims := j.deriveDimensions(res)
	require.Len(t, dims, 1)
	assert.ElementsMatch(t, []string{"c-bridge"}, dims[0].Recipients)
}

func TestDeriveDimensions_EmptyTrace(t *testing.T) {
	j := &Judge{}
	dims := j.deriveDimensions(agent.StepResult{TestCase: &agent.TestCase{ID: "x"}})
	assert.Empty(t, dims)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/head/examiner/ -run TestDeriveDimensions -v`
Expected: FAIL — `deriveDimensions` is the nil stub.

- [ ] **Step 3: Implement `deriveDimensions`**

Create `internal/head/examiner/dimensions.go`:

```go
package examiner

import (
	"sort"

	"github.com/binoctal/cerberus/internal/head/agent"
)

// deriveDimensions produces flow-level dimensions (source 2) from a StepResult's
// per-step trace. Currently it derives membership: for each message type that a
// ws_send sent, the recipients are the connections whose ws_receive matched it,
// and the sender is the ws_send connection. Excluded is left nil — proving
// sender exclusion requires an active probe (see spec "Exclusion requires an
// active probe"), deferred.
func (j *Judge) deriveDimensions(r agent.StepResult) []Dimension {
	senders := map[string]string{}      // type -> sender connectionID
	recipients := map[string]map[string]bool{} // type -> set of recipient connectionIDs
	for _, ev := range r.Evidence {
		if ev.MatchedType == "" {
			continue
		}
		switch ev.Action {
		case "ws_send":
			if _, ok := senders[ev.MatchedType]; !ok {
				senders[ev.MatchedType] = ev.ConnectionID
			}
			if recipients[ev.MatchedType] == nil {
				recipients[ev.MatchedType] = map[string]bool{}
			}
		case "ws_receive":
			if ev.Matched {
				if recipients[ev.MatchedType] == nil {
					recipients[ev.MatchedType] = map[string]bool{}
				}
				recipients[ev.MatchedType][ev.ConnectionID] = true
			}
		}
	}
	if len(senders) == 0 {
		return nil
	}
	types := make([]string, 0, len(senders))
	for t := range senders {
		types = append(types, t)
	}
	sort.Strings(types)
	out := make([]Dimension, 0, len(types))
	for _, t := range types {
		rcv := make([]string, 0, len(recipients[t]))
		for c := range recipients[t] {
			rcv = append(rcv, c)
		}
		sort.Strings(rcv)
		out = append(out, Dimension{
			Kind:       "membership",
			Label:      t + " recipients",
			Recipients: rcv,
			Sender:     senders[t],
		})
	}
	return out
}
```

In `internal/head/examiner/judge.go`, delete the nil stub from Task 1 (the real method in `dimensions.go` now satisfies the call in `buildEvidenceContext`).

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/head/examiner/ -run TestDeriveDimensions -v`
Expected: PASS.

- [ ] **Step 5: Run the full examiner package**

Run: `go test ./internal/head/examiner/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/head/examiner/dimensions.go internal/head/examiner/dimensions_test.go internal/head/examiner/judge.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "feat(examiner): derive ws_flow membership dimension from step trace"
```

---

### Task 4: Validation — fan-out case + dimension-strip condition

**Files:**
- Modify: `internal/head/examiner/vocab_validation_manual_test.go`
- Create: `cerberus-docs/technical/validation/2026-08-06-structured-evidence-validation.md`

**Interfaces:**
- Consumes: `agent.Evidence` structured fields, `Judge.deriveDimensions`.

- [ ] **Step 1: Add a fan-out validation case + a strip condition**

In `vocab_validation_manual_test.go`, add a case to `buildValidationCases` whose `StepResult.Evidence` is a real fan-out trace (sender + ≥2 recipients of the same type), so `deriveDimensions` yields a `membership` dimension. Then add a second condition that strips the derived dimension before judging (judge the same results with `deriveDimensions` disabled), `N=3` each.

Concretely: extend `buildValidationCases` with:

```go
{
	name: "fanout",
	result: agent.StepResult{
		TestCase: &agent.TestCase{
			ID: "vc-fanout", Name: "fanout", Target: "ws://localhost:8989/ws",
			Expectation: "the broadcast reaches the other web peers",
		},
		Status:   agent.StepPassed,
		Attempts: 1,
		Result:   types.WSResult{OK: true, MatchedMessage: `{"type":"workflow:task_progress","payload":{"pct":50}}`},
		Evidence: []agent.Evidence{
			{Action: "ws_send", ConnectionID: "c-web", MatchedType: "workflow:task_progress", Content: "ws_send: ok"},
			{Action: "ws_receive", ConnectionID: "c-bridge", MatchedType: "workflow:task_progress", Matched: true, Content: "ws_receive: matched"},
			{Action: "ws_receive", ConnectionID: "c-web-2", MatchedType: "workflow:task_progress", Matched: true, Content: "ws_receive: matched"},
		},
	},
},
```

Add a `derive` toggle to the condition struct and pass it into the judge construction (when false, use a judge whose `deriveDimensions` is disabled — simplest: a bool field `Judge.deriveEnabled` defaulted true, set false for the strip condition). Run `with-dimensions` and `no-dimensions` (strip), `N=3`.

- [ ] **Step 2: Run the validation manually**

Run: `go test -tags=manual ./internal/head/examiner/ -run TestExaminerVocabValidation -v -timeout=600s`
Expected: it runs (skips if no creds). Record the per-case status/conf for the `fanout` case under both conditions.

- [ ] **Step 3: Write the results doc**

Create `cerberus-docs/technical/validation/2026-08-06-structured-evidence-validation.md` with the setup, the drift table for the `fanout` case (with-dimensions vs no-dimensions, N=3), and an honest conclusion: did the `membership` dimension lower drift vs the stripped condition? Note this is the soft-guidance effectiveness measurement; a null result is valid and reportable.

- [ ] **Step 4: Commit**

```bash
git add internal/head/examiner/vocab_validation_manual_test.go cerberus-docs/technical/validation/2026-08-06-structured-evidence-validation.md
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "test(examiner): fan-out membership validation + result"
```

---

### Task 5: Full verification

- [ ] **Step 1: Format and vet**

Run: `make fmt && go vet ./... && go vet -tags=manual ./...`
Expected: clean.

- [ ] **Step 2: Lint**

Run: `make lint`
Expected: clean.

- [ ] **Step 3: Full test suite**

Run: `make test`
Expected: PASS, race-clean.

- [ ] **Step 4: Commit any fmt drift**

```bash
git add -A
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "chore: fmt after structured evidence dimensions"
```

---

## Self-Review Notes

- **Spec coverage:** P0 mechanism → Task 1. Source-2 input enrichment → Task 2. `deriveDimensions` fan-out → Task 3. Validation → Task 4. Exclusion is deferred (option 2: `Excluded` nil, renderer says "not probed") — matches spec scope. `mergeDimensions` + `dimensionsFor` helpers (Task 1) connect the two sources.
- **Placeholder scan:** every code step shows actual code. Task 2 Step 1 offers a fallback (unit-test the helper directly) if the in-memory WS harness is heavy; the helper is the part under test.
- **Type consistency:** `types.Dimension` (Task 1) matches `EvidenceData.Dimensions`, `renderDimensions`, and `deriveDimensions` return type. `agent.Evidence` fields (Task 2) match what `deriveDimensions` reads (Task 3).
- **Known carry-over:** the `Judge.deriveEnabled` toggle (Task 4) and `dimensionsFor`/`mergeDimensions` helpers (Task 1) are named here and referenced consistently.
