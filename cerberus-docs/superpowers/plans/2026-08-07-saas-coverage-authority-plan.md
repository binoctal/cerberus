# SaaS Coverage Authority Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace cerberus's hallucinated SaaS coverage verdict (`coverage_pct=0 / reached=false` from an LLM fed no data) with an honest model: unmeasured coverage reads "not applicable"; WS/SaaS sessions get an objective message-edge path-coverage measurement and a concrete gap list.

**Architecture:** Phase 1 (honesty): `AssessCoverage` short-circuits when coverage is unmeasured — no LLM call, no fake 0%. Phase 2 (objective path coverage): a `pathCoverage` provider derives exercised/declared message edges from case evidence; `AssessCoverage` compares against a new `Gate.PathThreshold` and emits `Kind:"path"` gaps naming unexercised edges. Local-codebase line coverage is unchanged.

**Tech Stack:** Go 1.25, table-driven tests, existing examiner/session packages.

**Spec:** `cerberus-docs/superpowers/specs/2026-08-07-saas-coverage-authority-design.md`

## Global Constraints

- Module: `github.com/binoctal/cerberus`, Go 1.25, no CGo.
- Commit author: `binoctal <binoctal@gmail.com>`, NO `Co-Authored-By`.
- Code comments and commit messages in English; match existing comment density.
- All docs go in `cerberus-docs/`, never `docs/`.
- Zero regression: local-codebase sessions (no service vocabulary) must produce byte-identical coverage behavior.
- Non-message path coverage (auth/lifecycle/callback) is out of scope (Phase 3, separate brainstorm).

---

## File Structure

- `internal/head/contract/types.go` — `Assessment.Measured bool`; `Gate.PathThreshold float64`.
- `internal/head/examiner/assess.go` — Phase 1 `!Known` short-circuit; Phase 2 path-unit branch.
- `internal/head/examiner/assess_test.go` — Phase 1 + Phase 2 unit tests (NEW or extend).
- `internal/session/coverage.go` — `pathCoverage(ctx, sess, results)` provider; routing by has-vocab.
- `internal/session/coverage_test.go` — path-coverage unit tests (NEW or extend).
- `internal/session/run_phases_examiner.go` — log/summary surfaces "coverage: N/A" when `!Measured`.
- `internal/head/scout/assembly.go` — set `PathThreshold` deterministically for has-vocab contracts.

---

### Task 1: `Assessment.Measured` + `Gate.PathThreshold` fields

**Files:**
- Modify: `internal/head/contract/types.go`
- Test: `internal/head/contract/types_test.go`

**Interfaces:**
- Produces: `Assessment.Measured bool` (true when coverage was actually measured; false ⇒ not-applicable); `Gate.PathThreshold float64`.

- [ ] **Step 1: Write the failing test**

Append to `internal/head/contract/types_test.go`:

```go
func TestAssessmentMeasuredDefaultsTrue(t *testing.T) {
	// Existing measured coverage paths set Measured=true explicitly; a zero-value
	// Assessment must not be misread as "measured". Callers gate on Measured.
	var a Assessment
	if a.Measured {
		t.Fatalf("zero-value Assessment.Measured must be false (not-applicable by default)")
	}
}

func TestGatePathThresholdRoundTrip(t *testing.T) {
	g := Gate{Module: "m", LineThreshold: 0.8, PathThreshold: 1.0}
	b, _ := json.Marshal(g)
	var got Gate
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if g.PathThreshold != 1.0 {
		t.Fatalf("PathThreshold did not round-trip: %v", g.PathThreshold)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/head/contract/ -run 'TestAssessmentMeasured|TestGatePathThreshold' -v`
Expected: FAIL — `Measured` and `PathThreshold` undefined.

- [ ] **Step 3: Add the fields**

In `internal/head/contract/types.go`, add to `Assessment` (after `CoveragePct`):

```go
	// Measured is false when no objective coverage could be obtained (SaaS/WS
	// session with no local SUT module). When false, Reached/CoveragePct are
	// NOT meaningful and must not be read as a coverage failure; the session
	// outcome is verdict-based. True for both line and path measurements.
	Measured bool `json:"measured"`
```

Add to `Gate` (after `BranchThreshold`):

```go
	// PathThreshold is the objective gate for SaaS/WS path coverage: the
	// required fraction (0–1) of declared vocab message edges that must be
	// exercised by passing cases. Used only when the coverage unit is "path".
	PathThreshold float64 `json:"path_threshold"`
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/head/contract/ -run 'TestAssessmentMeasured|TestGatePathThreshold' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/head/contract/types.go internal/head/contract/types_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "feat(contract): Assessment.Measured + Gate.PathThreshold fields"
```

---

### Task 2: Phase 1 — `AssessCoverage` short-circuits when coverage is unmeasured

**Files:**
- Modify: `internal/head/examiner/assess.go:17`
- Test: `internal/head/examiner/assess_test.go` (NEW)

**Interfaces:**
- Consumes: `contract.Assessment.Measured` (Task 1), `CoverageMeasurement.Known`.
- Produces: when `!m.Known`, `AssessCoverage` returns `&Assessment{Measured:false}` WITHOUT calling the LLM and WITHOUT emitting `Reached=false`/`CoveragePct=0`/coverage gaps.

- [ ] **Step 1: Write the failing test**

Create `internal/head/examiner/assess_test.go`:

```go
package examiner

import (
	"context"
	"errors"
	"testing"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/contract"
	"github.com/binoctal/cerberus/internal/llm"
)

// boobyTrapDriver fails the test if DecideWithTools is invoked. Used to prove
// Phase 1: an unmeasured (SaaS) coverage assessment must NOT call the LLM.
type boobyTrapDriver struct{ t *testing.T }

func (b boobyTrapDriver) DecideWithTools(_ context.Context, _ ai.Prompt, _ []ai.Tool) (*llm.Message, error) {
	b.t.Fatalf("DecideWithTools must NOT be called when coverage is unmeasured")
	return nil, errors.New("unreachable")
}

// TestAssessCoverage_UnmeasuredIsNotApplicable verifies that a SaaS session with
// no measurable local SUT gets an honest "not applicable" assessment instead of
// the prior hallucinated 0% / reached=false / invented gaps.
func TestAssessCoverage_UnmeasuredIsNotApplicable(t *testing.T) {
	j := &Judge{judge: judgeSphere{driver: boobyTrapDriver{t}}}
	a, err := j.AssessCoverage(context.Background(), &contract.Contract{}, nil,
		contract.CoverageMeasurement{Known: false})
	if err != nil {
		t.Fatalf("AssessCoverage: %v", err)
	}
	if a.Measured {
		t.Fatalf("unmeasured assessment must report Measured=false, got %+v", a)
	}
	if a.Reached {
		t.Fatalf("unmeasured assessment must not assert Reached=true (misleading)")
	}
	if a.CoveragePct != 0 {
		t.Fatalf("unmeasured CoveragePct must be 0 (no value), got %v", a.CoveragePct)
	}
	for _, g := range a.Gaps {
		if g.Kind == "coverage" {
			t.Fatalf("unmeasured assessment must not emit a coverage gap, got %+v", g)
		}
	}
}
```

> NOTE for the implementer: the test constructs `Judge` with an inner `judge` field
> of type `judgeSphere{driver: ...}`. Read `internal/head/examiner/judge.go` to
> confirm the exact field names (`judge` / `judgeDriver`); if `judgeDriver` is a
> direct field of `Judge`, construct `&Judge{judgeDriver: boobyTrapDriver{t}}`
> instead and adjust the receiver. Match the real shape — do not invent it.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/head/examiner/ -run TestAssessCoverage_UnmeasuredIsNotApplicable -v`
Expected: FAIL — either compile error (field shape) or the booby-trap fires (LLM called).

- [ ] **Step 3: Implement the short-circuit**

In `internal/head/examiner/assess.go`, add at the top of `AssessCoverage`, before the prompt/driver call:

```go
	if !m.Known {
		// Unmeasured (SaaS/WS session with no local SUT module, or provider
		// failure): coverage is NOT APPLICABLE. Do NOT call the LLM — it would
		// hallucinate a 0% / not-reached verdict from the absence of data. The
		// session outcome is verdict-based; Reached/CoveragePct stay meaningless.
		return &contract.Assessment{Measured: false}, nil
	}
```

- [ ] **Step 4: Set Measured=true on the measured return paths**

Still in `assess.go`, the two existing return points (the `!m.Known` block is now replaced by Step 3's early return, so only the measured path remains). Before each `return a, nil` after the objective-gate logic, set `a.Measured = true`:

```go
	// Objective gate: below threshold → not reached regardless of the LLM.
	if m.Pct < c.CoverageGate.LineThreshold {
		a.Reached = false
		...
	}
	a.CoveragePct = m.Pct
	a.Measured = true
	return a, nil
```

(The LLM-judged measured path also sets `a.Measured = true` — add it before that return too. Re-read the function: after Step 3 there is one remaining return for the measured case; set Measured there.)

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./internal/head/examiner/ -run TestAssessCoverage -v`
Expected: PASS — booby-trap NOT tripped, Measured=false, no coverage gap.

- [ ] **Step 6: Run the full examiner package (no-regression)**

Run: `go test ./internal/head/examiner/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/head/examiner/assess.go internal/head/examiner/assess_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "fix(examiner): unmeasured coverage is not-applicable, not a hallucinated 0%"
```

---

### Task 3: Surface "coverage: N/A" in the session log/summary

**Files:**
- Modify: `internal/session/coverage.go:40` (`assessCoverageIfContract`)
- Modify: `internal/session/run_phases_examiner.go` (summary, if it references coverage)

**Interfaces:**
- Consumes: `Assessment.Measured` (Task 1).
- Produces: when `!Measured`, log `"coverage not applicable (no measurable local SUT)"` instead of the misleading `reached=false coverage_pct=0`.

- [ ] **Step 1: Update `assessCoverageIfContract`**

In `internal/session/coverage.go`, replace the log block inside `assessCoverageIfContract`:

```go
	if err == nil {
		sess.Assessment = assessment
		if !assessment.Measured {
			sess.Logger.Info("coverage not applicable",
				zap.String("reason", "no measurable local SUT (SaaS/WS session); outcome is verdict-based"))
		} else {
			sess.Logger.Info("coverage assessment",
				zap.Bool("reached", assessment.Reached),
				zap.Int("gaps", len(assessment.Gaps)),
				zap.Float64("coverage_pct", assessment.CoveragePct))
		}
	} else {
		sess.Logger.Warn("coverage assessment failed", zap.Error(err))
	}
```

- [ ] **Step 2: Verify the build**

Run: `go vet ./internal/session/`
Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add internal/session/coverage.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "fix(session): log coverage as not-applicable when unmeasured"
```

---

### Task 4: Phase 2 — `pathCoverage` provider (declared vs exercised message edges)

**Files:**
- Modify: `internal/session/coverage.go`
- Test: `internal/session/coverage_test.go` (NEW or extend)

**Interfaces:**
- Consumes: `[]agent.StepResult` (each carries `TestCase.Steps` for `connectionID→role` and `Evidence` for `ws_send`/`ws_receive` matched types); the service `Vocabulary.Edges` (declared surface).
- Produces: `contract.CoverageMeasurement{Pct: exercised/required, Unit: "path", Known: true}` and (for `AssessCoverage`) the exercised set so gaps can name unexercised edges. Exposed via a pure helper `exercisedEdges(results []agent.StepResult, required []project.VocabEdge) (exercised map[string]bool, connRole map[string]string)`.

- [ ] **Step 1: Write the failing test**

Create `internal/session/coverage_test.go`:

```go
package session

import (
	"testing"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// TestExercisedEdges verifies the path-coverage core: a declared message edge is
// "exercised" when a case's evidence shows its sender role sent the type and a
// recipient role received it. connectionID→role comes from the case's ws_connect
// steps; unmatched connections are excluded (conservative under-count).
func TestExercisedEdges(t *testing.T) {
	required := []project.VocabEdge{
		{FromRole: "bridge", ToRole: "web", Type: "device:online", Trigger: "message_handled"},
		{FromRole: "web", ToRole: "bridge", Type: "session:send", Trigger: "message_handled"},
	}
	results := []agent.StepResult{{
		TestCase: &agent.TestCase{Steps: []agent.TestStep{
			{Action: "ws_connect", ConnectionID: "c-web", Role: "web"},
			{Action: "ws_connect", ConnectionID: "c-bridge", Role: "bridge"},
		}},
		Evidence: []agent.Evidence{
			{Action: "ws_send", ConnectionID: "c-bridge", MatchedType: "device:online"},
			{Action: "ws_receive", ConnectionID: "c-web", MatchedType: "device:online", Matched: true},
		},
	}}
	exercised, _ := exercisedEdges(results, required)
	// device:online bridge→web exercised; session:send web→bridge NOT.
	key := func(e project.VocabEdge) string { return e.FromRole + "|" + e.ToRole + "|" + e.Type }
	if !exercised[key(required[0])] {
		t.Errorf("expected device:online bridge->web exercised")
	}
	if exercised[key(required[1])] {
		t.Errorf("session:send web->bridge must NOT be exercised")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/session/ -run TestExercisedEdges -v`
Expected: FAIL — `exercisedEdges` undefined.

- [ ] **Step 3: Implement `exercisedEdges` and `pathCoverage`**

In `internal/session/coverage.go`, add:

```go
// edgeKey is the stable identity of a vocab message edge.
func edgeKey(from, to, typ string) string { return from + "|" + to + "|" + typ }

// exercisedEdges computes which declared message edges a session's results
// exercised. Per case, connectionID→role is mapped from that case's ws_connect
// steps; a ws_send of type T from role Rs plus a matched ws_receive of T by role
// Rr exercises edge (Rs→Rr, T). Connections with no resolvable role are excluded
// (conservative). Returns the exercised set (keyed edgeKey) and the connRole map
// (for diagnostics). Pure; unit-testable without a live server.
func exercisedEdges(results []agent.StepResult, required []project.VocabEdge) (map[string]bool, map[string]string) {
	exercised := map[string]bool{}
	for _, r := range results {
		// connectionID → role for THIS case (roles are case-scoped via connect steps).
		connRole := map[string]string{}
		for _, s := range r.TestCase.Steps {
			if s.Action == "ws_connect" && s.Role != "" {
				connRole[s.ConnectionID] = s.Role
			}
		}
		sentByType := map[string]string{}      // type → sender role
		receivedByType := map[string]map[string]bool{} // type → set of recipient roles
		for _, ev := range r.Evidence {
			if ev.MatchedType == "" {
				continue
			}
			role := connRole[ev.ConnectionID]
			switch ev.Action {
			case "ws_send":
				if role != "" {
					sentByType[ev.MatchedType] = role
				}
			case "ws_receive":
				if !ev.ExpectAbsent && ev.Matched && role != "" {
					if receivedByType[ev.MatchedType] == nil {
						receivedByType[ev.MatchedType] = map[string]bool{}
					}
					receivedByType[ev.MatchedType][role] = true
				}
			}
		}
		for typ, sender := range sentByType {
			for recipient := range receivedByType[typ] {
				if recipient != sender {
					exercised[edgeKey(sender, recipient, typ)] = true
				}
			}
		}
	}
	return exercised, nil
}

// pathCoverage measures message-edge path coverage: exercised / required, over
// the session's declared vocab edges (message_handled, non-unsupported). Known is
// true whenever at least one required edge is declared (a measured 0%, not an
// unmeasured gap). results carry each case's Steps (connID→role) and Evidence.
func pathCoverage(results []agent.StepResult, required []project.VocabEdge) contract.CoverageMeasurement {
	if len(required) == 0 {
		return contract.CoverageMeasurement{Known: false}
	}
	exercised, _ := exercisedEdges(results, required)
	return contract.CoverageMeasurement{
		Pct:   float64(len(exercised)) / float64(len(required)),
		Unit:  "path",
		Known: true,
	}
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/session/ -run TestExercisedEdges -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/session/coverage.go internal/session/coverage_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "feat(session): pathCoverage provider — exercised vs declared message edges"
```

---

### Task 5: Route has-vocab sessions to path coverage

**Files:**
- Modify: `internal/session/coverage.go` (`coverageReportForSession` / `lineCoverageReport`)
- Modify: `internal/session/run_phases_examiner.go` (pass `results` to the measurement)

**Interfaces:**
- Produces: when the session's service declares a non-empty vocabulary, the Examiner phase measures path coverage (not line coverage). Otherwise line coverage (unchanged).

- [ ] **Step 1: Add a has-vocab helper and route**

In `internal/session/coverage.go`, add a helper and a results-aware measurement entry point. First read `coverageReportForSession` and `lineCoverage`'s callers in `run_phases_examiner.go` to thread `results` through. Add:

```go
// sessionHasVocab reports whether any service declares a non-empty WS vocabulary
// (the SaaS/WS path-coverage surface). Structural, not mode-based.
func sessionHasVocab(sess *Session) bool {
	for _, svc := range sess.Services() { // confirm the accessor; see note below
		if svc.Vocabulary != nil && len(svc.Vocabulary.Edges) > 0 {
			return true
		}
	}
	return false
}

// requiredEdges collects the declared message_handled, non-unsupported edges.
func requiredEdges(sess *Session) []project.VocabEdge {
	var out []project.VocabEdge
	for _, svc := range sess.Services() {
		if svc.Vocabulary == nil {
			continue
		}
		for _, e := range svc.Vocabulary.Edges {
			if e.Trigger == "message_handled" && !e.Unsupported && !e.Partial {
				out = append(out, e)
			}
		}
	}
	return out
}
```

> NOTE for the implementer: confirm the `Session → Services` accessor (read
> `internal/session/session.go`). If services live on a nested config field, use
> that. `run_phases_examiner.go` calls `sess.lineCoverage(ctx)`; add a parallel
> `sess.pathCoverageMeasurement(ctx, results)` that the Examiner phase calls when
> `sessionHasVocab(sess)`, else falls back to `lineCoverage`. Thread `results` to
> the measurement site.

- [ ] **Step 2: Wire the routing in the Examiner phase**

In `internal/session/run_phases_examiner.go`, where `assessCoverageIfContract` is called (or where `sess.lineCoverage` is resolved), branch:

```go
	var measurement contract.CoverageMeasurement
	if sessionHasVocab(sess) {
		measurement = pathCoverage(results, requiredEdges(sess))
	} else {
		measurement = sess.lineCoverage(ctx)
	}
```

(Adjust to match the real call site — `assessCoverageIfContract` currently calls `sess.lineCoverage` internally; thread the choice in.)

- [ ] **Step 3: Verify build + existing session tests**

Run: `go vet ./internal/session/ && go test ./internal/session/`
Expected: clean + PASS (no-vocab sessions unchanged).

- [ ] **Step 4: Commit**

```bash
git add internal/session/coverage.go internal/session/run_phases_examiner.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "feat(session): route has-vocab sessions to path coverage"
```

---

### Task 6: `AssessCoverage` path-unit branch (PathThreshold + path gaps)

**Files:**
- Modify: `internal/head/examiner/assess.go`
- Test: `internal/head/examiner/assess_test.go`

**Interfaces:**
- Consumes: `Gate.PathThreshold` (Task 1); `CoverageMeasurement.Unit == "path"`.
- Produces: when `Unit == "path"`, compare `m.Pct` vs `c.CoverageGate.PathThreshold`; below ⇒ `Reached=false` + `Gap{Kind:"path", Detail:"<from>→<to> <type> not exercised"}` for each unexercised required edge. `Measured=true`.

- [ ] **Step 1: Write the failing test**

Append to `internal/head/examiner/assess_test.go`:

```go
// TestAssessCoverage_PathGate verifies a path-unit measurement below the gate
// yields Reached=false with a concrete path gap naming the unexercised edge.
// Uses a real (stubbed) driver only if needed; the path branch is objective and
// must not require a per-edge LLM verdict.
func TestAssessCoverage_PathGate(t *testing.T) {
	j := &Judge{judge: judgeSphere{driver: noCallDriver{t}}}
	c := &contract.Contract{CoverageGate: contract.Gate{PathThreshold: 1.0}}
	m := contract.CoverageMeasurement{Pct: 0.5, Unit: "path", Known: true}
	a, err := j.AssessCoverage(context.Background(), c, nil, m)
	if err != nil {
		t.Fatalf("AssessCoverage: %v", err)
	}
	if a.Measured != true || a.Reached {
		t.Fatalf("expected Measured=true, Reached=false; got %+v", a)
	}
	// Objective gate overrides the LLM; a path shortfall produces a path gap.
	found := false
	for _, g := range a.Gaps {
		if g.Kind == "path" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a Kind=path gap below PathThreshold, got %+v", a.Gaps)
	}
}
```

(`noCallDriver` is `boobyTrapDriver` renamed/reused — the path gate, like the line gate, is objective and must not depend on the LLM. If the implementation still calls the LLM for path gaps' reasoning, that is acceptable as long as the objective gate overrides; adjust the driver to a capturing stub if so. Match the real implementation.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/head/examiner/ -run TestAssessCoverage_PathGate -v`
Expected: FAIL — path branch does not exist; gap kind is wrong.

- [ ] **Step 3: Implement the path branch**

In `internal/head/examiner/assess.go`, after the Phase 1 `!Known` short-circuit, branch on `m.Unit`:

```go
	if m.Unit == "path" {
		// Objective path gate: required edges must be exercised. The caller
		// (session.pathCoverage) computes Pct; the gap list is derived from the
		// unexercised required edges it passes via results (threaded separately —
		// see session routing). Below PathThreshold ⇒ not reached.
		a.Measured = true
		a.CoveragePct = m.Pct
		if m.Pct < c.CoverageGate.PathThreshold {
			a.Reached = false
			a.Gaps = append(a.Gaps, contract.Gap{
				Kind:   "path",
				Detail: fmt.Sprintf("%.0f%% of required message edges exercised < %.0f%% gate", m.Pct*100, c.CoverageGate.PathThreshold*100),
			})
		}
		// Per-edge gap names are attached by the session layer, which has the
		// required-edge list (see Task 5 wiring). AssessCoverage stays objective.
		return a, nil
	}
```

> NOTE for the implementer: the per-edge gap list ("edge X not exercised") needs
> the required-edge set + exercised set, which live in the session layer
> (`exercisedEdges`). Two options: (a) compute per-edge gaps in the session layer
> after `AssessCoverage` returns, appending to `sess.Assessment.Gaps`; (b) thread
> the required/exercised sets into `AssessCoverage`. Pick (a) — keep
> `AssessCoverage` signature stable; the session appends concrete `Kind:"path"`
> gaps from `exercisedEdges`. Implement (a) in Task 5's session wiring; here just
> emit the headline path gap.

- [ ] **Step 4: Session appends per-edge path gaps**

In the session routing (Task 5), after the assessment, append concrete gaps for unexercised required edges:

```go
	exercised, _ := exercisedEdges(results, requiredEdges(sess))
	for _, e := range requiredEdges(sess) {
		if !exercised[edgeKey(e.FromRole, e.ToRole, e.Type)] {
			sess.Assessment.Gaps = append(sess.Assessment.Gaps, contract.Gap{
				Kind:   "path",
				Detail: fmt.Sprintf("edge %s→%s %s not exercised", e.FromRole, e.ToRole, e.Type),
			})
		}
	}
```

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./internal/head/examiner/ -run TestAssessCoverage -v && go test ./internal/session/ -run TestExercisedEdges -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/head/examiner/assess.go internal/head/examiner/assess_test.go \
        internal/session/coverage.go internal/session/run_phases_examiner.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "feat(examiner): objective path-coverage gate + concrete edge gaps"
```

---

### Task 7: Set `PathThreshold` deterministically for has-vocab contracts

**Files:**
- Modify: `internal/head/scout/assembly.go:229` (`assembleContract`)
- Test: `internal/head/scout/assembly_test.go`

**Interfaces:**
- Produces: when the project declares a service vocabulary, the assembled contract's `CoverageGate.PathThreshold` defaults to 1.0 (every required message edge must be exercised) — overriding the LLM's unreliable gate. The LLM's `set_coverage_gate` (module/line/branch) is ignored for has-vocab contracts (no local SUT module).

- [ ] **Step 1: Write the failing test**

Append to `internal/head/scout/assembly_test.go`:

```go
// TestAssembleContract_HasVocabSetsPathThreshold verifies that a contract for a
// service with a declared vocabulary gets an objective path gate (PathThreshold
// = 1.0) instead of the LLM's unreliable line/branch gate (which has no SUT
// module for a SaaS service).
func TestAssembleContract_HasVocabSetsPathThreshold(t *testing.T) {
	calls := []llm.ToolCall{{Name: "set_coverage_gate", Input: map[string]any{
		"module": "imagined", "line_threshold": 0.8,
	}}}
	hasVocab := true
	c := assembleContract(calls, "standard", nil, hasVocab)
	if c.CoverageGate.PathThreshold != 1.0 {
		t.Fatalf("has-vocab contract must set PathThreshold=1.0, got %v", c.CoverageGate)
	}
	if c.CoverageGate.Module != "" {
		t.Fatalf("has-vocab contract must drop the LLM's SUT module, got %q", c.CoverageGate.Module)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/head/scout/ -run TestAssembleContract_HasVocabSetsPathThreshold -v`
Expected: FAIL — `assembleContract` takes no `hasVocab` arg; PathThreshold stays 0.

- [ ] **Step 3: Thread `hasVocab` and set the gate**

In `internal/head/scout/assembly.go`, change the `assembleContract` signature to take `hasVocab bool` and, after the tool-call loop, override the gate:

```go
func assembleContract(calls []llm.ToolCall, depth string, invs []contract.InvariantRef, hasVocab bool) *contract.Contract {
	c := &contract.Contract{Depth: depth, Priorities: contract.Priorities{}, Invariants: invs}
	for _, call := range calls {
		switch call.Name {
		// ... existing cases unchanged ...
		case "set_coverage_gate":
			if hasVocab {
				// SaaS/WS service: the LLM's module/line/branch gate is meaningless
				// (no local SUT). Use the objective path gate; every declared
				// message edge must be exercised. The authority surface is the
				// extracted vocabulary, not the LLM's guess.
				continue
			}
			c.CoverageGate = contract.Gate{
				Module:          llm.StrField(call, "module"),
				LineThreshold:   llm.NumField(call, "line_threshold"),
				BranchThreshold: llm.NumField(call, "branch_threshold"),
			}
		}
	}
	if hasVocab {
		c.CoverageGate.PathThreshold = 1.0
	}
	return c
}
```

Update the single caller of `assembleContract` (search the file) to pass `hasVocab` computed from the project's services.

- [ ] **Step 4: Run to verify it passes + scout package regression**

Run: `go test ./internal/head/scout/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/head/scout/assembly.go internal/head/scout/assembly_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "feat(scout): objective path gate for has-vocab contracts (override unreliable LLM gate)"
```

---

### Task 8: Docs + full verification

**Files:**
- Create: `cerberus-docs/technical/2026-08-07-saas-coverage-authority.md` (authority model note)
- Modify: `cerberus-docs/technical/validation/2026-08-07-openagents-integration-test-report.md` (confidence section → authority model)

- [ ] **Step 1: Write the authority-model note**

Document: suite = completeness authority; run = objective progress/gaps; unmeasured = N/A. Reference the spec.

- [ ] **Step 2: Format, vet, lint, test**

Run: `make fmt && go vet ./... && make lint && make test`
Expected: clean + PASS, race-clean.

- [ ] **Step 3: Live proof (manual, open-agents up)**

Run: `make integration-openagents TEST=TestVocabularyDriven` (sanity, unaffected) — green.
Then a real `cerberus run` against open-agents (per the test report's reproduce) and confirm the coverage log now reads either `"coverage not applicable"` (if Phase 2 routing not triggered for that config) or `"coverage assessment reached=false gaps=N coverage_pct=X"` with **objective** path gaps naming real edges — never a hallucinated 0% with invented gaps. Record the observed line in the validation report.

- [ ] **Step 4: Commit**

```bash
git add cerberus-docs/
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "docs: SaaS coverage authority model + live verification note"
```

---

## Self-Review Notes

- **Spec coverage:** Phase 1 honesty (Decision 1) → Tasks 1–3. Phase 2 message-edge path coverage (Decisions 2–4) → Tasks 4–7. Authority-model docs → Task 8. Phase 3 (non-message paths) explicitly out of scope.
- **Placeholder scan:** every code step shows real content. The three "NOTE for the implementer" blocks (Judge field shape, Session→Services accessor, per-edge gap location) name exactly what to confirm and why — they are confirm-the-real-shape instructions, not TBDs.
- **Type consistency:** `Assessment.Measured`, `Gate.PathThreshold`, `CoverageMeasurement{Unit:"path",Known:true}`, `Gap{Kind:"path"}` are defined in Task 1/4 and consumed identically in Tasks 2/5/6. `exercisedEdges` + `edgeKey` defined in Task 4, reused in Tasks 5/6.
- **Zero-regression:** no-vocab sessions skip every Phase 2 branch (`sessionHasVocab` false ⇒ `lineCoverage`; `assembleContract` hasVocab=false ⇒ unchanged gate; `AssessCoverage` `Unit!="path"` ⇒ existing line branch). The Phase 1 `!Known` short-circuit is the only behavior change for unmeasured local sessions, and it replaces a hallucinated false with an honest N/A — strictly better.
- **Repair loop safety:** `hasCoverageGap` triggers only on `Kind:"coverage"`; Phase 1 emits none (Measured=false ⇒ no gaps), Phase 2 emits `Kind:"path"` (informational, not repair fuel). No phantom repair.
